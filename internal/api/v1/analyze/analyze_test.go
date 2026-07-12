package analyze

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/problem"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/analyzer"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/retriever"
)

// fakeRetriever records the query it saw and returns canned results/error.
type fakeRetriever struct {
	gotQuery string
	gotK     int
	results  []retriever.Result
	err      error
}

func (f *fakeRetriever) HybridSearch(_ context.Context, query string, k int) ([]retriever.Result, error) {
	f.gotQuery, f.gotK = query, k
	return f.results, f.err
}

// fakeScorer records the text + chunks it saw and returns a canned verdict/error.
type fakeScorer struct {
	gotText   string
	gotChunks []retriever.Result
	result    *analyzer.AnalysisResult
	err       error
}

func (f *fakeScorer) Score(_ context.Context, text string, chunks []retriever.Result) (*analyzer.AnalysisResult, error) {
	f.gotText, f.gotChunks = text, chunks
	return f.result, f.err
}

// fakeBudget records how many times Reserve was called and returns a canned
// (ok, err). ok=true (budget available) by default, matching an allow-list
// zero value that "just works" for tests that don't care about budgeting.
type fakeBudget struct {
	calls int
	ok    bool
	err   error
}

func allowBudget() *fakeBudget { return &fakeBudget{ok: true} }

func (f *fakeBudget) Reserve(_ context.Context) (bool, error) {
	f.calls++
	return f.ok, f.err
}

func TestHandle_OK(t *testing.T) {
	ret := &fakeRetriever{results: []retriever.Result{{Content: "pattern", ScamType: "x"}}}
	scorer := &fakeScorer{result: &analyzer.AnalysisResult{
		RiskLevel:  analyzer.RiskRed,
		RedFlags:   []string{"đặt cọc gấp"},
		Disclaimer: "chỉ mang tính tham khảo",
	}}
	h := New(ret, scorer, allowBudget(), WithTopK(3))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(`{"text":"  đặt cọc 10 triệu  "}`))
	if err := h.Handle(rr, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	// text is trimmed before retrieval, and topK is threaded through.
	if ret.gotQuery != "đặt cọc 10 triệu" {
		t.Errorf("retriever query = %q, want trimmed text", ret.gotQuery)
	}
	if ret.gotK != 3 {
		t.Errorf("retriever k = %d, want 3", ret.gotK)
	}
	// the scorer receives the same trimmed text and the retrieved chunks.
	if scorer.gotText != "đặt cọc 10 triệu" || len(scorer.gotChunks) != 1 {
		t.Errorf("scorer got text=%q chunks=%d", scorer.gotText, len(scorer.gotChunks))
	}

	var got analyzer.AnalysisResult
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.RiskLevel != analyzer.RiskRed || len(got.RedFlags) != 1 {
		t.Errorf("body = %+v", got)
	}
}

func TestHandle_BadInput(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"malformed json", `{"text":`},
		{"empty text", `{"text":""}`},
		{"whitespace only", `{"text":"   "}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// collaborators must not be called on bad input.
			ret := &fakeRetriever{err: errors.New("should not be called")}
			scorer := &fakeScorer{err: errors.New("should not be called")}
			budget := &fakeBudget{err: errors.New("should not be called")}
			h := New(ret, scorer, budget)

			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(tc.body))
			err := h.Handle(rr, req)

			p, ok := err.(*problem.Problem)
			if !ok {
				t.Fatalf("err = %T (%v), want *problem.Problem", err, err)
			}
			if p.Status != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", p.Status)
			}
			if budget.calls != 0 || ret.gotK != 0 || scorer.gotText != "" {
				t.Error("collaborators were called on invalid input")
			}
		})
	}
}

func TestHandle_PipelineError(t *testing.T) {
	ret := &fakeRetriever{err: errors.New("pgvector down")}
	scorer := &fakeScorer{}
	h := New(ret, scorer, allowBudget())

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(`{"text":"abc"}`))
	err := h.Handle(rr, req)

	p, ok := err.(*problem.Problem)
	if !ok {
		t.Fatalf("err = %T (%v), want *problem.Problem", err, err)
	}
	if p.Status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", p.Status)
	}
}

func TestHandle_BudgetExhausted(t *testing.T) {
	// ok=false, err=nil: daily cap reached, not a store failure.
	ret := &fakeRetriever{err: errors.New("should not be called")}
	scorer := &fakeScorer{err: errors.New("should not be called")}
	budget := &fakeBudget{ok: false}
	h := New(ret, scorer, budget)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(`{"text":"abc"}`))
	err := h.Handle(rr, req)

	p, ok := err.(*problem.Problem)
	if !ok {
		t.Fatalf("err = %T (%v), want *problem.Problem", err, err)
	}
	if p.Status != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", p.Status)
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Error("want Retry-After header set")
	}
	if ret.gotK != 0 || scorer.gotText != "" {
		t.Error("pipeline was called despite exhausted budget")
	}
}

func TestHandle_BudgetStoreError_FailsClosed(t *testing.T) {
	ret := &fakeRetriever{err: errors.New("should not be called")}
	scorer := &fakeScorer{err: errors.New("should not be called")}
	budget := &fakeBudget{err: errors.New("connection refused")}
	h := New(ret, scorer, budget)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(`{"text":"abc"}`))
	err := h.Handle(rr, req)

	p, ok := err.(*problem.Problem)
	if !ok {
		t.Fatalf("err = %T (%v), want *problem.Problem", err, err)
	}
	if p.Status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", p.Status)
	}
	if ret.gotK != 0 || scorer.gotText != "" {
		t.Error("pipeline was called despite budget store error (must fail closed)")
	}
}
