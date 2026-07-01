package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRouter_Wiring exercises the real chi router + middleware stack (not the
// handler methods in isolation): method routing, /health, and /analyze
// validation all through NewRouter — no DB/LLM required.
func TestRouter_Wiring(t *testing.T) {
	h := New(&fakeRetriever{}, &fakeScorer{})
	srv := httptest.NewServer(NewRouter(h))
	defer srv.Close()

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"health ok", http.MethodGet, "/health", "", http.StatusOK},
		{"analyze empty text → 400", http.MethodPost, "/analyze", `{"text":""}`, http.StatusBadRequest},
		{"wrong method on analyze → 405", http.MethodGet, "/analyze", "", http.StatusMethodNotAllowed},
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
