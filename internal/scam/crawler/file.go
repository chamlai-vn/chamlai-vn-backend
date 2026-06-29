package crawler

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LoadURLs reads a seed file of one url per line. Blank lines and lines whose
// first non-space character is '#' (comments) are skipped. Surrounding
// whitespace is trimmed.
func LoadURLs(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("crawler: open seeds %q: %w", path, err)
	}
	defer f.Close()

	var urls []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		urls = append(urls, line)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("crawler: read seeds %q: %w", path, err)
	}
	return urls, nil
}

// defaultFileSource labels documents that come from a hand-curated local file
// (e.g. a YouTube transcript exported by hand) when the frontmatter omits
// "source".
const defaultFileSource = "manual"

// ParseLocalFile reads a hand-curated document with a small "---"-fenced
// frontmatter block followed by the body:
//
//	---
//	title: Cảnh báo lừa đảo ...
//	scam_type: investment_fraud
//	source: youtube
//	url: https://youtu.be/abc123
//	---
//	<body text ...>
//
// Only flat "key: value" lines are parsed (no nested YAML), which covers the
// four keys above without pulling in a YAML dependency. It returns the document
// and the scam_type from the frontmatter (empty if absent, so the caller can
// fall back to InferScamType). "url" and a non-empty body are required; "source"
// defaults to "manual".
func ParseLocalFile(path string) (FetchedDoc, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return FetchedDoc{}, "", fmt.Errorf("crawler: read file %q: %w", path, err)
	}

	meta, body, err := splitFrontmatter(string(raw))
	if err != nil {
		return FetchedDoc{}, "", fmt.Errorf("crawler: %q: %w", path, err)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return FetchedDoc{}, "", fmt.Errorf("crawler: %q: empty body", path)
	}
	if meta["url"] == "" {
		return FetchedDoc{}, "", fmt.Errorf("crawler: %q: frontmatter missing required \"url\"", path)
	}

	source := meta["source"]
	if source == "" {
		source = defaultFileSource
	}
	doc := FetchedDoc{
		URL:     meta["url"],
		Title:   meta["title"],
		Content: body,
		Source:  source,
	}
	return doc, meta["scam_type"], nil
}

// splitFrontmatter separates a leading "---"-fenced block from the body and
// parses the block into flat key/value pairs. It errors if the text does not
// start with a "---" fence or the closing fence is missing.
func splitFrontmatter(text string) (map[string]string, string, error) {
	// Trim a leading UTF-8 BOM () plus surrounding whitespace so a file
	// saved with a BOM still matches the opening "---" fence.
	text = strings.TrimLeft(text, "\uFEFF \t\r\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, "", fmt.Errorf("missing \"---\" frontmatter fence")
	}

	meta := make(map[string]string)
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			body := strings.Join(lines[i+1:], "\n")
			return meta, body, nil
		}
		key, val, ok := strings.Cut(lines[i], ":")
		if !ok {
			continue // tolerate blank or malformed lines inside the block
		}
		meta[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	return nil, "", fmt.Errorf("unterminated frontmatter (missing closing \"---\")")
}
