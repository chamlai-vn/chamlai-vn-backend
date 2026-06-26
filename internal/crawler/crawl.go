package crawler

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Fetch retrieves rawURL and extracts the article into a FetchedDoc using the
// per-host rule in sites.go. It returns an error (for the caller to log-and-skip)
// when the host is unregistered, the response is not 200, the page can't be
// parsed, or the extracted body is empty — an empty body usually means the
// site's layout changed and its selector needs updating.
func (c *Crawler) Fetch(ctx context.Context, rawURL string) (FetchedDoc, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return FetchedDoc{}, fmt.Errorf("crawler: parse url %q: %w", rawURL, err)
	}
	rule, ok := ruleFor(u.Host)
	if !ok {
		return FetchedDoc{}, fmt.Errorf("crawler: unknown host %q (no site rule)", u.Host)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return FetchedDoc{}, fmt.Errorf("crawler: new request %q: %w", rawURL, err)
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		return FetchedDoc{}, fmt.Errorf("crawler: get %q: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return FetchedDoc{}, fmt.Errorf("crawler: get %q: status %d", rawURL, resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return FetchedDoc{}, fmt.Errorf("crawler: parse %q: %w", rawURL, err)
	}

	title := firstNonEmpty(
		strings.TrimSpace(doc.Find(rule.titleSel).First().Text()),
		strings.TrimSpace(doc.Find("h1").First().Text()),
	)
	content := extractContent(doc.Find(rule.contentSel).First())
	if content == "" {
		return FetchedDoc{}, fmt.Errorf("crawler: %q: empty content (selector %q matched nothing?)", rawURL, rule.contentSel)
	}

	return FetchedDoc{URL: rawURL, Title: title, Content: content, Source: rule.source}, nil
}

// extractContent joins the text of the <p> descendants of sel with blank lines,
// matching ragutil.Chunk's preferred "\n\n" paragraph boundary. If the
// container has no paragraphs it falls back to the container's own text, so a
// site that wraps body text without <p> tags still yields something.
func extractContent(sel *goquery.Selection) string {
	var paras []string
	sel.Find("p").Each(func(_ int, p *goquery.Selection) {
		if t := strings.TrimSpace(p.Text()); t != "" {
			paras = append(paras, t)
		}
	})
	if len(paras) > 0 {
		return strings.Join(paras, "\n\n")
	}
	return strings.TrimSpace(sel.Text())
}

// firstNonEmpty returns the first non-empty string, or "".
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
