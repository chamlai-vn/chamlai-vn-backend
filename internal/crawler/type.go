package crawler

// FetchedDoc is a parsed article ready to be labelled and indexed. It maps
// directly onto ingest.Document (the caller adds the inferred ScamType).
type FetchedDoc struct {
	URL     string
	Title   string
	Content string
	Source  string // short source key, e.g. "vnexpress", "manual"
}

// siteRule describes how to extract an article from one host: which short
// source key to record, and the CSS selectors for the title and the body
// container. Body text is taken from the <p> descendants of contentSel.
type siteRule struct {
	source     string
	titleSel   string
	contentSel string
}
