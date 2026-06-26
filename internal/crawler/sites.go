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
// Selectors below were verified against each site's live article HTML. Hosts are
// keyed without a leading "www." (ruleFor strips it). laodong.vn is intentionally
// absent: it serves a JS cookie/anti-bot challenge that a plain HTTP client can't
// pass — ingest those via the local-file (markdown) path instead of crawling.
var siteRules = map[string]siteRule{
	"vnexpress.net":   {source: "vnexpress", titleSel: "h1.title-detail", contentSel: "article.fck_detail"},
	"tuoitre.vn":      {source: "tuoitre", titleSel: "h1.detail-title", contentSel: "div.detail-content"},
	"vtv.vn":          {source: "vtv", titleSel: "h1.title", contentSel: "div.detail-content"},
	"cand.vn":         {source: "cand", titleSel: "h1.article__title", contentSel: "div.article__body"},
	"mst.gov.vn":      {source: "mst-attt", titleSel: "h1", contentSel: "div.detail-content"},
	"vietnamnet.vn":   {source: "vietnamnet", titleSel: "h1.content-detail-title", contentSel: "div.maincontent"},
	"thanhnien.vn":    {source: "thanhnien", titleSel: "h1.detail-title", contentSel: "div.detail-content"},
	"vietnamplus.vn":  {source: "vietnamplus", titleSel: "h1.article__title", contentSel: "div.article__body"},
	"baochinhphu.vn":  {source: "baochinhphu", titleSel: "h1.detail-title", contentSel: "div.detail-content"},
	"bocongan.gov.vn": {source: "bocongan", titleSel: "h1", contentSel: "div.tinymce-content"},
	// chongluadao.vn uses a React/Next.js build with CSS module class names that change
	// each deploy, so plain-HTTP crawling cannot extract content reliably. Ingest articles
	// from this site via the local-file (markdown) path instead.
	"ncsgroup.vn": {source: "ncsgroup", titleSel: "h1", contentSel: "div.maincontent"},
}

// ruleFor returns the extraction rule for host, matching with the leading
// "www." stripped. The bool is false for an unregistered host.
func ruleFor(host string) (siteRule, bool) {
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	r, ok := siteRules[host]
	return r, ok
}
