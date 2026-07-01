package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func (f *fakeRetriever) Retrieve(_ context.Context, query string, k int) ([]retriever.Result, error) {
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

func TestHandleAnalyze_OK(t *testing.T) {
	ret := &fakeRetriever{results: []retriever.Result{{Content: "pattern", ScamType: "x"}}}
	scorer := &fakeScorer{result: &analyzer.AnalysisResult{
		RiskLevel:  analyzer.RiskRed,
		RedFlags:   []string{"đặt cọc gấp"},
		Disclaimer: "chỉ mang tính tham khảo",
	}}
	h := New(ret, scorer, WithTopK(3))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/analyze", strings.NewReader(`{"text":"  đặt cọc 10 triệu  "}`))
	h.HandleAnalyze(rr, req)

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

func TestHandleAnalyze_BadInput(t *testing.T) {
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
			h := New(ret, scorer)

			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/analyze", strings.NewReader(tc.body))
			h.HandleAnalyze(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rr.Code)
			}
			if ret.gotK != 0 || scorer.gotText != "" {
				t.Error("collaborators were called on invalid input")
			}
		})
	}
}

func TestHandleAnalyze_PipelineError(t *testing.T) {
	ret := &fakeRetriever{err: errors.New("pgvector down")}
	scorer := &fakeScorer{}
	h := New(ret, scorer)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/analyze", strings.NewReader(`{"text":"abc"}`))
	h.HandleAnalyze(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
}

func TestHandleHealth(t *testing.T) {
	h := New(&fakeRetriever{}, &fakeScorer{})
	rr := httptest.NewRecorder()
	h.HandleHealth(rr, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if strings.TrimSpace(rr.Body.String()) != `{"status":"ok"}` {
		t.Errorf("body = %s", rr.Body.String())
	}
}
