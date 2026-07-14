package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/llm"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/analyzer"
)

const (
	// webSearchTimeout is longer than GenerateStructured's fixed 60s
	// (internal/ai/llm/anthropic.go) because this loop can involve multiple
	// server-side search rounds before returning.
	webSearchTimeout = 90 * time.Second
	// assessMaxTokens covers the Assess call's natural-language answer —
	// synthesizing multiple search results with citations routinely runs
	// well past 2048 tokens in practice (observed truncating on most cases
	// at that limit), so this is deliberately generous.
	assessMaxTokens = 4096
	// structureMaxTokens covers the separate, much shorter structuring call
	// (structureAnswer) — a fixed-shape AnalysisResult, not free text.
	structureMaxTokens = 2048
	// webSearchMaxPauseTurns caps how many times Assess resends a
	// long-running turn (stop_reason=pause_turn) before giving up, so a
	// pathological case can't loop forever.
	webSearchMaxPauseTurns = 4
)

// webSearcher assesses a suspicious message using live web search and
// returns the model's natural-language answer plus any cited source URLs.
// This is a narrow interface (mirroring analyzer.LLM, analyze.Retriever) so
// scoreGeneric is testable with a fake, even though the concrete
// implementation reaches the Anthropic SDK directly — the one place in this
// repo that does. Web search requires tool_choice:auto, which doesn't fit
// llm.Service's forced-tool-use shape, so it can't go through that
// interface without leaking a capability none of the other providers have
// (see cmd/benchmark's package doc and the plan's Alternatives Considered).
type webSearcher interface {
	Assess(ctx context.Context, text string) (rawText string, sources []string, searchFailed bool, err error)
}

// anthropicWebSearcher is the concrete webSearcher backed by the Anthropic
// Messages API's server-side web_search tool.
type anthropicWebSearcher struct {
	client  anthropic.Client
	model   string
	maxUses int64
	reqOpts []option.RequestOption // extra per-call SDK options (e.g. base URL in tests)
}

func newAnthropicWebSearcher(apiKey, model string, maxUses int, opts ...option.RequestOption) *anthropicWebSearcher {
	return &anthropicWebSearcher{
		client:  anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:   model,
		maxUses: int64(maxUses),
		reqOpts: opts,
	}
}

const genericSystemPrompt = "Bạn là một trợ lý AI hữu ích. Người dùng sẽ hỏi bạn về một tin nhắn hoặc tình huống mà họ nghi ngờ có thể là lừa đảo. " +
	"Hãy tìm kiếm thông tin liên quan trên mạng nếu cần, rồi trả lời rõ ràng: tình huống này có phải lừa đảo không, tại sao, và người dùng nên làm gì tiếp theo."

// Assess sends text to Sonnet with the web_search tool, tool_choice left
// unset (== "auto" — required for web search: forcing a tool choice would
// make the model call it immediately without ever searching).
//
// The search loop runs server-side inside the Anthropic API: a
// server_tool_use block and its matching web_search_tool_result block
// arrive already paired in the SAME response — there is no tool_result to
// send back, unlike a client tool. The only reason to loop is
// StopReason == "pause_turn" (the API pausing a long-running turn); the fix
// is to resend the assistant response's content unchanged and continue,
// per Anthropic's server-tools docs. Hand-processing individual
// server_tool_use/web_search_tool_result pairs as if this were a client
// tool would be a misreading of the API.
func (a *anthropicWebSearcher) Assess(ctx context.Context, text string) (string, []string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, webSearchTimeout)
	defer cancel()

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: assessMaxTokens,
		System:    []anthropic.TextBlockParam{{Text: genericSystemPrompt}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(text)),
		},
		Tools: []anthropic.ToolUnionParam{{
			OfWebSearchTool20250305: &anthropic.WebSearchTool20250305Param{
				MaxUses: anthropic.Int(a.maxUses),
			},
		}},
	}

	var resp *anthropic.Message
	for i := 0; i < webSearchMaxPauseTurns; i++ {
		var err error
		resp, err = a.client.Messages.New(ctx, params, a.reqOpts...)
		if err != nil {
			return "", nil, false, fmt.Errorf("websearch: messages: %w", err)
		}
		if resp.StopReason == anthropic.StopReasonMaxTokens {
			return "", nil, false, fmt.Errorf("websearch: response truncated at max_tokens (%d)", assessMaxTokens)
		}
		if resp.StopReason != anthropic.StopReasonPauseTurn {
			break
		}
		params.Messages = append(params.Messages, resp.ToParam())
	}
	if resp == nil {
		return "", nil, false, fmt.Errorf("websearch: no response after %d pause_turn continuation(s)", webSearchMaxPauseTurns)
	}

	return extractAssessResult(resp)
}

// extractAssessResult walks resp.Content for the final answer text, source
// URLs (from citations on text blocks and from raw web_search_tool_result
// blocks), and whether any search errored (e.g. max_uses_exceeded — the API
// still returns 200, the error is inside the result block).
func extractAssessResult(resp *anthropic.Message) (string, []string, bool, error) {
	var text strings.Builder
	var sources []string
	seen := map[string]bool{}
	addSource := func(url string) {
		if url != "" && !seen[url] {
			seen[url] = true
			sources = append(sources, url)
		}
	}

	searchFailed := false
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			tb := block.AsText()
			text.WriteString(tb.Text)
			for _, cit := range tb.Citations {
				if cit.Type == "web_search_result_location" {
					addSource(cit.URL)
				}
			}
		case "web_search_tool_result":
			wsr := block.AsWebSearchToolResult()
			if wsr.Content.ErrorCode != "" {
				searchFailed = true
				continue
			}
			for _, r := range wsr.Content.OfWebSearchResultBlockArray {
				addSource(r.URL)
			}
		}
	}

	if text.Len() == 0 {
		return "", sources, true, fmt.Errorf("websearch: no text content in response (stop_reason=%s)", resp.StopReason)
	}
	return text.String(), sources, searchFailed, nil
}

const structureSystemPrompt = "Bạn được cung cấp câu trả lời bằng văn bản tự nhiên của một trợ lý AI khác cho câu hỏi về nghi vấn lừa đảo. " +
	"Nhiệm vụ CHỈ là trích xuất trung thực nội dung của câu trả lời đó vào định dạng có cấu trúc — " +
	"KHÔNG tự phân tích lại, KHÔNG suy diễn thêm thông tin không có trong văn bản gốc."

// structureAnswer maps rawText (the generic arm's natural-language answer)
// into the same AnalysisResult shape the rag-hybrid arm produces, via a
// SEPARATE forced-tool-use call — not part of the Assess call above, because
// tool_choice:auto (required for web search) and tool_choice:tool (required
// to force this shape) cannot coexist in one request.
func structureAnswer(ctx context.Context, structureLLM llm.Service, rawText string) (analyzer.AnalysisResult, error) {
	raw, err := structureLLM.GenerateStructured(ctx, llm.Request{
		System:    structureSystemPrompt,
		User:      fmt.Sprintf("Trích xuất câu trả lời sau vào định dạng có cấu trúc:\n\n%s", rawText),
		ToolName:  analyzer.AnalysisToolName,
		ToolDesc:  analyzer.AnalysisToolDesc,
		Schema:    analyzer.AnalysisToolSchema,
		MaxTokens: structureMaxTokens,
	})
	if err != nil {
		return analyzer.AnalysisResult{}, fmt.Errorf("structure: %w", err)
	}

	var result analyzer.AnalysisResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return analyzer.AnalysisResult{}, fmt.Errorf("structure: unmarshal: %w", err)
	}
	switch result.RiskLevel {
	case analyzer.RiskRed, analyzer.RiskYellow, analyzer.RiskGreen:
	default:
		return analyzer.AnalysisResult{}, fmt.Errorf("structure: invalid risk_level %q", result.RiskLevel)
	}
	// Mirror analyzer.Score's conventions: never trust the model for the
	// disclaimer, normalise nil slices to empty for stable JSON output.
	result.Disclaimer = analyzer.Disclaimer
	if result.RedFlags == nil {
		result.RedFlags = []string{}
	}
	if result.MatchedPatterns == nil {
		result.MatchedPatterns = []string{}
	}
	if result.RecommendedActions == nil {
		result.RecommendedActions = []string{}
	}
	// This arm has no corpus retrieval, so it cites no source documents —
	// but the slice is still normalised to non-nil for shape parity with
	// the rag-hybrid arm's AnalysisResult.
	if result.Sources == nil {
		result.Sources = []analyzer.Source{}
	}
	return result, nil
}

// scoreGeneric runs the two-phase generic arm: assess with web search, then
// structure the answer into a comparable AnalysisResult.
func scoreGeneric(ctx context.Context, ws webSearcher, structureLLM llm.Service, tc TestCase) (ArmOutput, error) {
	rawText, sources, searchFailed, err := ws.Assess(ctx, tc.Text)
	if err != nil {
		return ArmOutput{}, fmt.Errorf("assess: %w", err)
	}

	result, err := structureAnswer(ctx, structureLLM, rawText)
	if err != nil {
		return ArmOutput{}, fmt.Errorf("structure: %w", err)
	}

	return ArmOutput{
		Result:       result,
		RawText:      rawText,
		Sources:      sources,
		SearchFailed: searchFailed,
	}, nil
}
