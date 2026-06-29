package crawler

import "strings"

// siteRules maps a normalised host (no leading "www.") to its extraction rule.
// The actual entries live in sites_local.go (git-ignored) so that the specific
// sites being crawled are not exposed in the public repository. Copy
// internal/crawler/sites_local.go.example to sites_local.go and fill in your
// own selectors to get started.
//
// Selectors are best-effort for each site's current article layout and WILL
// drift when a publisher redesigns. When a source starts producing "empty
// content" errors, re-inspect the page HTML and update the selector.
var siteRules = map[string]siteRule{}

// ruleFor returns the extraction rule for host, matching with the leading
// "www." stripped. The bool is false for an unregistered host.
func ruleFor(host string) (siteRule, bool) {
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	r, ok := siteRules[host]
	return r, ok
}
