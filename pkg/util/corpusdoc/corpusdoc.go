// Package corpusdoc parses and serializes the canonical 4-section corpus
// markdown format. It is the single interchange type for the corpus
// crawl→ingest pipeline: internal/scam/enrich produces a Document,
// -mode=generate serializes it to data/corpus/*.md for human review,
// -mode=ingest parses the reviewed file back, and internal/scam/ingest
// consumes the parsed Document directly — neither side depends on the
// other's package.
//
// The format:
//
//	# General information
//	url: https://...
//	title: <thủ đoạn tóm tắt>
//	type: impersonation_authority        # one of internal/scam/crawler.ValidScamTypes
//	# Content
//	<nội dung thật, có thể được LLM làm sạch/bổ sung>
//	# User query
//	1. <câu hỏi giọng nạn nhân>
//	2. <...>
//	# Prevention
//	<cách phòng tránh>
//
// Sections may appear in any order but all four must be present. This is a
// fixed 4-section scanner, not a general markdown parser — see splitSections.
package corpusdoc

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/crawler"
)

// Document is the parsed/to-be-serialized form of one corpus markdown file.
type Document struct {
	URL         string
	Title       string
	ScamType    string
	Content     string
	UserQueries []string
	Prevention  string
}

const (
	sectionGeneral    = "General information"
	sectionContent    = "Content"
	sectionUserQuery  = "User query"
	sectionPrevention = "Prevention"
)

var knownSections = map[string]bool{
	sectionGeneral:    true,
	sectionContent:    true,
	sectionUserQuery:  true,
	sectionPrevention: true,
}

// queryNumberPrefix strips a leading "1. " / "1) " list marker from a
// "# User query" line.
var queryNumberPrefix = regexp.MustCompile(`^\d+[.)]\s*`)

// Parse reads the canonical 4-section format and returns a validated
// Document. It never panics: malformed or adversarial input (missing
// sections, an unknown section header, a scam type outside
// crawler.ValidScamTypes, a non-http(s) url) is rejected with a plain error.
func Parse(text string) (Document, error) {
	// Trim a leading UTF-8 BOM plus surrounding whitespace, mirroring
	// crawler/file.go's splitFrontmatter so a file saved with a BOM still
	// parses.
	text = strings.TrimLeft(text, "\uFEFF \t\r\n")

	sections, err := splitSections(text)
	if err != nil {
		return Document{}, fmt.Errorf("corpusdoc: %w", err)
	}

	general, ok := sections[sectionGeneral]
	if !ok {
		return Document{}, fmt.Errorf("corpusdoc: missing %q section", sectionGeneral)
	}
	meta := parseKeyValueLines(general)

	content := strings.TrimSpace(strings.Join(sections[sectionContent], "\n"))
	if content == "" {
		return Document{}, fmt.Errorf("corpusdoc: missing or empty %q section", sectionContent)
	}

	doc := Document{
		URL:         strings.TrimSpace(meta["url"]),
		Title:       strings.TrimSpace(meta["title"]),
		ScamType:    strings.TrimSpace(meta["type"]),
		Content:     content,
		UserQueries: parseUserQueries(sections[sectionUserQuery]),
		Prevention:  strings.TrimSpace(strings.Join(sections[sectionPrevention], "\n")),
	}
	if err := validate(doc); err != nil {
		return Document{}, err
	}
	return doc, nil
}

// Serialize renders doc back into the canonical 4-section format. Parse(
// Serialize(doc)) round-trips doc's fields (modulo whitespace normalisation).
func Serialize(doc Document) string {
	var b strings.Builder
	b.WriteString("# " + sectionGeneral + "\n")
	fmt.Fprintf(&b, "url: %s\n", doc.URL)
	fmt.Fprintf(&b, "title: %s\n", doc.Title)
	fmt.Fprintf(&b, "type: %s\n", doc.ScamType)
	b.WriteString("# " + sectionContent + "\n")
	b.WriteString(strings.TrimSpace(doc.Content))
	b.WriteString("\n# " + sectionUserQuery + "\n")
	for i, q := range doc.UserQueries {
		fmt.Fprintf(&b, "%d. %s\n", i+1, q)
	}
	b.WriteString("# " + sectionPrevention + "\n")
	b.WriteString(strings.TrimSpace(doc.Prevention))
	b.WriteString("\n")
	return b.String()
}

// validate rejects a Document that is missing a required field, declares a
// scam type outside the fixed label set, or carries a url that isn't
// http(s) — the same checks the ingest pipeline needs to trust the document.
func validate(doc Document) error {
	if doc.URL == "" {
		return fmt.Errorf("corpusdoc: missing required \"url\"")
	}
	u, err := url.Parse(doc.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("corpusdoc: url %q must use http(s) scheme", doc.URL)
	}
	if doc.Title == "" {
		return fmt.Errorf("corpusdoc: missing required \"title\"")
	}
	if doc.ScamType == "" {
		return fmt.Errorf("corpusdoc: missing required \"type\"")
	}
	if !crawler.ValidScamTypes[doc.ScamType] {
		return fmt.Errorf("corpusdoc: unknown scam type %q", doc.ScamType)
	}
	return nil
}

// splitSections scans text for lines of the exact form "# <known section
// name>" and buckets the lines that follow each header. Sections may repeat
// or appear in any order (a later occurrence appends); any content before
// the first header, or a header naming something outside knownSections, is
// rejected.
func splitSections(text string) (map[string][]string, error) {
	sections := make(map[string][]string)
	var current string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			name := strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			if !knownSections[name] {
				return nil, fmt.Errorf("unknown section header %q", name)
			}
			current = name
			if _, exists := sections[current]; !exists {
				sections[current] = nil
			}
			continue
		}
		if current == "" {
			if trimmed != "" {
				return nil, fmt.Errorf("content before the first section header")
			}
			continue
		}
		sections[current] = append(sections[current], line)
	}
	if len(sections) == 0 {
		return nil, fmt.Errorf("no section headers found")
	}
	return sections, nil
}

// parseKeyValueLines parses flat "key: value" lines, tolerating blank or
// malformed lines — mirrors crawler/file.go's splitFrontmatter so the
// "General information" block uses the same forgiving technique as the
// existing hand-curated-file frontmatter.
func parseKeyValueLines(lines []string) map[string]string {
	meta := make(map[string]string)
	for _, line := range lines {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		meta[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	return meta
}

// parseUserQueries turns the raw "# User query" section lines into a clean
// list of question texts: blank lines are dropped and a leading "1. "/"1) "
// numbering marker is stripped from each line.
func parseUserQueries(lines []string) []string {
	var out []string
	for _, line := range lines {
		q := strings.TrimSpace(line)
		if q == "" {
			continue
		}
		q = strings.TrimSpace(queryNumberPrefix.ReplaceAllString(q, ""))
		if q != "" {
			out = append(out, q)
		}
	}
	return out
}
