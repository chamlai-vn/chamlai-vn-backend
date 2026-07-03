package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStack_LoggerObservesRecoveredPanicStatus verifies the ordering
// documented in RequestLogger: with RequestLogger wrapping Recoverer, a
// panic still gets logged with status 500, not silently dropped.
func TestStack_LoggerObservesRecoveredPanicStatus(t *testing.T) {
	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))

	panics := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})

	stack := RequestID(RequestLogger(Recoverer(panics)))

	rr := httptest.NewRecorder()
	stack.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
	if !strings.Contains(buf.String(), "status=500") {
		t.Errorf("request logger did not observe recovered panic's status: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "http_request") {
		t.Errorf("request logger did not emit its summary line: %s", buf.String())
	}
}
