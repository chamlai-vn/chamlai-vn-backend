package middleware

import (
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"golang.org/x/time/rate"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/problem"
)

// Internal tuning for the per-IP bucket cache. Not exposed as config: these
// are RAM/eviction knobs an operator would never sensibly set from the
// environment, unlike the requests-per-minute rate itself. Promote one to a
// config field only once real traffic data justifies tuning it (see
// docs/plans/2026-07-11-001-feat-rate-limit-budget-cap-plan.md, "Config mới").
const (
	rateLimitBurst  = 10
	rateLimitMaxIPs = 100_000
	rateLimitTTL    = 10 * time.Minute
)

// RateLimitPerIP throttles each client IP to rps requests/second with a
// burst of rateLimitBurst, returning 429 with a Retry-After header once a
// key exceeds it. Buckets live in a size- and TTL-bounded LRU so an
// unbounded number of distinct IPs (e.g. a spray of spoofed or rotated
// addresses) can't grow memory without limit; an idle IP's bucket expires
// and is recreated fresh on its next request — an accepted trade-off for a
// generous, VN-CGNAT-aware threshold (see origin doc).
//
// rps <= 0 disables the limiter (every request passes through unchanged) —
// useful for tests that don't want rate limiting in the mix.
func RateLimitPerIP(rps rate.Limit) func(http.Handler) http.Handler {
	if rps <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}

	// expirable.LRU is already internally synchronized (every Get/Add takes
	// its own lock), so no additional locking is needed here. Two goroutines
	// racing on the same brand-new key can each create a *rate.Limiter and
	// call Add — harmless: the LRU keeps whichever Add landed last, and the
	// loser is simply discarded, at most briefly doubling that one key's
	// effective burst on its very first requests.
	cache := expirable.NewLRU[string, *rate.Limiter](rateLimitMaxIPs, nil, rateLimitTTL)

	limiterFor := func(key string) *rate.Limiter {
		if lim, ok := cache.Get(key); ok {
			return lim
		}
		lim := rate.NewLimiter(rps, rateLimitBurst)
		cache.Add(key, lim)
		return lim
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			lim := limiterFor(rateKey(clientIP(r)))

			reservation := lim.Reserve()
			if !reservation.OK() {
				// Burst can never admit even one token — a misconfiguration
				// (e.g. burst 0), not something to punish the caller for.
				next.ServeHTTP(w, r)
				return
			}
			if delay := reservation.Delay(); delay > 0 {
				reservation.Cancel() // rejecting: give the token back
				secs := int(math.Ceil(delay.Seconds()))
				if secs < 1 {
					secs = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				problem.Write(w, problem.TooManyRequests("bạn gửi quá nhanh, vui lòng thử lại sau ít phút"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// rateKey normalizes an IP into the rate-limiter bucket key: IPv4 addresses
// are kept in full; IPv6 addresses are collapsed to their /64, the prefix a
// single subscriber is typically allocated. Keying on the full /128 would
// let anyone rotate freely within their own /64 and defeat the limiter
// without spoofing anything.
func rateKey(ipStr string) string {
	ip, err := netip.ParseAddr(ipStr)
	if err != nil {
		return ipStr
	}
	if ip.Is4() || ip.Is4In6() {
		return ip.Unmap().String()
	}
	prefix, err := ip.Prefix(64)
	if err != nil {
		return ip.String()
	}
	return prefix.Masked().String()
}
