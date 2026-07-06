package chat

import (
	"context"
	"errors"
	"testing"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/analyzer"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/retriever"
)

type fakeClassifier struct {
	called     bool
	gotLatest  string
	intent     Intent
	confidence float64
	err        error
}

func (f *fakeClassifier) Classify(_ context.Context, _ []Message, latest string) (Intent, float64, error) {
	f.called = true
	f.gotLatest = latest
	return f.intent, f.confidence, f.err
}

type fakeRetriever struct {
	called  bool
	gotK    int
	results []retriever.Result
	err     error
}

func (f *fakeRetriever) HybridSearch(_ context.Context, _ string, k int) ([]retriever.Result, error) {
	f.called = true
	f.gotK = k
	return f.results, f.err
}

type fakeScorer struct {
	called  bool
	gotText string
	result  *analyzer.AnalysisResult
	err     error
}

func (f *fakeScorer) Score(_ context.Context, text string, _ []retriever.Result) (*analyzer.AnalysisResult, error) {
	f.called = true
	f.gotText = text
	return f.result, f.err
}

type fakeAnswerer struct {
	called    bool
	gotIntent Intent
	reply     string
	err       error
}

func (f *fakeAnswerer) Answer(_ context.Context, intent Intent, _ []Message, _ string) (string, error) {
	f.called = true
	f.gotIntent = intent
	return f.reply, f.err
}

func userMsg(content string) ChatRequest {
	return ChatRequest{Messages: []Message{{Role: RoleUser, Content: content}}}
}

func greenVerdict() *analyzer.AnalysisResult {
	return &analyzer.AnalysisResult{RiskLevel: analyzer.RiskGreen}
}

func TestReply_SuspiciousShortCircuitsToScam(t *testing.T) {
	cls := &fakeClassifier{intent: IntentProject, confidence: 1.0} // would say project…
	ret := &fakeRetriever{results: nil}
	sc := &fakeScorer{result: greenVerdict()}
	ans := &fakeAnswerer{}
	c := New(cls, ret, sc, ans)

	// contains a link → looksSuspicious → scam, regardless of the classifier.
	resp, err := c.Reply(context.Background(), userMsg("bấm vào http://bit.ly/x để nhận quà"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Intent != IntentScam {
		t.Errorf("intent = %q, want scam", resp.Intent)
	}
	if cls.called {
		t.Error("classifier was called despite suspicious short-circuit")
	}
	if !ret.called || !sc.called {
		t.Error("scam pipeline not run")
	}
	if resp.Analysis == nil {
		t.Error("analysis missing on scam turn")
	}
}

func TestReply_ClassifierScam(t *testing.T) {
	cls := &fakeClassifier{intent: IntentScam, confidence: 0.9}
	ret := &fakeRetriever{}
	sc := &fakeScorer{result: greenVerdict()}
	ans := &fakeAnswerer{}
	c := New(cls, ret, sc, ans, WithTopK(3))

	resp, err := c.Reply(context.Background(), userMsg("cái này có đáng tin không nhỉ"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Intent != IntentScam || resp.Analysis == nil {
		t.Errorf("resp = %+v, want scam with analysis", resp)
	}
	if ret.gotK != 3 {
		t.Errorf("topK = %d, want 3", ret.gotK)
	}
	if ans.called {
		t.Error("answerer should not run on scam turn")
	}
}

func TestReply_ProjectAndFounder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		intent Intent
	}{
		{"project", IntentProject},
		{"founder", IntentFounder},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cls := &fakeClassifier{intent: tc.intent, confidence: 0.95}
			ret := &fakeRetriever{}
			sc := &fakeScorer{}
			ans := &fakeAnswerer{reply: "câu trả lời"}
			c := New(cls, ret, sc, ans)

			resp, err := c.Reply(context.Background(), userMsg("ai làm ra cái này vậy"))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Intent != tc.intent || resp.Reply != "câu trả lời" {
				t.Errorf("resp = %+v", resp)
			}
			if resp.Analysis != nil {
				t.Error("analysis must be nil on non-scam turn")
			}
			if ans.gotIntent != tc.intent {
				t.Errorf("answerer intent = %q", ans.gotIntent)
			}
			if ret.called || sc.called {
				t.Error("scam pipeline should not run on non-scam turn")
			}
		})
	}
}

func TestReply_LowConfidenceFailsSafeToScam(t *testing.T) {
	cls := &fakeClassifier{intent: IntentProject, confidence: 0.4} // below default 0.7
	ret := &fakeRetriever{}
	sc := &fakeScorer{result: greenVerdict()}
	ans := &fakeAnswerer{}
	c := New(cls, ret, sc, ans)

	resp, err := c.Reply(context.Background(), userMsg("một câu bình thường"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Intent != IntentScam {
		t.Errorf("intent = %q, want scam (fail-safe)", resp.Intent)
	}
	if ans.called {
		t.Error("answerer ran despite low confidence")
	}
}

func TestReply_ClassifierErrorFailsSafeToScam(t *testing.T) {
	cls := &fakeClassifier{err: errors.New("llm down")}
	ret := &fakeRetriever{}
	sc := &fakeScorer{result: greenVerdict()}
	ans := &fakeAnswerer{}
	c := New(cls, ret, sc, ans)

	resp, err := c.Reply(context.Background(), userMsg("một câu bình thường"))
	if err != nil {
		t.Fatalf("router error must not abort; got %v", err)
	}
	if resp.Intent != IntentScam || !sc.called {
		t.Errorf("expected fail-safe scam; resp=%+v scorerCalled=%v", resp, sc.called)
	}
}

func TestReply_PipelineErrorsPropagate(t *testing.T) {
	t.Run("retriever", func(t *testing.T) {
		cls := &fakeClassifier{intent: IntentScam, confidence: 1}
		c := New(cls, &fakeRetriever{err: errors.New("pgvector down")}, &fakeScorer{}, &fakeAnswerer{})
		if _, err := c.Reply(context.Background(), userMsg("abc")); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("scorer", func(t *testing.T) {
		cls := &fakeClassifier{intent: IntentScam, confidence: 1}
		c := New(cls, &fakeRetriever{}, &fakeScorer{err: errors.New("claude down")}, &fakeAnswerer{})
		if _, err := c.Reply(context.Background(), userMsg("abc")); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("answerer", func(t *testing.T) {
		cls := &fakeClassifier{intent: IntentProject, confidence: 1}
		c := New(cls, &fakeRetriever{}, &fakeScorer{}, &fakeAnswerer{err: errors.New("claude down")})
		if _, err := c.Reply(context.Background(), userMsg("ai làm cái này")); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestReply_InvalidConversation(t *testing.T) {
	c := New(&fakeClassifier{}, &fakeRetriever{}, &fakeScorer{}, &fakeAnswerer{})
	cases := map[string]ChatRequest{
		"empty":          {Messages: nil},
		"last assistant": {Messages: []Message{{Role: RoleUser, Content: "hi"}, {Role: RoleAssistant, Content: "hello"}}},
		"blank content":  {Messages: []Message{{Role: RoleUser, Content: "   "}}},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := c.Reply(context.Background(), req)
			if !errors.Is(err, ErrNoUserMessage) {
				t.Fatalf("err = %v, want ErrNoUserMessage", err)
			}
		})
	}
}
