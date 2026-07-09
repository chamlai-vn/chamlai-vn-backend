package model

import "time"

// Document is a scam-warning corpus document: the canonical 4-section markdown
// (see pkg/util/corpusdoc) parsed and stored. Prevention is kept as its own
// field — it is surfaced to the analyzer as reference advice but never
// chunked/embedded (see internal/scam/ingest).
type Document struct {
	ID         int64
	URL        string
	Title      string
	Content    string
	Prevention string
	ScamType   string
	Source     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
