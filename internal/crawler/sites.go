package crawler

import "strings"

// siteRules maps a normalised host (no leading "www.") to its extraction rule.
//
// These selectors are best-effort for each site's current article layout and
// WILL drift when a publisher redesigns. When a source starts producing empty
// content, re-inspect the page HTML and update the selector here — Fetch logs an
// "empty content" error precisely so that drift is visible. Keeping every rule
// in this one table is deliberate: tuning a selector should never mean touching
// the fetch logic.
var siteRules = map[string]siteRule{
	"vnexpress.net": {source: "vnexpress", titleSel: "h1.title-detail", contentSel: "article.fck_detail"},
	"tuoitre.vn":    {source: "tuoitre", titleSel: "h1.detail-title", contentSel: "div.detail-content"},
	"vtv.vn":        {source: "vtv", titleSel: "h1.title-detail", contentSel: "div.ta-justify"},
	"cand.com.vn":   {source: "cand", titleSel: "h1.box-title-detail", contentSel: "div.box-content-detail"},
	"mst.gov.vn":    {source: "mst-attt", titleSel: "h1", contentSel: "div.post-content"},
}

// ruleFor returns the extraction rule for host, matching with the leading
// "www." stripped. The bool is false for an unregistered host.
func ruleFor(host string) (siteRule, bool) {
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	r, ok := siteRules[host]
	return r, ok
}
