package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"
)

// anthropicMessageResponse builds a minimal Messages API response carrying
// one tool_use block, enough for the SDK to decode.
func anthropicMessageResponse(toolName string, input map[string]any, stopReason string) string {
	inputJSON, _ := json.Marshal(input)
	body := map[string]any{
		"id":    "msg_test",
		"type":  "message",
		"role":  "assistant",
		"model": "claude-haiku-4-5-20251001",
		"content": []any{map[string]any{
			"type":  "tool_use",
			"id":    "toolu_test",
			"name":  toolName,
			"input": json.RawMessage(inputJSON),
		}},
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage":         map[string]any{"input_tokens": 10, "output_tokens": 5},
	}
	b, _ := json.Marshal(body)
	return string(b)
}

func newTestAnthropic(_ *testing.T, srv *httptest.Server, cfg AnthropicConfig) *Anthropic {
	return NewAnthropic(cfg, WithAnthropicRequestOptions(
		option.WithBaseURL(srv.URL),
		option.WithHTTPClient(srv.Client()),
	))
}

func TestAnthropicGenerateStructured_ReturnsToolInput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(anthropicMessageResponse("record_scam_analysis", map[string]any{"verdict": "green"}, "tool_use")))
	}))
	defer srv.Close()

	a := newTestAnthropic(t, srv, AnthropicConfig{APIKey: "test"})
	raw, err := a.GenerateStructured(context.Background(), Request{User: "q", ToolName: "record_scam_analysis"})
	if err != nil {
		t.Fatalf("GenerateStructured: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal returned input: %v", err)
	}
	if got["verdict"] != "green" {
		t.Errorf("verdict = %v, want green", got["verdict"])
	}
}

func TestAnthropicGenerateStructured_SendsForcedToolChoice(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(anthropicMessageResponse("record_scam_analysis", map[string]any{"verdict": "red"}, "tool_use")))
	}))
	defer srv.Close()

	a := newTestAnthropic(t, srv, AnthropicConfig{APIKey: "test"})
	if _, err := a.GenerateStructured(context.Background(), Request{
		System:   "you score scams",
		User:     "nghi ngờ lừa đảo",
		ToolName: "record_scam_analysis",
		ToolDesc: "record the verdict",
		Schema:   json.RawMessage(`{"type":"object","properties":{"verdict":{"type":"string"}},"required":["verdict"]}`),
	}); err != nil {
		t.Fatalf("GenerateStructured: %v", err)
	}

	toolChoice, ok := gotBody["tool_choice"].(map[string]any)
	if !ok {
		t.Fatalf("tool_choice missing: %v", gotBody["tool_choice"])
	}
	if toolChoice["type"] != "tool" || toolChoice["name"] != "record_scam_analysis" {
		t.Errorf("tool_choice = %v, want {type:tool name:record_scam_analysis}", toolChoice)
	}
	tools, ok := gotBody["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %v, want exactly one", gotBody["tools"])
	}
}

func TestAnthropicGenerateStructured_TruncatedAtMaxTokensErrors(t *testing.T) {
	// Regression test: a response that hit the token limit can still carry a
	// tool_use block whose input JSON is structurally valid but missing
	// fields generated after the cutoff. This must be a hard error, not a
	// silent partial result — see internal/scam/enrich's enrichMaxTokens.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(anthropicMessageResponse("record_corpus_document", map[string]any{"title": "t", "content": "c"}, "max_tokens")))
	}))
	defer srv.Close()

	a := newTestAnthropic(t, srv, AnthropicConfig{APIKey: "test"})
	_, err := a.GenerateStructured(context.Background(), Request{User: "q", ToolName: "record_corpus_document"})
	if err == nil {
		t.Fatal("expected error for stop_reason=max_tokens, got nil")
	}
}

func TestAnthropicGenerateStructured_NoToolUseBlockErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_test", "type": "message", "role": "assistant",
			"model": "claude-haiku-4-5-20251001",
			"content": [{"type": "text", "text": "no tool call"}],
			"stop_reason": "end_turn", "stop_sequence": null,
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`))
	}))
	defer srv.Close()

	a := newTestAnthropic(t, srv, AnthropicConfig{APIKey: "test"})
	if _, err := a.GenerateStructured(context.Background(), Request{User: "q", ToolName: "record_scam_analysis"}); err == nil {
		t.Fatal("expected error when response has no tool_use block, got nil")
	}
}

func TestAnthropicGenerateStructured_NonOKStatusErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()

	a := newTestAnthropic(t, srv, AnthropicConfig{APIKey: "bad"})
	if _, err := a.GenerateStructured(context.Background(), Request{User: "q", ToolName: "record_scam_analysis"}); err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
}

func TestAnthropicGenerateStructured_EmptyToolNameErrorsWithoutHTTPCall(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	a := newTestAnthropic(t, srv, AnthropicConfig{APIKey: "test"})
	if _, err := a.GenerateStructured(context.Background(), Request{User: "q"}); err == nil {
		t.Fatal("expected error for empty tool name, got nil")
	}
	if called {
		t.Error("HTTP call made despite empty tool name")
	}
}

func TestNewAnthropic_DefaultsModel(t *testing.T) {
	a := NewAnthropic(AnthropicConfig{APIKey: "k"})
	if a.Model() != anthropicDefaultModel {
		t.Errorf("Model() = %q, want %q", a.Model(), anthropicDefaultModel)
	}
	a2 := NewAnthropic(AnthropicConfig{APIKey: "k", Model: "claude-sonnet-4-6"})
	if a2.Model() != "claude-sonnet-4-6" {
		t.Errorf("Model() = %q, want claude-sonnet-4-6", a2.Model())
	}
}
