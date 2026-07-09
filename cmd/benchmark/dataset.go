package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/chamlai-vn/chamlai-vn-backend/config"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/llm"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/analyzer"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/crawler"
)

const (
	nScamCases      = 120
	nBenignCases    = 40
	datasetFileName = "dataset.json"
	// datasetMaxTokens overrides GenerateStructured's 1024 default — a
	// long_narration case (5-8 sentences) plus 2-4 key_facts can exceed it
	// and hit the hard StopReasonMaxTokens error (internal/ai/llm/anthropic.go).
	datasetMaxTokens = 2048
)

// benignCategory is a hand-picked taxonomy of legitimate messages that are
// easily mistaken for scams — the counterpart to crawler.ScamTypeDescriptions
// for the benign half of the dataset. Deliberately covers the same surface
// area scam messages exploit (bank notices, delivery, promos, work
// requests) so the dataset genuinely stress-tests false positives rather
// than testing obviously-safe messages.
type benignCategory struct {
	Slug        string
	Description string
}

var benignCategories = []benignCategory{
	{"sms_bank_real", "tin nhắn xác thực OTP hoặc thông báo giao dịch/số dư thật từ ngân hàng"},
	{"delivery_real", "thông báo giao hàng/bưu kiện thật từ đơn vị vận chuyển, không yêu cầu đóng phí bất thường"},
	{"promo_real", "tin nhắn khuyến mãi/ưu đãi thật từ nhà mạng, ví điện tử, hoặc sàn thương mại điện tử đã đăng ký"},
	{"work_real", "lời mời họp, thông báo công việc, hoặc tin nhắn nội bộ hợp pháp từ đồng nghiệp/đối tác"},
}

// caseSpec is the harness-assigned ground truth for one dataset case, built
// deterministically by buildSpecs BEFORE any LLM call — the generator only
// ever fills in Text/KeyFacts for a spec it's handed, it never decides
// IsScam/ScamType/Style/ExpectedVerdict itself.
type caseSpec struct {
	id              string
	isScam          bool
	scamType        string // "" for benign
	description     string // taxonomy description fed to the prompt
	style           string
	expectedVerdict string
}

// buildSpecs enumerates every (type, style) cell deterministically: 120 scam
// cases spread across the 13 scam types (round-robin styles within each
// type) plus 40 benign cases spread across 4 benign categories (10 each,
// styles rotate evenly). Sorted scam-type order keeps generated IDs stable
// across runs for the same nScamCases/nBenignCases.
func buildSpecs() []caseSpec {
	scamTypes := make([]string, 0, len(crawler.ScamTypeDescriptions))
	for st := range crawler.ScamTypeDescriptions {
		scamTypes = append(scamTypes, st)
	}
	sort.Strings(scamTypes)

	var specs []caseSpec
	styleIdx := 0

	n := len(scamTypes)
	base, extra := nScamCases/n, nScamCases%n
	for ti, st := range scamTypes {
		count := base
		if ti < extra {
			count++ // first `extra` types absorb the remainder, one case each
		}
		for i := 0; i < count; i++ {
			specs = append(specs, caseSpec{
				id:              fmt.Sprintf("scam-%s-%02d", st, i+1),
				isScam:          true,
				scamType:        st,
				description:     crawler.ScamTypeDescriptions[st],
				style:           AllStyles[styleIdx%len(AllStyles)],
				expectedVerdict: analyzer.RiskRed,
			})
			styleIdx++
		}
	}

	perCategory := nBenignCases / len(benignCategories)
	for _, bc := range benignCategories {
		for i := 0; i < perCategory; i++ {
			specs = append(specs, caseSpec{
				id:              fmt.Sprintf("benign-%s-%02d", bc.Slug, i+1),
				isScam:          false,
				description:     bc.Description,
				style:           AllStyles[styleIdx%len(AllStyles)],
				expectedVerdict: analyzer.RiskGreen,
			})
			styleIdx++
		}
	}
	return specs
}

const (
	datasetToolName = "record_test_case"
	datasetToolDesc = "Ghi lại một tin nhắn test và các ý chính liên quan dưới dạng dữ liệu có cấu trúc."
)

var datasetCaseSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "text": {
      "type": "string",
      "description": "Nội dung tin nhắn/câu hỏi bằng tiếng Việt, đúng theo phong cách và độ dài được yêu cầu."
    },
    "key_facts": {
      "type": "array",
      "items": {"type": "string"},
      "description": "2-4 ý chính (tiếng Việt) mà một câu trả lời tốt nên nhắc tới khi phân tích tin nhắn này."
    }
  },
  "required": ["text", "key_facts"]
}`)

var styleInstructions = map[string]string{
	StyleShortQuestion:   "Viết một câu hỏi NGẮN (1-2 câu) như người dùng thật nhắn hỏi trợ lý, không thuật lại toàn bộ câu chuyện.",
	StyleLongNarration:   "Viết một đoạn tường thuật DÀI (5-8 câu) kể lại chi tiết sự việc đã xảy ra với người dùng hoặc người quen của họ.",
	StyleVerbatimMessage: "Viết ĐÚNG NGUYÊN VĂN tin nhắn như thể copy-paste trực tiếp từ nguồn gốc (kẻ lừa đảo, hoặc tin nhắn thật nếu là trường hợp hợp pháp) — không có phần dẫn dắt của người dùng, không có lời chào hỏi trợ lý.",
	StyleThirdPerson:     "Viết dưới góc nhìn NGƯỜI THỨ BA, kể lại việc một người quen (bạn bè/người thân) gặp phải tình huống này.",
}

// runGen generates a stratified dataset of nScamCases scam + nBenignCases
// benign cases into runDir/dataset.json, using a Haiku client pinned
// independently of cfg.AnthropicModel — the generator role must stay cheap
// regardless of what model the rag-hybrid arm under test is configured to
// use (see arms.go). The generator never receives corpus content, only a
// scam-type/benign-category label per case, so it cannot produce cases that
// leak near-verbatim indexed documents and unfairly inflate the RAG arm.
func runGen(ctx context.Context, cfg config.Configuration, runDir string, limit int) {
	if cfg.AnthropicAPIKey == "" {
		log.Fatal("ANTHROPIC_API_KEY is required for -gen")
	}
	llmSvc, err := llm.New(llm.Config{
		Provider:  llm.ProviderAnthropic,
		Anthropic: llm.AnthropicConfig{APIKey: cfg.AnthropicAPIKey}, // Model unset -> cheap default (Haiku)
	})
	if err != nil {
		log.Fatalf("llm: %v", err)
	}
	log.Printf("generator ready: model=%s", llmSvc.Model())

	specs := buildSpecs()
	if limit > 0 && limit < len(specs) {
		specs = specs[:limit]
	}
	log.Printf("gen: %d case(s) queued", len(specs))

	cases := make([]TestCase, len(specs))
	var t tally
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, spec := range specs {
		wg.Add(1)
		go func(i int, spec caseSpec) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			tc, err := generateCase(ctx, llmSvc, spec)
			if err != nil {
				t.fail("skip %s: %v", spec.id, err)
				return
			}
			cases[i] = tc
			t.ok("generated %s [scam_type=%s style=%s]", tc.ID, tc.ScamType, tc.Style)
		}(i, spec)
	}
	wg.Wait()

	final := cases[:0]
	for _, tc := range cases {
		if tc.ID != "" {
			final = append(final, tc)
		}
	}

	nNew, _, nErr := t.totals()
	log.Printf("gen done: %d generated, %d errors", nNew, nErr)

	if err := writeDataset(runDir, final); err != nil {
		log.Fatalf("gen: %v", err)
	}

	meta := readMeta(runDir)
	meta.Timestamp = nowUTC()
	meta.GitSHA = gitSHA()
	meta.NCases = len(final)
	meta.DatasetHash = hashDataset(final)
	meta.GeneratorModel = llmSvc.Model()
	if err := writeMeta(runDir, meta); err != nil {
		log.Fatalf("gen: %v", err)
	}

	log.Printf("gen: wrote %d case(s) to %s", len(final), filepath.Join(runDir, datasetFileName))
	log.Printf("gen: REVIEW the dataset before -run — it's plain, readable JSON")
}

// generateCase asks the generator for Text+KeyFacts for spec and assembles
// the full TestCase, merging in the harness-assigned ground truth.
func generateCase(ctx context.Context, llmSvc llm.Service, spec caseSpec) (TestCase, error) {
	var system, user string
	if spec.isScam {
		system = "Bạn giúp xây dựng bộ dữ liệu kiểm thử cho một hệ thống phát hiện lừa đảo qua tin nhắn tiếng Việt. " +
			"Nhiệm vụ của bạn CHỈ là viết ra một tình huống lừa đảo giả định, hợp lý — không truy cập hay trích dẫn bất kỳ nguồn có sẵn nào."
		user = fmt.Sprintf(
			"Viết MỘT tình huống liên quan đến chiêu trò lừa đảo loại %q (%s).\n%s\n"+
				"Tự nghĩ ra chi tiết cụ thể (tên, số tiền, ứng dụng, số điện thoại giả...) — không sao chép nguyên văn từ nguồn nào.",
			spec.scamType, spec.description, styleInstructions[spec.style],
		)
	} else {
		system = "Bạn giúp xây dựng bộ dữ liệu kiểm thử cho một hệ thống phát hiện lừa đảo qua tin nhắn tiếng Việt. " +
			"Nhiệm vụ của bạn là viết ra một tin nhắn HOÀN TOÀN HỢP PHÁP, không phải lừa đảo, để kiểm tra hệ thống có báo động giả hay không."
		user = fmt.Sprintf(
			"Viết MỘT tin nhắn/tình huống HỢP PHÁP: %s. "+
				"Tình huống này có thể khiến người nhận e ngại nhưng thực chất vô hại.\n%s",
			spec.description, styleInstructions[spec.style],
		)
	}

	raw, err := llmSvc.GenerateStructured(ctx, llm.Request{
		System:    system,
		User:      user,
		ToolName:  datasetToolName,
		ToolDesc:  datasetToolDesc,
		Schema:    datasetCaseSchema,
		MaxTokens: datasetMaxTokens, // default 1024 truncates long_narration cases + key_facts
	})
	if err != nil {
		return TestCase{}, fmt.Errorf("generate: %w", err)
	}

	var out struct {
		Text     string   `json:"text"`
		KeyFacts []string `json:"key_facts"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return TestCase{}, fmt.Errorf("unmarshal: %w", err)
	}
	if out.Text == "" {
		return TestCase{}, fmt.Errorf("empty text in generated case")
	}

	return TestCase{
		ID:              spec.id,
		Text:            out.Text,
		IsScam:          spec.isScam,
		ExpectedVerdict: spec.expectedVerdict,
		ScamType:        spec.scamType,
		Style:           spec.style,
		KeyFacts:        out.KeyFacts,
	}, nil
}

func writeDataset(runDir string, cases []TestCase) error {
	raw, err := json.MarshalIndent(cases, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal dataset: %w", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, datasetFileName), raw, 0o644); err != nil {
		return fmt.Errorf("write dataset: %w", err)
	}
	return nil
}

// readDataset loads runDir/dataset.json, written by a prior -gen.
func readDataset(runDir string) ([]TestCase, error) {
	raw, err := os.ReadFile(filepath.Join(runDir, datasetFileName))
	if err != nil {
		return nil, fmt.Errorf("read dataset (run -gen first?): %w", err)
	}
	var cases []TestCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		return nil, fmt.Errorf("unmarshal dataset: %w", err)
	}
	return cases, nil
}
