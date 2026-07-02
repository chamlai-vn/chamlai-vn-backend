package reranker

import "context"

// Service abstracts over reranking providers (Voyage, Cohere, self-hosted).
// A cross-encoder reranker scores query and document together, so it can
// sharpen results a bi-encoder + rank-fusion pass already narrowed down.
type Service interface {
	// Rerank orders documents by relevance to query, best first, returning at
	// most topK results. Results reference documents by their index into the
	// given slice. topK <= 0 returns every document, reranked.
	Rerank(ctx context.Context, query string, documents []string, topK int) ([]Result, error)

	// Model returns the underlying model id, for logging + eval metadata.
	Model() string
}

// Result is one reranked document reference.
type Result struct {
	Index          int     // position in the documents slice passed to Rerank
	RelevanceScore float64 // provider score, higher = more relevant
}
