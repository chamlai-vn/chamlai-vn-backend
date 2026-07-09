package retriever

// Result is one retrieved scam-pattern document and how relevant it is to
// the query. Doc-centric: with multi-representation embedding a document can
// be matched via several of its chunks (its content and/or a doc2query
// "# User query" line); those all collapse to a single Result per document
// before this is returned (see collapseToDocuments and HybridSearch's
// per-arm collapse). Content is always the document's full body
// (documents.content) — even when the chunk that matched was a
// kind=query vector — so downstream consumers (analyzer, reranker) are
// always grounded in the actual scam-warning text, never in a victim-style
// question that happened to retrieve it.
type Result struct {
	DocumentID int64
	Title      string
	Content    string
	Prevention string
	ScamType   string
	SourceURL  string  // documents.url — source article URL for attribution
	Score      float64 // meaning depends on the code path — see Retrieve/HybridSearch doc comments
}
