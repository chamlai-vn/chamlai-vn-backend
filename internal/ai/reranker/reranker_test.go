package reranker

import "testing"

func TestNew_UnknownProviderErrors(t *testing.T) {
	if _, err := New(Config{Provider: "nonexistent"}); err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}
}

func TestNew_Voyage(t *testing.T) {
	svc, err := New(Config{Provider: ProviderVoyage, Voyage: VoyageConfig{APIKey: "key"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if svc.Model() != "rerank-2.5" {
		t.Errorf("Model() = %q, want rerank-2.5", svc.Model())
	}
}
