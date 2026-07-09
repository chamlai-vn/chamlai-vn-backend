package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/chamlai-vn/chamlai-vn-backend/config"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/llm"
)

const (
	judgedDirName   = "judged"
	crossFileSuffix = ".cross"

	judgeModel       = "claude-opus-4-8"
	judgeMaxTokens   = 4096 // GenerateStructured's 1024 default is too small for two full JudgeVerdicts
	judgeConcurrency = 4    // Opus calls are slow and costly; keep this modest
)

const judgeToolName = "record_judge_comparison"
const judgeToolDesc = "Ghi lại đánh giá so sánh hai câu trả lời dưới dạng dữ liệu có cấu trúc."

// judgeVerdictSchema is inlined twice into judgeToolSchema (answer_a,
// answer_b) — the fixed strengths/weaknesses/reasoning/score shape the
// product requires, unchanged by how the score is collected.
const judgeVerdictSchemaFields = `
      "type": "object",
      "properties": {
        "strengths": {"type": "array", "items": {"type": "string"}, "description": "Điểm mạnh của câu trả lời này (tiếng Việt)."},
        "weaknesses": {"type": "array", "items": {"type": "string"}, "description": "Điểm yếu của câu trả lời này (tiếng Việt)."},
        "reasoning": {"type": "string", "description": "Lý do chấm điểm cho câu trả lời này (tiếng Việt)."},
        "score": {"type": "number", "minimum": 0, "maximum": 10, "description": "Điểm cho câu trả lời này, thang 0-10 (có thể dùng số thập phân)."}
      },
      "required": ["strengths", "weaknesses", "reasoning", "score"]`

var judgeToolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "answer_a": {` + judgeVerdictSchemaFields + `
    },
    "answer_b": {` + judgeVerdictSchemaFields + `
    },
    "preferred_answer": {
      "type": "string",
      "enum": ["A", "B", "tie"],
      "description": "Câu trả lời nào tốt hơn tổng thể, hoặc \"tie\" nếu ngang nhau."
    }
  },
  "required": ["answer_a", "answer_b", "preferred_answer"]
}`)

// judgeSystemPrompt deliberately withholds the case's ground-truth verdict:
// the judge scores explanation/advice quality using KeyFacts as a rubric,
// not whether the verdict matched — that axis is already covered by the
// objective confusion matrix (pkg/util/eval), and handing the judge the
// literal answer would both double-count that signal and anchor its
// scoring toward whichever answer superficially agrees with it.
const judgeSystemPrompt = "Bạn là chuyên gia đánh giá độc lập, so sánh chất lượng của hai câu trả lời cho cùng một tin nhắn nghi ngờ lừa đảo. " +
	"Bạn KHÔNG được cho biết trước tin nhắn này có thực sự là lừa đảo hay không — nhiệm vụ của bạn là đánh giá CHẤT LƯỢNG GIẢI THÍCH VÀ LỜI KHUYÊN " +
	"của mỗi câu trả lời dựa trên các ý chính (key facts) được cung cấp, KHÔNG phải đánh giá xem kết luận đúng hay sai. " +
	"Chấm mỗi câu trả lời độc lập trên thang điểm 0-10, kèm điểm mạnh, điểm yếu, và lý do. Cuối cùng cho biết câu trả lời nào tốt hơn tổng thể, hoặc \"tie\" nếu ngang nhau."

// runJudge scores every (rag-hybrid, generic-websearch) pair the -run phase
// produced, dual-order (see judgeCase), and checkpoints one JudgedCase per
// case to judged/<id>.json. If crossFamilyN > 0 and GEMINI_API_KEY is set,
// additionally scores the first crossFamilyN cases with a cross-family
// judge into judged/<id>.cross.json, for self-preference-bias validation —
// reported separately in -report, never merged into the primary headline.
func runJudge(ctx context.Context, cfg config.Configuration, runDir string, limit, crossFamilyN int) {
	if cfg.AnthropicAPIKey == "" {
		log.Fatal("ANTHROPIC_API_KEY is required for -judge")
	}
	cases, err := readDataset(runDir)
	if err != nil {
		log.Fatalf("judge: %v", err)
	}
	if limit > 0 && limit < len(cases) {
		cases = cases[:limit]
	}
	if err := os.MkdirAll(filepath.Join(runDir, judgedDirName), 0o755); err != nil {
		log.Fatalf("judge: %v", err)
	}

	judgeLLM := llm.NewAnthropic(llm.AnthropicConfig{APIKey: cfg.AnthropicAPIKey, Model: judgeModel})
	log.Printf("judge ready: model=%s", judgeLLM.Model())

	var t tally
	runJudgePool(ctx, runDir, cases, judgeLLM, "", judgeConcurrency, &t)
	nNew, nSkip, nErr := t.totals()
	log.Printf("judge done: %d judged, %d skipped, %d errors", nNew, nSkip, nErr)

	meta := readMeta(runDir)
	meta.JudgeModel = judgeLLM.Model()

	switch {
	case crossFamilyN <= 0:
		// disabled
	case cfg.GeminiAPIKey == "":
		log.Printf("judge: GEMINI_API_KEY not set, skipping cross-family validation subset")
	default:
		crossLLM, err := llm.NewGemini(llm.GeminiConfig{APIKey: cfg.GeminiAPIKey})
		if err != nil {
			log.Printf("judge: cross-family gemini client: %v — skipping cross-family validation", err)
			break
		}
		log.Printf("cross-family judge ready: model=%s", crossLLM.Model())

		n := crossFamilyN
		if n > len(cases) {
			n = len(cases)
		}
		var ct tally
		runJudgePool(ctx, runDir, cases[:n], crossLLM, crossFileSuffix, judgeConcurrency, &ct)
		cNew, cSkip, cErr := ct.totals()
		log.Printf("cross-family judge done: %d judged, %d skipped, %d errors", cNew, cSkip, cErr)
		meta.CrossFamilyModel = crossLLM.Model()
		meta.CrossFamilyN = n
	}

	if err := writeMeta(runDir, meta); err != nil {
		log.Fatalf("judge: %v", err)
	}
}

// runJudgePool scores cases with judgeLLM, writing judged/<id><fileSuffix>.json
// (fileSuffix is "" for the primary judge, ".cross" for the cross-family
// validation subset). Skips cases whose file already exists or whose arm
// outputs (from -run) are missing; writes a checkpoint only on success —
// same resume contract as runArmPool.
func runJudgePool(ctx context.Context, runDir string, cases []TestCase, judgeLLM llm.Service, fileSuffix string, concurrency int, t *tally) {
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, tc := range cases {
		path := judgedFilePath(runDir, tc.ID, fileSuffix)
		if _, err := os.Stat(path); err == nil {
			t.skip("skip (exists): %s%s", tc.ID, fileSuffix)
			continue
		}

		ragOut, ragErr := readArmOutput(armFilePath(runDir, tc.ID, ArmRAGHybrid))
		genericOut, genErr := readArmOutput(armFilePath(runDir, tc.ID, ArmGenericWebSearch))
		if ragErr != nil || genErr != nil {
			t.skip("skip (missing arm output, run -run first): %s", tc.ID)
			continue
		}

		wg.Add(1)
		go func(tc TestCase, ragOut, genericOut ArmOutput, path string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			jc, err := judgeCase(ctx, judgeLLM, tc, ragOut, genericOut)
			if err != nil {
				t.fail("error %s: %v", tc.ID, err)
				return
			}
			jc.CaseID = tc.ID

			if err := writeJudgedCase(path, jc); err != nil {
				t.fail("write %s: %v", tc.ID, err)
				return
			}
			t.ok("judged %s", tc.ID)
		}(tc, ragOut, genericOut, path)
	}
	wg.Wait()
}

// judgeCase runs two Opus calls per case with the arms' labels swapped
// (order 1: rag=A, generic=B; order 2: rag=B, generic=A) rather than a
// single call with a random shuffle — position bias in LLM-as-judge is
// systematic, not noise, and strongest exactly when two answers are close
// in quality, so distributing it randomly doesn't cancel it. If the two
// orders disagree on which arm is better, that disagreement is reported
// (Preferred="tie", OrderConsistent=false) rather than silently averaged
// into a single answer.
func judgeCase(ctx context.Context, judgeLLM llm.Service, tc TestCase, ragOut, genericOut ArmOutput) (JudgedCase, error) {
	ragAnswer := renderAnswerForJudge(ragOut)
	genericAnswer := renderAnswerForJudge(genericOut)

	call1, err := runJudgeCall(ctx, judgeLLM, tc, ragAnswer, genericAnswer) // A=rag, B=generic
	if err != nil {
		return JudgedCase{}, fmt.Errorf("order 1 (rag=A): %w", err)
	}
	call2, err := runJudgeCall(ctx, judgeLLM, tc, genericAnswer, ragAnswer) // A=generic, B=rag
	if err != nil {
		return JudgedCase{}, fmt.Errorf("order 2 (rag=B): %w", err)
	}

	pref1 := decodePreference(call1.PreferredAnswer, ArmRAGHybrid, ArmGenericWebSearch)
	pref2 := decodePreference(call2.PreferredAnswer, ArmGenericWebSearch, ArmRAGHybrid)
	orderConsistent := pref1 == pref2

	preferred := "tie"
	if orderConsistent {
		preferred = pref1
	}

	return JudgedCase{
		RAGVerdict:      call1.AnswerA, // order 1: A = rag
		GenericVerdict:  call1.AnswerB, // order 1: B = generic
		Preferred:       preferred,
		OrderConsistent: orderConsistent,
	}, nil
}

// decodePreference maps a judge call's raw "A"/"B"/"tie" answer back to the
// arm name that was in that slot for that call.
func decodePreference(raw, ifA, ifB string) string {
	switch raw {
	case "A":
		return ifA
	case "B":
		return ifB
	default:
		return "tie"
	}
}

func runJudgeCall(ctx context.Context, judgeLLM llm.Service, tc TestCase, answerA, answerB string) (JudgeCallResult, error) {
	user := fmt.Sprintf(
		"Tin nhắn gốc:\n%s\n\nCác ý chính nên được đề cập trong một câu trả lời tốt:\n%s\n\nCâu trả lời A:\n%s\n\nCâu trả lời B:\n%s",
		tc.Text, formatKeyFacts(tc.KeyFacts), answerA, answerB,
	)
	raw, err := judgeLLM.GenerateStructured(ctx, llm.Request{
		System:    judgeSystemPrompt,
		User:      user,
		ToolName:  judgeToolName,
		ToolDesc:  judgeToolDesc,
		Schema:    judgeToolSchema,
		MaxTokens: judgeMaxTokens,
	})
	if err != nil {
		return JudgeCallResult{}, err
	}
	var result JudgeCallResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return JudgeCallResult{}, fmt.Errorf("unmarshal: %w", err)
	}
	return result, nil
}

func formatKeyFacts(facts []string) string {
	if len(facts) == 0 {
		return "(không có)"
	}
	var b strings.Builder
	for _, f := range facts {
		fmt.Fprintf(&b, "- %s\n", f)
	}
	return b.String()
}

// renderAnswerForJudge produces a natural-language rendering of one arm's
// output: the generic arm's RawText is exactly what a real user would see,
// so it's used as-is; the rag-hybrid arm has no RawText (its Result IS the
// natural output), so it's rendered from the structured fields the same way
// a consumer of AnalysisResult would present it.
func renderAnswerForJudge(out ArmOutput) string {
	if out.RawText != "" {
		return out.RawText
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Mức độ rủi ro: %s\n", out.Result.RiskLevel)
	if len(out.Result.RedFlags) > 0 {
		b.WriteString("Dấu hiệu cảnh báo:\n")
		for _, f := range out.Result.RedFlags {
			fmt.Fprintf(&b, "- %s\n", f)
		}
	}
	if len(out.Result.MatchedPatterns) > 0 {
		b.WriteString("Chiêu trò đã biết khớp:\n")
		for _, p := range out.Result.MatchedPatterns {
			fmt.Fprintf(&b, "- %s\n", p)
		}
	}
	if len(out.Result.RecommendedActions) > 0 {
		b.WriteString("Khuyến nghị:\n")
		for _, a := range out.Result.RecommendedActions {
			fmt.Fprintf(&b, "- %s\n", a)
		}
	}
	return b.String()
}

func judgedFilePath(runDir, caseID, fileSuffix string) string {
	return filepath.Join(runDir, judgedDirName, fmt.Sprintf("%s%s.json", caseID, fileSuffix))
}

func writeJudgedCase(path string, jc JudgedCase) error {
	raw, err := json.MarshalIndent(jc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return os.WriteFile(path, raw, 0o644)
}

func readJudgedCase(path string) (JudgedCase, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return JudgedCase{}, err
	}
	var jc JudgedCase
	if err := json.Unmarshal(raw, &jc); err != nil {
		return JudgedCase{}, fmt.Errorf("unmarshal %s: %w", path, err)
	}
	return jc, nil
}
