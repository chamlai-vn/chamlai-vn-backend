package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	chimw "github.com/go-chi/chi/v5/middleware"
)

func TestClientIPFromCloudflare_TrustsHeaderFromCloudflarePeer(t *testing.T) {
	var got string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = chimw.GetClientIP(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "104.16.1.1:443" // inside a published Cloudflare range
	req.Header.Set("CF-Connecting-IP", "198.51.100.7")

	ClientIPFromCloudflare(next).ServeHTTP(httptest.NewRecorder(), req)

	if got != "198.51.100.7" {
		t.Errorf("client IP = %q, want header value (peer is a Cloudflare edge)", got)
	}
}

func TestClientIPFromCloudflare_IgnoresHeaderFromNonCloudflarePeer(t *testing.T) {
	var got string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = chimw.GetClientIP(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.99:1234" // NOT a Cloudflare range
	req.Header.Set("CF-Connecting-IP", "1.2.3.4")

	ClientIPFromCloudflare(next).ServeHTTP(httptest.NewRecorder(), req)

	if got != "203.0.113.99" {
		t.Errorf("client IP = %q, want the real TCP peer (header must be ignored when peer isn't Cloudflare)", got)
	}
}

func TestClientIPFromCloudflare_FlyIOHop(t *testing.T) {
	// On Fly.io: RemoteAddr=Fly proxy, Fly-Client-IP=Cloudflare edge, CF-Connecting-IP=real client.
	var got string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = chimw.GetClientIP(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "37.16.13.28:12345"               // Fly's proxy IP, not in Cloudflare range
	req.Header.Set("Fly-Client-IP", "104.16.1.1")      // Cloudflare edge (inside Cloudflare range)
	req.Header.Set("CF-Connecting-IP", "198.51.100.7") // real client

	ClientIPFromCloudflare(next).ServeHTTP(httptest.NewRecorder(), req)

	if got != "198.51.100.7" {
		t.Errorf("client IP = %q, want CF-Connecting-IP value (Fly-Client-IP identified as Cloudflare)", got)
	}
}

func TestClientIPFromCloudflare_IgnoresHeaderWhenFlyClientIPNotCloudflare(t *testing.T) {
	// Direct access to .fly.dev (bypasses Cloudflare), attacker forges CF-Connecting-IP.
	var got string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = chimw.GetClientIP(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.50:5000"            // attacker's real IP
	req.Header.Set("Fly-Client-IP", "203.0.113.50") // same as RemoteAddr (no Cloudflare proxy)
	req.Header.Set("CF-Connecting-IP", "1.2.3.4")   // forged

	ClientIPFromCloudflare(next).ServeHTTP(httptest.NewRecorder(), req)

	if got != "203.0.113.50" {
		t.Errorf("client IP = %q, want the real TCP peer (Fly-Client-IP not Cloudflare, forged header ignored)", got)
	}
}

func TestPeerIsCloudflare(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"104.16.1.1:443", true},   // 104.16.0.0/13
		{"172.64.10.5:443", true},  // 172.64.0.0/13
		{"203.0.113.1:443", false}, // TEST-NET-3, not Cloudflare
		{"not-an-ip:443", false},
		{"104.16.1.1", true}, // bare IP without port
	}
	for _, tc := range cases {
		if got := peerIsCloudflare(tc.addr); got != tc.want {
			t.Errorf("peerIsCloudflare(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}
