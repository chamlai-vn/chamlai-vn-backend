package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/genai"
)

// geminiFunctionCallResponse is a minimal GenerateContent response carrying one
// function call, enough for the SDK to decode.
func geminiFunctionCallResponse(name string, args map[string]any) string {
	body := map[string]any{
		"candidates": []any{map[string]any{
			"content": map[string]any{
				"role":  "model",
				"parts": []any{map[string]any{"functionCall": map[string]any{"name": name, "args": args}}},
			},
			"finishReason": "STOP",
		}},
	}
	b, _ := json.Marshal(body)
	return string(b)
}

func newTestGemini(t *testing.T, srv *httptest.Server, cfg GeminiConfig) *Gemini {
	t.Helper()
	g, err := NewGemini(cfg, WithGeminiClientConfig(func(cc *genai.ClientConfig) {
		cc.HTTPOptions.BaseURL = srv.URL
	}))
	if err != nil {
		t.Fatalf("NewGemini: %v", err)
	}
	return g
}

func TestGeminiGenerateStructured_SendsForcedFunctionCall(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(geminiFunctionCallResponse("record_scam_analysis", map[string]any{"verdict": "red"})))
	}))
	defer srv.Close()

	g := newTestGemini(t, srv, GeminiConfig{APIKey: "test"})
	_, err := g.GenerateStructured(context.Background(), Request{
		System:   "you score scams",
		User:     "nghi ngờ lừa đảo",
		ToolName: "record_scam_analysis",
		ToolDesc: "record the verdict",
		Schema:   json.RawMessage(`{"type":"object","properties":{"verdict":{"type":"string"}},"required":["verdict"]}`),
	})
	if err != nil {
		t.Fatalf("GenerateStructured: %v", err)
	}

	// Forced single function call — mode ANY + allowedFunctionNames (R2 parity).
	toolConfig, ok := gotBody["toolConfig"].(map[string]any)
	if !ok {
		t.Fatalf("toolConfig missing: %v", gotBody["toolConfig"])
	}
	fcc, _ := toolConfig["functionCallingConfig"].(map[string]any)
	if fcc["mode"] != "ANY" {
		t.Errorf("functionCallingConfig.mode = %v, want ANY", fcc["mode"])
	}
	allowed, _ := fcc["allowedFunctionNames"].([]any)
	if len(allowed) != 1 || allowed[0] != "record_scam_analysis" {
		t.Errorf("allowedFunctionNames = %v, want [record_scam_analysis]", allowed)
	}
	tools, ok := gotBody["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %v, want exactly one", gotBody["tools"])
	}
	decls, _ := tools[0].(map[string]any)["functionDeclarations"].([]any)
	if len(decls) != 1 || decls[0].(map[string]any)["name"] != "record_scam_analysis" {
		t.Errorf("functionDeclarations = %v, want one named record_scam_analysis", decls)
	}
}

func TestGeminiGenerateStructured_ReturnsFunctionArgs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(geminiFunctionCallResponse("record_scam_analysis", map[string]any{"verdict": "green", "confidence": 0.8})))
	}))
	defer srv.Close()

	g := newTestGemini(t, srv, GeminiConfig{APIKey: "test"})
	raw, err := g.GenerateStructured(context.Background(), Request{User: "q", ToolName: "record_scam_analysis"})
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

func TestGeminiGenerateStructured_TruncatedAtMaxTokensErrors(t *testing.T) {
	// Regression test: Gemini rejects a function call truncated at the token
	// limit as finish_reason=MAX_TOKENS before it even reaches
	// MALFORMED_FUNCTION_CALL — surface that specific, actionable cause. See
	// internal/scam/enrich's enrichMaxTokens for the bug this guards.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"partial"}]},"finishReason":"MAX_TOKENS"}]}`))
	}))
	defer srv.Close()

	g := newTestGemini(t, srv, GeminiConfig{APIKey: "test"})
	if _, err := g.GenerateStructured(context.Background(), Request{User: "q", ToolName: "record_corpus_document"}); err == nil {
		t.Fatal("expected error for finish_reason=MAX_TOKENS, got nil")
	}
}

func TestGeminiGenerateStructured_MalformedFunctionCallErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"MALFORMED_FUNCTION_CALL"}]}`))
	}))
	defer srv.Close()

	g := newTestGemini(t, srv, GeminiConfig{APIKey: "test"})
	if _, err := g.GenerateStructured(context.Background(), Request{User: "q", ToolName: "record_corpus_document"}); err == nil {
		t.Fatal("expected error for finish_reason=MALFORMED_FUNCTION_CALL, got nil")
	}
}

func TestGeminiGenerateStructured_NoFunctionCallErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"no call"}]},"finishReason":"STOP"}]}`))
	}))
	defer srv.Close()

	g := newTestGemini(t, srv, GeminiConfig{APIKey: "test"})
	if _, err := g.GenerateStructured(context.Background(), Request{User: "q", ToolName: "record_scam_analysis"}); err == nil {
		t.Fatal("expected error when response has no function call, got nil")
	}
}

func TestGeminiGenerateStructured_NonOKStatusErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()

	g := newTestGemini(t, srv, GeminiConfig{APIKey: "bad"})
	if _, err := g.GenerateStructured(context.Background(), Request{User: "q", ToolName: "record_scam_analysis"}); err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
}

func TestGeminiGenerateStructured_EmptyToolNameErrorsWithoutHTTPCall(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	g := newTestGemini(t, srv, GeminiConfig{APIKey: "test"})
	if _, err := g.GenerateStructured(context.Background(), Request{User: "q"}); err == nil {
		t.Fatal("expected error for empty tool name, got nil")
	}
	if called {
		t.Error("HTTP call made despite empty tool name")
	}
}

func TestNewGemini_DefaultsModel(t *testing.T) {
	g, err := NewGemini(GeminiConfig{APIKey: "k"})
	if err != nil {
		t.Fatalf("NewGemini: %v", err)
	}
	if g.Model() != "gemini-3.5-flash" {
		t.Errorf("Model() = %q, want gemini-3.5-flash", g.Model())
	}
	g2, err := NewGemini(GeminiConfig{APIKey: "k", Model: "gemini-3.5-pro"})
	if err != nil {
		t.Fatalf("NewGemini: %v", err)
	}
	if g2.Model() != "gemini-3.5-pro" {
		t.Errorf("Model() = %q, want gemini-3.5-pro", g2.Model())
	}
}
