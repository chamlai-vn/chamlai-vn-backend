// Package crawler turns a list of scam-warning article urls (and hand-curated
// local files) into ingest-ready documents: it fetches a page, extracts the
// title and body with per-site CSS selectors, and labels the scam type with a
// rule-based classifier. It deliberately does NOT crawl recursively or discover
// links — callers supply an explicit url list — which is why it builds on
// net/http + goquery rather than a full crawling framework like colly.
//
// Construction lives here (Crawler, New, Options). The behaviour is split by
// role: crawl.go fetches+parses a url, file.go reads local files and seed
// lists, sites.go holds the per-host selector registry, label.go infers the
// scam type, and type.go carries the DTOs.
package crawler

import (
	"fmt"
	"net/http"
	"time"
)

// defaultUserAgent identifies the crawler as a browser. Several Vietnamese news
// sites return 403 or an empty shell to the default Go user-agent, so we send a
// realistic one.
const defaultUserAgent = "Mozilla/5.0 (compatible; ChamLaiBot/1.0; +https://chamlai.vn)"

// defaultTimeout caps a single page fetch. A slow source should be skipped, not
// allowed to stall the whole batch.
const defaultTimeout = 15 * time.Second

// maxRedirects caps how many redirect hops Fetch follows — matches Go's
// http.Client default (10), stated explicitly because defaultCheckRedirect
// below replaces the default redirect policy.
const maxRedirects = 10

// defaultCheckRedirect re-applies the per-host allowlist (sites.go, ruleFor)
// to every redirect hop, not just the initial URL. Fetch already checks
// ruleFor before the first request, but Go's default http.Client redirect
// policy would otherwise follow a 3xx from an allowlisted host to ANY
// destination — including an internal address — without re-checking it.
// Blocking that here closes an SSRF gap shared by the manual crawl path and
// the corpus-generate pipeline, both of which use this client.
func defaultCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("crawler: stopped after %d redirects", maxRedirects)
	}
	if _, ok := ruleFor(req.URL.Host); !ok {
		return fmt.Errorf("crawler: redirect to unregistered host %q blocked", req.URL.Host)
	}
	return nil
}

// HTTPDoer is the slice of *http.Client the crawler needs. Tests supply an
// httptest-backed client; production uses the default.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Crawler fetches and parses scam-warning articles. Safe for concurrent use as
// long as its HTTPDoer is (the default *http.Client is).
type Crawler struct {
	client    HTTPDoer
	userAgent string
}

// Option configures a Crawler. Zero-value defaults are applied in New.
type Option func(*Crawler)

// WithHTTPClient overrides the HTTP client (e.g. an httptest.Server client in
// tests, or a tuned client in production). Nil is ignored.
func WithHTTPClient(c HTTPDoer) Option {
	return func(cr *Crawler) {
		if c != nil {
			cr.client = c
		}
	}
}

// WithUserAgent overrides the User-Agent header. Empty is ignored.
func WithUserAgent(ua string) Option {
	return func(cr *Crawler) {
		if ua != "" {
			cr.userAgent = ua
		}
	}
}

// New builds a Crawler. Unset options fall back to a 15s-timeout http.Client
// (with defaultCheckRedirect enforcing the host allowlist on redirects) and a
// browser-like User-Agent.
func New(opts ...Option) *Crawler {
	cr := &Crawler{
		client: &http.Client{
			Timeout:       defaultTimeout,
			CheckRedirect: defaultCheckRedirect,
		},
		userAgent: defaultUserAgent,
	}
	for _, opt := range opts {
		opt(cr)
	}
	return cr
}
