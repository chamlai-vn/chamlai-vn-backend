package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"
)

// searchResponse builds a minimal Messages API response with one text block
// (optionally citing sources) and, optionally, a matching
// web_search_tool_result block — enough for the SDK to decode and for
// extractAssessResult to exercise its citation/source-extraction paths.
func searchResponse(stopReason, text string, citedURL string, resultErrorCode string) string {
	content := []any{}
	textBlock := map[string]any{"type": "text", "text": text}
	if citedURL != "" {
		textBlock["citations"] = []any{map[string]any{
			"type":            "web_search_result_location",
			"url":             citedURL,
			"title":           "t",
			"cited_text":      "c",
			"encrypted_index": "e",
		}}
	} else {
		textBlock["citations"] = []any{}
	}
	content = append(content, textBlock)

	if resultErrorCode != "" {
		content = append(content, map[string]any{
			"type":        "web_search_tool_result",
			"tool_use_id": "srvtoolu_1",
			"content":     map[string]any{"type": "web_search_tool_result_error", "error_code": resultErrorCode},
		})
	}

	body := map[string]any{
		"id":            "msg_test",
		"type":          "message",
		"role":          "assistant",
		"model":         "claude-sonnet-4-6",
		"content":       content,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage":         map[string]any{"input_tokens": 10, "output_tokens": 5},
	}
	b, _ := json.Marshal(body)
	return string(b)
}

func newTestWebSearcher(srv *httptest.Server) *anthropicWebSearcher {
	return newAnthropicWebSearcher("test", "claude-sonnet-4-6", 4,
		option.WithBaseURL(srv.URL), option.WithHTTPClient(srv.Client()))
}

func TestAssess_ReturnsTextAndSources(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(searchResponse("end_turn", "Đây là lừa đảo.", "https://example.com/scam-warning", "")))
	}))
	defer srv.Close()

	ws := newTestWebSearcher(srv)
	text, sources, searchFailed, err := ws.Assess(context.Background(), "nghi ngờ lừa đảo")
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if text != "Đây là lừa đảo." {
		t.Errorf("text = %q, want %q", text, "Đây là lừa đảo.")
	}
	if len(sources) != 1 || sources[0] != "https://example.com/scam-warning" {
		t.Errorf("sources = %v, want [https://example.com/scam-warning]", sources)
	}
	if searchFailed {
		t.Error("searchFailed = true, want false")
	}
}

func TestAssess_SendsAutoToolChoiceWithWebSearchTool(t *testing.T) {
	// Regression guard: forcing a tool choice here would make the model call
	// it immediately without ever searching — tool_choice must stay unset.
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(searchResponse("end_turn", "ok", "", "")))
	}))
	defer srv.Close()

	ws := newTestWebSearcher(srv)
	if _, _, _, err := ws.Assess(context.Background(), "q"); err != nil {
		t.Fatalf("Assess: %v", err)
	}

	if _, ok := gotBody["tool_choice"]; ok {
		t.Errorf("tool_choice = %v, want omitted (auto)", gotBody["tool_choice"])
	}
	tools, ok := gotBody["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %v, want exactly one", gotBody["tools"])
	}
	tool := tools[0].(map[string]any)
	if tool["type"] != "web_search_20250305" {
		t.Errorf("tool type = %v, want web_search_20250305", tool["type"])
	}
	if maxUses, _ := tool["max_uses"].(float64); int(maxUses) != 4 {
		t.Errorf("max_uses = %v, want 4", tool["max_uses"])
	}
}

func TestAssess_LoopsOnPauseTurnThenSucceeds(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_, _ = w.Write([]byte(searchResponse("pause_turn", "đang tìm...", "", "")))
			return
		}
		_, _ = w.Write([]byte(searchResponse("end_turn", "kết luận cuối cùng", "", "")))
	}))
	defer srv.Close()

	ws := newTestWebSearcher(srv)
	text, _, _, err := ws.Assess(context.Background(), "q")
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (one pause_turn continuation)", calls)
	}
	if text != "kết luận cuối cùng" {
		t.Errorf("text = %q, want the final response's text", text)
	}
}

func TestAssess_MaxTokensErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(searchResponse("max_tokens", "cắt ngang", "", "")))
	}))
	defer srv.Close()

	ws := newTestWebSearcher(srv)
	if _, _, _, err := ws.Assess(context.Background(), "q"); err == nil {
		t.Fatal("expected error for stop_reason=max_tokens, got nil")
	}
}

func TestAssess_SearchErrorFlagsSearchFailedButKeepsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(searchResponse("end_turn", "trả lời từ kiến thức nội tại", "", "max_uses_exceeded")))
	}))
	defer srv.Close()

	ws := newTestWebSearcher(srv)
	text, _, searchFailed, err := ws.Assess(context.Background(), "q")
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if !searchFailed {
		t.Error("searchFailed = false, want true (max_uses_exceeded)")
	}
	if text != "trả lời từ kiến thức nội tại" {
		t.Errorf("text = %q, want the fallback answer to survive a search error", text)
	}
}

func TestAssess_NoTextContentErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{
			"id": "msg_test", "type": "message", "role": "assistant",
			"model": "claude-sonnet-4-6", "content": []any{},
			"stop_reason": "end_turn", "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": 5, "output_tokens": 0},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()

	ws := newTestWebSearcher(srv)
	if _, _, _, err := ws.Assess(context.Background(), "q"); err == nil {
		t.Fatal("expected error when response has no text content, got nil")
	}
}
