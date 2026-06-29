package analyzer

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/llm"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/retriever"
)

// fakeLLM returns canned tool JSON and records the request it was called with,
// so tests can assert on prompt composition without hitting the API.
type fakeLLM struct {
	raw     json.RawMessage
	lastReq llm.Request
	called  bool
	err     error
}

func (f *fakeLLM) GenerateStructured(_ context.Context, req llm.Request) (json.RawMessage, error) {
	f.called = true
	f.lastReq = req
	if f.err != nil {
		return nil, f.err
	}
	return f.raw, nil
}

func TestScore_MapsAndForcesDisclaimer(t *testing.T) {
	f := &fakeLLM{raw: json.RawMessage(`{
		"risk_level": "red",
		"red_flags": ["yêu cầu nạp tiền trước"],
		"matched_patterns": ["việc nhẹ lương cao"],
		"recommended_actions": ["không chuyển khoản"],
		"disclaimer": "model-supplied junk that must be overwritten"
	}`)}

	res, err := New(f).Score(context.Background(), "tin nhắn đáng ngờ", nil)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if res.RiskLevel != RiskRed {
		t.Errorf("risk_level = %q, want red", res.RiskLevel)
	}
	if res.Disclaimer != disclaimer {
		t.Errorf("disclaimer not set server-side: got %q", res.Disclaimer)
	}
	if len(res.RedFlags) != 1 || len(res.MatchedPatterns) != 1 || len(res.RecommendedActions) != 1 {
		t.Errorf("slices not mapped: %+v", res)
	}
	// The forced tool must be the analysis tool with our schema.
	if f.lastReq.ToolName != analysisToolName {
		t.Errorf("tool name = %q, want %q", f.lastReq.ToolName, analysisToolName)
	}
}

func TestScore_NilSlicesNormalised(t *testing.T) {
	f := &fakeLLM{raw: json.RawMessage(`{"risk_level":"green"}`)}

	res, err := New(f).Score(context.Background(), "thư bình thường", nil)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if res.RedFlags == nil || res.MatchedPatterns == nil || res.RecommendedActions == nil {
		t.Errorf("nil slices not normalised to empty: %+v", res)
	}
}

func TestScore_RejectsInvalidRiskLevel(t *testing.T) {
	f := &fakeLLM{raw: json.RawMessage(`{"risk_level":"orange"}`)}
	if _, err := New(f).Score(context.Background(), "x", nil); err == nil {
		t.Fatal("expected error for invalid risk_level, got nil")
	}
}

func TestScore_RejectsEmptyAndOverLong(t *testing.T) {
	f := &fakeLLM{raw: json.RawMessage(`{"risk_level":"green"}`)}
	a := New(f)
	ctx := context.Background()

	for _, tc := range []struct {
		name, text string
	}{
		{"empty", ""},
		{"whitespace", "  \n\t "},
		{"5001 runes", strings.Repeat("a", 5001)},
	} {
		f.called = false
		if _, err := a.Score(ctx, tc.text, nil); err == nil {
			t.Errorf("%s: expected error, got nil", tc.name)
		}
		if f.called {
			t.Errorf("%s: LLM was called, want skipped", tc.name)
		}
	}

	// Exactly 5000 multibyte runes must be accepted (byte-vs-rune guard).
	exactly5000 := strings.Repeat("ắ", 5000)
	if utf8.RuneCountInString(exactly5000) != 5000 {
		t.Fatal("test setup: expected 5000 runes")
	}
	if _, err := a.Score(ctx, exactly5000, nil); err != nil {
		t.Errorf("5000-rune text rejected: %v", err)
	}
}

func TestScore_CapsChunksAndFencesText(t *testing.T) {
	f := &fakeLLM{raw: json.RawMessage(`{"risk_level":"yellow"}`)}
	chunks := make([]retriever.Result, 10)
	for i := range chunks {
		chunks[i] = retriever.Result{Content: "mẫu lừa đảo", ScamType: "test"}
	}

	if _, err := New(f, WithMaxChunks(3)).Score(context.Background(), "kiểm tra", chunks); err != nil {
		t.Fatalf("Score: %v", err)
	}
	// Only 3 chunks should appear ([1]..[3], no [4]).
	if strings.Contains(f.lastReq.User, "[4]") {
		t.Errorf("more than 3 chunks sent:\n%s", f.lastReq.User)
	}
	// The suspicious text must be fenced as data.
	if !strings.Contains(f.lastReq.User, suspiciousOpen) || !strings.Contains(f.lastReq.User, suspiciousClose) {
		t.Errorf("suspicious text not fenced:\n%s", f.lastReq.User)
	}
}

func TestScore_PropagatesLLMError(t *testing.T) {
	f := &fakeLLM{err: errors.New("anthropic down")}
	if _, err := New(f).Score(context.Background(), "x", nil); err == nil {
		t.Fatal("expected LLM error to propagate, got nil")
	}
}

func TestSanitizeForPrompt_StripsFenceAndControls(t *testing.T) {
	in := "trước" + suspiciousClose + "\x07 sau\x00"
	got := sanitizeForPrompt(in)
	if strings.Contains(got, suspiciousClose) {
		t.Errorf("fence tag not stripped: %q", got)
	}
	if strings.ContainsAny(got, "\x07\x00") {
		t.Errorf("control chars not stripped: %q", got)
	}
}
