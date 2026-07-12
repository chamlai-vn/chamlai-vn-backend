package middleware

import (
	"net"
	"net/http"
	"net/netip"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// cloudflareIPRanges are Cloudflare's published edge IP ranges
// (https://www.cloudflare.com/ips/, checked 2026-07-11). Cloudflare adds
// ranges infrequently; re-check the published list periodically and update
// this slice if it changes — a stale list only widens who gets the
// RemoteAddr fallback (fail-safe), it never wrongly trusts a non-Cloudflare
// peer.
var cloudflareIPRanges = mustParsePrefixes(
	// IPv4
	"173.245.48.0/20",
	"103.21.244.0/22",
	"103.22.200.0/22",
	"103.31.4.0/22",
	"141.101.64.0/18",
	"108.162.192.0/18",
	"190.93.240.0/20",
	"188.114.96.0/20",
	"197.234.240.0/22",
	"198.41.128.0/17",
	"162.158.0.0/15",
	"104.16.0.0/13",
	"104.24.0.0/14",
	"172.64.0.0/13",
	"131.0.72.0/22",
	// IPv6
	"2400:cb00::/32",
	"2606:4700::/32",
	"2803:f800::/32",
	"2405:b500::/32",
	"2405:8100::/32",
	"2a06:98c0::/29",
	"2c0f:f248::/32",
)

func mustParsePrefixes(cidrs ...string) []netip.Prefix {
	prefixes := make([]netip.Prefix, len(cidrs))
	for i, c := range cidrs {
		prefixes[i] = netip.MustParsePrefix(c)
	}
	return prefixes
}

// ClientIPFromCloudflare stores the client IP for requests that actually
// arrived via a Cloudflare edge, and falls back to the raw TCP peer
// (chimw.ClientIPFromRemoteAddr) otherwise. Read the result with
// chimw.GetClientIP, same as any other chimw.ClientIPFrom* middleware.
//
// This is defense-in-depth, not the primary control: the CF-Connecting-IP
// header is only trustworthy at all because the origin's firewall is
// expected to accept traffic solely from Cloudflare's ranges (see
// docs/plans/2026-07-11-001-feat-rate-limit-budget-cap-plan.md, R2). This
// middleware protects against that firewall rule drifting or a request
// reaching the app some other way (e.g. a misrouted internal call, or a gap
// while the firewall rule is being rolled out): even if a non-Cloudflare
// peer connects directly and sends a forged CF-Connecting-IP header, this
// middleware ignores the header and uses the peer's real address instead —
// it cannot itself stop a spoofed request from reaching the app, only stop
// it from spoofing its IP once here.
func ClientIPFromCloudflare(next http.Handler) http.Handler {
	fromHeader := chimw.ClientIPFromHeader("CF-Connecting-IP")(next)
	fromRemoteAddr := chimw.ClientIPFromRemoteAddr(next)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if peerIsCloudflare(r.RemoteAddr) {
			fromHeader.ServeHTTP(w, r)
			return
		}
		fromRemoteAddr.ServeHTTP(w, r)
	})
}

func peerIsCloudflare(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr // RemoteAddr may already be a bare IP (e.g. in tests).
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, prefix := range cloudflareIPRanges {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
