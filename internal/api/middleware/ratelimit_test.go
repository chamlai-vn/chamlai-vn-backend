package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	chimw "github.com/go-chi/chi/v5/middleware"
	"golang.org/x/time/rate"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// withClientIP mirrors router.go's real ordering: a chimw.ClientIPFrom*
// middleware runs before RateLimitPerIP and strips RemoteAddr down to a bare
// IP in the request context. Calling RateLimitPerIP directly on a raw
// *http.Request (as clientIP's fallback path does) would leave the port
// attached and break rateKey's IPv4/IPv6 parsing — tests must go through
// this, not rely on the fallback meant for logger tests that don't care
// about the exact key.
func withClientIP(next http.Handler) http.Handler {
	return chimw.ClientIPFromRemoteAddr(next)
}

func TestRateLimitPerIP_AllowsBurstThenBlocks(t *testing.T) {
	// A slow refill rate (1 token/hour) means only the initial burst is
	// available within the test's lifetime — anything beyond it must 429.
	h := withClientIP(RateLimitPerIP(rate.Limit(1.0 / 3600))(okHandler()))

	for i := 0; i < rateLimitBurst; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.10:5555"
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200 (within burst)", i, rr.Code)
		}
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.10:5555"
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (burst exhausted)", rr.Code)
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Error("want Retry-After header set on 429")
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/problem+json; charset=utf-8" {
		t.Errorf("content-type = %q", ct)
	}
}

func TestRateLimitPerIP_DifferentIPsAreIndependent(t *testing.T) {
	h := withClientIP(RateLimitPerIP(rate.Limit(1.0 / 3600))(okHandler()))

	// Exhaust IP A's burst.
	for i := 0; i < rateLimitBurst; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.20:1"
		h.ServeHTTP(rr, req)
	}
	rrA := httptest.NewRecorder()
	reqA := httptest.NewRequest(http.MethodGet, "/", nil)
	reqA.RemoteAddr = "203.0.113.20:1"
	h.ServeHTTP(rrA, reqA)
	if rrA.Code != http.StatusTooManyRequests {
		t.Fatalf("IP A: status = %d, want 429 (its burst is exhausted)", rrA.Code)
	}

	// A fresh, unrelated IP must still have its own full burst.
	rrB := httptest.NewRecorder()
	reqB := httptest.NewRequest(http.MethodGet, "/", nil)
	reqB.RemoteAddr = "203.0.113.21:1"
	h.ServeHTTP(rrB, reqB)
	if rrB.Code != http.StatusOK {
		t.Fatalf("IP B: status = %d, want 200 (independent bucket)", rrB.Code)
	}
}

func TestRateLimitPerIP_IPv6SameSlash64ShareABucket(t *testing.T) {
	h := withClientIP(RateLimitPerIP(rate.Limit(1.0 / 3600))(okHandler()))

	// Two distinct /128 addresses inside the same /64 must exhaust the same
	// bucket — otherwise an attacker could rotate freely within their own
	// allocation and never be throttled.
	addrs := []string{
		"[2001:db8:abcd:1::1]:1",
		"[2001:db8:abcd:1::2]:1",
	}
	for i := 0; i < rateLimitBurst; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = addrs[i%len(addrs)]
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d (%s): status = %d, want 200 (within shared burst)", i, addrs[i%len(addrs)], rr.Code)
		}
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = addrs[0]
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (shared /64 bucket exhausted)", rr.Code)
	}
}

func TestRateLimitPerIP_IPv6DifferentSlash64AreIndependent(t *testing.T) {
	h := withClientIP(RateLimitPerIP(rate.Limit(1.0 / 3600))(okHandler()))

	for i := 0; i < rateLimitBurst; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "[2001:db8:aaaa:1::1]:1"
		h.ServeHTTP(rr, req)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "[2001:db8:bbbb:1::1]:1" // different /64
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (different /64 is a distinct bucket)", rr.Code)
	}
}

func TestRateLimitPerIP_NonPositiveRPS_Disables(t *testing.T) {
	h := withClientIP(RateLimitPerIP(0)(okHandler()))

	for i := 0; i < rateLimitBurst+5; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.30:1"
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200 (limiter disabled)", i, rr.Code)
		}
	}
}

func TestRateKey(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		same bool
	}{
		{"ipv4 identical", "203.0.113.5", "203.0.113.5", true},
		{"ipv4 different host", "203.0.113.5", "203.0.113.6", false},
		{"ipv6 same /64", "2001:db8:abcd:1::1", "2001:db8:abcd:1::2", true},
		{"ipv6 different /64", "2001:db8:abcd:1::1", "2001:db8:abcd:2::1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ka, kb := rateKey(tc.a), rateKey(tc.b)
			if (ka == kb) != tc.same {
				t.Errorf("rateKey(%q)=%q, rateKey(%q)=%q; same=%v, want %v", tc.a, ka, tc.b, kb, ka == kb, tc.same)
			}
		})
	}
}
