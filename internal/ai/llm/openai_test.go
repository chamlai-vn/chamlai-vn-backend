package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openai/openai-go/option"
)

// openaiToolCallResponse is a minimal Chat Completions response carrying one
// forced tool call, enough for the SDK to decode.
func openaiToolCallResponse(name, args string) string {
	body := map[string]any{
		"id":      "chatcmpl-test",
		"object":  "chat.completion",
		"created": 0,
		"model":   "gpt-5.4-mini",
		"choices": []any{map[string]any{
			"index":         0,
			"finish_reason": "tool_calls",
			"message": map[string]any{
				"role":    "assistant",
				"content": nil,
				"tool_calls": []any{map[string]any{
					"id":       "call_1",
					"type":     "function",
					"function": map[string]any{"name": name, "arguments": args},
				}},
			},
		}},
	}
	b, _ := json.Marshal(body)
	return string(b)
}

func newTestOpenAI(t *testing.T, srv *httptest.Server, cfg OpenAIConfig) *OpenAI {
	t.Helper()
	return NewOpenAI(cfg, WithOpenAIRequestOptions(
		option.WithBaseURL(srv.URL),
		option.WithAPIKey("test"),
	))
}

func TestOpenAIGenerateStructured_SendsForcedToolCall(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openaiToolCallResponse("record_scam_analysis", `{"verdict":"red"}`)))
	}))
	defer srv.Close()

	o := newTestOpenAI(t, srv, OpenAIConfig{APIKey: "test"})
	_, err := o.GenerateStructured(context.Background(), Request{
		System:   "you score scams",
		User:     "nghi ngờ lừa đảo",
		ToolName: "record_scam_analysis",
		ToolDesc: "record the verdict",
		Schema:   json.RawMessage(`{"type":"object","properties":{"verdict":{"type":"string"}},"required":["verdict"]}`),
	})
	if err != nil {
		t.Fatalf("GenerateStructured: %v", err)
	}

	if gotBody["model"] != "gpt-5.4-mini" {
		t.Errorf("model = %v, want gpt-5.4-mini", gotBody["model"])
	}
	// Forced single tool call — tool_choice must name the tool (R2 parity).
	tc, ok := gotBody["tool_choice"].(map[string]any)
	if !ok {
		t.Fatalf("tool_choice not an object: %v", gotBody["tool_choice"])
	}
	fn, _ := tc["function"].(map[string]any)
	if tc["type"] != "function" || fn["name"] != "record_scam_analysis" {
		t.Errorf("tool_choice = %v, want forced function record_scam_analysis", tc)
	}
	tools, ok := gotBody["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %v, want exactly one", gotBody["tools"])
	}
	toolFn, _ := tools[0].(map[string]any)["function"].(map[string]any)
	if toolFn["name"] != "record_scam_analysis" {
		t.Errorf("tool function name = %v, want record_scam_analysis", toolFn["name"])
	}
	if _, ok := toolFn["parameters"].(map[string]any); !ok {
		t.Errorf("tool parameters schema missing: %v", toolFn["parameters"])
	}
	if _, ok := gotBody["max_completion_tokens"]; !ok {
		t.Errorf("max_completion_tokens missing from request body")
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 2 {
		t.Errorf("messages = %d, want 2 (system + user)", len(msgs))
	}
}

func TestOpenAIGenerateStructured_ReturnsToolArguments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openaiToolCallResponse("record_scam_analysis", `{"verdict":"green","confidence":0.8}`)))
	}))
	defer srv.Close()

	o := newTestOpenAI(t, srv, OpenAIConfig{APIKey: "test"})
	raw, err := o.GenerateStructured(context.Background(), Request{
		User:     "q",
		ToolName: "record_scam_analysis",
	})
	if err != nil {
		t.Fatalf("GenerateStructured: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal returned args: %v", err)
	}
	if got["verdict"] != "green" {
		t.Errorf("verdict = %v, want green", got["verdict"])
	}
}

func TestOpenAIGenerateStructured_NoToolCallErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","created":0,"model":"gpt-5.4-mini","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"no tool"}}]}`))
	}))
	defer srv.Close()

	o := newTestOpenAI(t, srv, OpenAIConfig{APIKey: "test"})
	if _, err := o.GenerateStructured(context.Background(), Request{User: "q", ToolName: "record_scam_analysis"}); err == nil {
		t.Fatal("expected error when response has no tool call, got nil")
	}
}

func TestOpenAIGenerateStructured_NonOKStatusErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()

	o := newTestOpenAI(t, srv, OpenAIConfig{APIKey: "bad"})
	if _, err := o.GenerateStructured(context.Background(), Request{User: "q", ToolName: "record_scam_analysis"}); err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
}

func TestOpenAIGenerateStructured_EmptyToolNameErrorsWithoutHTTPCall(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	o := newTestOpenAI(t, srv, OpenAIConfig{APIKey: "test"})
	if _, err := o.GenerateStructured(context.Background(), Request{User: "q"}); err == nil {
		t.Fatal("expected error for empty tool name, got nil")
	}
	if called {
		t.Error("HTTP call made despite empty tool name")
	}
}

func TestNewOpenAI_DefaultsModel(t *testing.T) {
	o := NewOpenAI(OpenAIConfig{APIKey: "k"})
	if o.Model() != "gpt-5.4-mini" {
		t.Errorf("Model() = %q, want gpt-5.4-mini", o.Model())
	}
	o2 := NewOpenAI(OpenAIConfig{APIKey: "k", Model: "gpt-5.4"})
	if o2.Model() != "gpt-5.4" {
		t.Errorf("Model() = %q, want gpt-5.4", o2.Model())
	}
}
