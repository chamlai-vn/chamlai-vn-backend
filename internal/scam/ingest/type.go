package ingest

// Result reports what IndexDocument stored.
type Result struct {
	DocID  int64
	Chunks int
}
