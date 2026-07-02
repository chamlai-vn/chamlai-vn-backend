package reranker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// withTestEndpoint points voyageEndpoint at srv for the duration of the test.
func withTestEndpoint(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := voyageEndpoint
	voyageEndpoint = srv.URL
	t.Cleanup(func() { voyageEndpoint = orig })
}

func TestVoyageRerank_SendsRequestBody(t *testing.T) {
	var gotBody voyageRerankRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"relevance_score":0.9}]}`))
	}))
	defer srv.Close()
	withTestEndpoint(t, srv)

	v := NewVoyage(VoyageConfig{APIKey: "key"})
	if _, err := v.Rerank(context.Background(), "nghi ngờ lừa đảo", []string{"doc a"}, 5); err != nil {
		t.Fatalf("Rerank: %v", err)
	}

	if gotBody.Query != "nghi ngờ lừa đảo" {
		t.Errorf("query = %q, want %q", gotBody.Query, "nghi ngờ lừa đảo")
	}
	if gotBody.Model != voyageDefaultModel {
		t.Errorf("model = %q, want %q", gotBody.Model, voyageDefaultModel)
	}
	if len(gotBody.Documents) != 1 || gotBody.Documents[0] != "doc a" {
		t.Errorf("documents = %v, want [doc a]", gotBody.Documents)
	}
	if gotBody.TopK != 5 {
		t.Errorf("top_k = %d, want 5", gotBody.TopK)
	}
}

func TestVoyageRerank_OmitsTopKWhenNonPositive(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	withTestEndpoint(t, srv)

	v := NewVoyage(VoyageConfig{APIKey: "key"})
	if _, err := v.Rerank(context.Background(), "q", []string{"doc a"}, 0); err != nil {
		t.Fatalf("Rerank: %v", err)
	}

	if _, ok := gotBody["top_k"]; ok {
		t.Errorf("top_k present in request body, want omitted for topK<=0")
	}
}

func TestVoyageRerank_MapsResponseInOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":2,"relevance_score":0.95},{"index":0,"relevance_score":0.4}]}`))
	}))
	defer srv.Close()
	withTestEndpoint(t, srv)

	v := NewVoyage(VoyageConfig{APIKey: "key"})
	results, err := v.Rerank(context.Background(), "q", []string{"a", "b", "c"}, 2)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Index != 2 || results[0].RelevanceScore != 0.95 {
		t.Errorf("results[0] = %+v, want {Index:2 RelevanceScore:0.95}", results[0])
	}
	if results[1].Index != 0 || results[1].RelevanceScore != 0.4 {
		t.Errorf("results[1] = %+v, want {Index:0 RelevanceScore:0.4}", results[1])
	}
}

func TestVoyageRerank_OutOfRangeIndexErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":5,"relevance_score":0.5}]}`))
	}))
	defer srv.Close()
	withTestEndpoint(t, srv)

	v := NewVoyage(VoyageConfig{APIKey: "key"})
	if _, err := v.Rerank(context.Background(), "q", []string{"a", "b"}, 5); err == nil {
		t.Fatal("expected out-of-range index error, got nil")
	}
}

func TestVoyageRerank_DuplicateIndexErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"relevance_score":0.5},{"index":0,"relevance_score":0.4}]}`))
	}))
	defer srv.Close()
	withTestEndpoint(t, srv)

	v := NewVoyage(VoyageConfig{APIKey: "key"})
	if _, err := v.Rerank(context.Background(), "q", []string{"a", "b"}, 5); err == nil {
		t.Fatal("expected duplicate index error, got nil")
	}
}

func TestVoyageRerank_NonOKStatusErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"invalid api key"}`))
	}))
	defer srv.Close()
	withTestEndpoint(t, srv)

	v := NewVoyage(VoyageConfig{APIKey: "bad"})
	if _, err := v.Rerank(context.Background(), "q", []string{"a"}, 5); err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
}

func TestVoyageRerank_EmptyDocumentsErrorsWithoutHTTPCall(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()
	withTestEndpoint(t, srv)

	v := NewVoyage(VoyageConfig{APIKey: "key"})
	if _, err := v.Rerank(context.Background(), "q", nil, 5); err == nil {
		t.Fatal("expected error for empty documents, got nil")
	}
	if called {
		t.Error("HTTP call made despite empty documents")
	}
}

func TestNewVoyage_DefaultsModel(t *testing.T) {
	v := NewVoyage(VoyageConfig{APIKey: "key"})
	if v.Model() != "rerank-2.5" {
		t.Errorf("Model() = %q, want rerank-2.5", v.Model())
	}

	v2 := NewVoyage(VoyageConfig{APIKey: "key", Model: "rerank-2.5-lite"})
	if v2.Model() != "rerank-2.5-lite" {
		t.Errorf("Model() = %q, want rerank-2.5-lite", v2.Model())
	}
}
