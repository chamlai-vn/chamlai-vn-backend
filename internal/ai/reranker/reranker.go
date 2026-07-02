// Package reranker scores a query against a candidate list of documents with
// a cross-encoder, sharpening the order a bi-encoder + rank-fusion pass already
// produced (retriever.HybridSearch runs it as an optional stage after RRF).
//
// The Service interface (service.go) is the contract the rest of the codebase
// depends on. Concrete providers live in sibling files (voyage.go); New picks
// one from Config so swapping provider/model is a config change, not a code
// change.
//
// Adding a provider:
//  1. Add a <Provider>Config struct here and a field for it on Config, plus a
//     Provider constant.
//  2. Add <provider>.go with the Service impl and a constructor of the canonical
//     shape New<Provider>(cfg <Provider>Config, opts ...<Provider>Option). Apply
//     zero-value defaults inside the constructor; keep Option types per provider
//     (do not share one global Option type).
//  3. Add a case to New.
package reranker

import "fmt"

// Provider selects which reranking backend New constructs.
type Provider string

const (
	ProviderVoyage Provider = "voyage" // Voyage AI (rerank-2.5, rerank-2.5-lite)
)

// VoyageConfig holds the Voyage AI connection settings (from env).
type VoyageConfig struct {
	APIKey string // VOYAGE_API_KEY
	Model  string // optional; "" → rerank-2.5
}

// Config selects and configures a reranking provider.
type Config struct {
	Provider Provider
	Voyage   VoyageConfig
}

// New builds the Service for the configured provider.
func New(cfg Config) (Service, error) {
	switch cfg.Provider {
	case ProviderVoyage:
		return NewVoyage(cfg.Voyage), nil

	default:
		return nil, fmt.Errorf("reranker: unknown provider %q", cfg.Provider)
	}
}
