package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/problem"
)

func TestRequestID_GeneratesWhenMissing(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	rr := httptest.NewRecorder()
	RequestID(next).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if id := rr.Header().Get(requestIDHeader); id == "" {
		t.Fatal("expected a generated request ID header")
	}
}

func TestRequestID_EchoesClientHeader(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(requestIDHeader, "client-supplied-id-123")

	RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rr, req)

	if got := rr.Header().Get(requestIDHeader); got != "client-supplied-id-123" {
		t.Errorf("id = %q, want echoed client id", got)
	}
}

func TestRequestID_RejectsOversizedOrControlChars(t *testing.T) {
	cases := []string{
		strings.Repeat("a", 65),
		"bad\nheader",
		"bad\x00null",
	}
	for _, bad := range cases {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(requestIDHeader, bad)

		RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rr, req)

		if got := rr.Header().Get(requestIDHeader); got == bad {
			t.Errorf("invalid client id %q was echoed back verbatim", bad)
		}
	}
}

func TestRequestID_InstallsContextLoggerTaggedWithID(t *testing.T) {
	var buf bytes.Buffer
	origDefault := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(origDefault)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		problem.LoggerFromContext(r.Context()).Info("inside handler")
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(requestIDHeader, "fixed-test-id")
	RequestID(next).ServeHTTP(rr, req)

	if !strings.Contains(buf.String(), "request_id=fixed-test-id") {
		t.Errorf("log line missing request_id attribute: %s", buf.String())
	}
}
