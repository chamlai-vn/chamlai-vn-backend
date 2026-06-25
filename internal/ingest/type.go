package ingest

// Document is a raw scam-warning article to index, before chunking/embedding.
type Document struct {
	URL      string
	Title    string
	Content  string
	ScamType string
	Source   string
}

// Result reports what IndexDocument stored.
type Result struct {
	DocID  int64
	Chunks int
}
