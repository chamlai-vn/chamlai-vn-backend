package retriever

// Result is one retrieved scam-pattern chunk and how similar it is to the query.
type Result struct {
	ChunkID    int64
	DocumentID int64
	Content    string
	ScamType   string
	SourceURL  string  // source article URL for attribution
	Score      float64 // 1 - cosine distance, clamped to [0,1]
}
