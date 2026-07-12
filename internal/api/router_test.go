package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/time/rate"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/v1/analyze"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/analyzer"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/retriever"
)

type fakeRetriever struct{}

func (fakeRetriever) HybridSearch(_ context.Context, _ string, _ int) ([]retriever.Result, error) {
	return nil, nil
}

type fakeScorer struct{}

func (fakeScorer) Score(_ context.Context, _ string, _ []retriever.Result) (*analyzer.AnalysisResult, error) {
	return &analyzer.AnalysisResult{RiskLevel: analyzer.RiskGreen}, nil
}

// fakeBudget always grants budget — these tests exercise the HTTP/router
// layer, not the budget gate itself (see analyze_test.go for that).
type fakeBudget struct{}

func (fakeBudget) Reserve(_ context.Context) (bool, error) { return true, nil }

func testConfig() Config {
	return Config{AllowOrigins: []string{"https://chamlai.vn"}, BodyLimitBytes: 64 * 1024}
}

// TestRouter_Wiring exercises the real chi router + middleware stack (not the
// handler methods in isolation): method routing, /health, /v1/analyze
// validation, and error shape all through NewRouter — no DB/LLM required.
func TestRouter_Wiring(t *testing.T) {
	h := analyze.New(fakeRetriever{}, fakeScorer{}, fakeBudget{})
	srv := httptest.NewServer(NewRouter(testConfig(), Handlers{Analyze: h}))
	defer srv.Close()

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"health ok", http.MethodGet, "/health", "", http.StatusOK},
		{"analyze empty text → 400", http.MethodPost, "/v1/analyze", `{"text":""}`, http.StatusBadRequest},
		{"analyze ok → 200", http.MethodPost, "/v1/analyze", `{"text":"đặt cọc gấp"}`, http.StatusOK},
		{"old unversioned analyze → 404", http.MethodPost, "/analyze", `{"text":"x"}`, http.StatusNotFound},
		{"wrong method on analyze → 405", http.MethodGet, "/v1/analyze", "", http.StatusMethodNotAllowed},
		{"unknown route → 404", http.MethodGet, "/nope", "", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, srv.URL+tc.path, strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}

func TestRouter_ErrorsAreProblemJSON(t *testing.T) {
	h := analyze.New(fakeRetriever{}, fakeScorer{}, fakeBudget{})
	srv := httptest.NewServer(NewRouter(testConfig(), Handlers{Analyze: h}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json; charset=utf-8" {
		t.Errorf("content-type = %q", ct)
	}
}

func TestRouter_EchoesRequestID(t *testing.T) {
	h := analyze.New(fakeRetriever{}, fakeScorer{}, fakeBudget{})
	srv := httptest.NewServer(NewRouter(testConfig(), Handlers{Analyze: h}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/health", nil)
	req.Header.Set("X-Request-Id", "test-id-abc")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("X-Request-Id"); got != "test-id-abc" {
		t.Errorf("X-Request-Id = %q, want echoed", got)
	}
}

func TestRouter_BodyOverLimit_413(t *testing.T) {
	h := analyze.New(fakeRetriever{}, fakeScorer{}, fakeBudget{})
	cfg := Config{AllowOrigins: []string{"*"}, BodyLimitBytes: 16}
	srv := httptest.NewServer(NewRouter(cfg, Handlers{Analyze: h}))
	defer srv.Close()

	body := `{"text":"this body is longer than sixteen bytes"}`
	resp, err := http.Post(srv.URL+"/v1/analyze", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.StatusCode)
	}
}

func TestRouter_SwaggerUI_GatedByConfig(t *testing.T) {
	h := analyze.New(fakeRetriever{}, fakeScorer{}, fakeBudget{})

	off := httptest.NewServer(NewRouter(testConfig(), Handlers{Analyze: h}))
	defer off.Close()
	resp, err := http.Get(off.URL + "/swagger/index.html")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("SwaggerUI=false: status = %d, want 404", resp.StatusCode)
	}

	cfg := testConfig()
	cfg.SwaggerUI = true
	on := httptest.NewServer(NewRouter(cfg, Handlers{Analyze: h}))
	defer on.Close()
	resp, err = http.Get(on.URL + "/swagger/doc.json")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("SwaggerUI=true: status = %d, want 200", resp.StatusCode)
	}
}

func TestRouter_CORSPreflight(t *testing.T) {
	h := analyze.New(fakeRetriever{}, fakeScorer{}, fakeBudget{})
	srv := httptest.NewServer(NewRouter(testConfig(), Handlers{Analyze: h}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodOptions, srv.URL+"/v1/analyze", nil)
	req.Header.Set("Origin", "https://chamlai.vn")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://chamlai.vn" {
		t.Errorf("Access-Control-Allow-Origin = %q", got)
	}
}

// TestRouter_RateLimitAppliesToV1Only exercises the real router (not the
// middleware in isolation): the /v1 group is throttled once its burst is
// exhausted, but /health — a liveness/readiness probe that must never be
// throttled — keeps returning 200 regardless.
func TestRouter_RateLimitAppliesToV1Only(t *testing.T) {
	h := analyze.New(fakeRetriever{}, fakeScorer{}, fakeBudget{})
	cfg := testConfig()
	cfg.RateLimitRPS = rate.Limit(1.0 / 3600) // effectively burst-only within this test
	srv := httptest.NewServer(NewRouter(cfg, Handlers{Analyze: h}))
	defer srv.Close()

	// http.DefaultClient reuses one keep-alive connection to srv here, so
	// every request below shares the same observed RemoteAddr/bucket.
	var last *http.Response
	for i := 0; i < 30; i++ {
		resp, err := http.Post(srv.URL+"/v1/analyze", "application/json", strings.NewReader(`{"text":"ok"}`))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		last = resp
	}
	if last.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("/v1/analyze after burst: status = %d, want 429", last.StatusCode)
	}

	for i := 0; i < 30; i++ {
		resp, err := http.Get(srv.URL + "/health")
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("/health request %d: status = %d, want 200 (never rate-limited)", i, resp.StatusCode)
		}
	}
}
