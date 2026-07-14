package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/llm"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/retriever"
)

// Disclaimer is the mandatory reference-only notice, set on every result so it
// can never be omitted or altered by the model. Exported so callers building
// a comparable AnalysisResult outside this package's Score (e.g.
// cmd/benchmark's generic-AI arm) attach the identical, correct text.
const Disclaimer = "Đây là công cụ tham khảo, không thay thế cho tư vấn pháp lý hay quyết định cuối cùng của bạn. Hãy luôn tự kiểm chứng qua các kênh chính thống."

// Score classifies suspiciousText as red/yellow/green using the retrieved scam
// patterns as grounding context. It does NOT call the retriever — chunks are
// passed in. With zero chunks it still scores, falling back to general scam
// heuristics. suspiciousText is validated as 1–5000 runes (trimmed); runes, not
// bytes, because Vietnamese is multibyte (mirrors retriever.Retrieve).
func (a *Analyzer) Score(ctx context.Context, suspiciousText string, chunks []retriever.Result) (*AnalysisResult, error) {
	text := strings.TrimSpace(suspiciousText)
	n := utf8.RuneCountInString(text)
	if n == 0 {
		return nil, fmt.Errorf("analyzer: empty text")
	}
	if n > 5000 {
		return nil, fmt.Errorf("analyzer: text too long (%d chars, max 5000)", n)
	}

	if len(chunks) > a.maxChunks {
		chunks = chunks[:a.maxChunks]
	}

	raw, err := a.llm.GenerateStructured(ctx, llm.Request{
		System:   buildSystemPrompt(),
		User:     buildUserPrompt(text, chunks),
		ToolName: AnalysisToolName,
		ToolDesc: AnalysisToolDesc,
		Schema:   AnalysisToolSchema,
	})
	if err != nil {
		return nil, fmt.Errorf("analyzer: score: %w", err)
	}

	var result AnalysisResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("analyzer: unmarshal result: %w", err)
	}

	// matched_source_indices is captured into a separate struct rather than
	// AnalysisResult: it is an internal correlation signal (which reference
	// chunks the model actually used), consumed here to build Sources and
	// never surfaced in the user-facing result.
	var cite struct {
		MatchedSourceIndices []int `json:"matched_source_indices"`
	}
	if err := json.Unmarshal(raw, &cite); err != nil {
		return nil, fmt.Errorf("analyzer: unmarshal citations: %w", err)
	}

	switch result.RiskLevel {
	case RiskRed, RiskYellow, RiskGreen:
	default:
		return nil, fmt.Errorf("analyzer: invalid risk_level %q", result.RiskLevel)
	}

	// Never trust the model for the mandatory disclaimer.
	result.Disclaimer = Disclaimer
	// Normalise nil slices to empty for stable JSON output.
	if result.RedFlags == nil {
		result.RedFlags = []string{}
	}
	if result.MatchedPatterns == nil {
		result.MatchedPatterns = []string{}
	}
	if result.RecommendedActions == nil {
		result.RecommendedActions = []string{}
	}
	// Correlate against the same (already-truncated) chunks the model was
	// shown, so a citation can only reference a document it actually saw.
	result.Sources = sourcesFromIndices(cite.MatchedSourceIndices, chunks)

	return &result, nil
}

// sourcesFromIndices maps the model's matched_source_indices back to the
// retrieved documents they point at, producing citations. The indices are
// 1-based positions into the reference block the model was shown (the [1],
// [2], … numbering buildUserPrompt in prompt.go emits over this same,
// already-truncated chunks slice), so a citation can only reference a
// document the model actually saw. Out-of-range indices are dropped (the
// model can't cite a document that wasn't provided); the result is deduped
// by position and is a non-nil empty slice when nothing matches.
func sourcesFromIndices(indices []int, chunks []retriever.Result) []Source {
	sources := make([]Source, 0, len(indices))
	seen := make(map[int]bool, len(indices))
	for _, idx := range indices {
		i := idx - 1 // reference block is numbered from [1], not [0]
		if i < 0 || i >= len(chunks) || seen[i] {
			continue
		}
		seen[i] = true
		c := chunks[i]
		sources = append(sources, Source{Title: c.Title, URL: c.SourceURL})
	}
	return sources
}
