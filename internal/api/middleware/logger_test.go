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

func TestRequestLogger_LogsStatusAndPath(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("short and stout"))
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/brew", nil)
	req = req.WithContext(problem.ContextWithLogger(req.Context(), logger))

	RequestLogger(next).ServeHTTP(rr, req)

	out := buf.String()
	if !strings.Contains(out, "status=418") {
		t.Errorf("missing status: %s", out)
	}
	if !strings.Contains(out, "path=/brew") {
		t.Errorf("missing path: %s", out)
	}
	if !strings.Contains(out, "level=WARN") {
		t.Errorf("4xx should log at WARN: %s", out)
	}
}

func TestRequestLogger_DefaultsStatusToOKWhenUnset(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// handler never calls WriteHeader explicitly
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(problem.ContextWithLogger(req.Context(), logger))

	RequestLogger(next).ServeHTTP(rr, req)

	if !strings.Contains(buf.String(), "status=200") {
		t.Errorf("missing default status: %s", buf.String())
	}
}

func TestLevelForStatus(t *testing.T) {
	cases := map[int]slog.Level{
		200: slog.LevelInfo,
		404: slog.LevelWarn,
		500: slog.LevelError,
	}
	for status, want := range cases {
		if got := levelForStatus(status); got != want {
			t.Errorf("levelForStatus(%d) = %v, want %v", status, got, want)
		}
	}
}
