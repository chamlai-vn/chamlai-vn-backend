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

	return &result, nil
}
