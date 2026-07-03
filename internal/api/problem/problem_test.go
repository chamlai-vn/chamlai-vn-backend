package problem

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWrite(t *testing.T) {
	rr := httptest.NewRecorder()
	Write(rr, BadRequest("text không được rỗng"))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/problem+json; charset=utf-8" {
		t.Errorf("content-type = %q", ct)
	}

	var got Problem
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Type != "about:blank" || got.Title != "Bad Request" || got.Status != 400 {
		t.Errorf("body = %+v", got)
	}
	if got.Detail != "text không được rỗng" {
		t.Errorf("detail = %q", got.Detail)
	}
}

func TestHandler_NilError(t *testing.T) {
	rr := httptest.NewRecorder()
	h := Handler(func(w http.ResponseWriter, r *http.Request) error {
		w.WriteHeader(http.StatusOK)
		return nil
	})
	h(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

func TestHandler_ProblemError(t *testing.T) {
	rr := httptest.NewRecorder()
	h := Handler(func(w http.ResponseWriter, r *http.Request) error {
		return BadRequest("text không được rỗng")
	})
	h(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestHandler_GenericError_MasksDetailButLogs(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	rr := httptest.NewRecorder()
	h := Handler(func(w http.ResponseWriter, r *http.Request) error {
		return errors.New("pgvector down: connection refused")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(ContextWithLogger(context.Background(), logger))
	h(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}

	var got Problem
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if strings.Contains(got.Detail, "pgvector") {
		t.Errorf("internal detail leaked to client: %q", got.Detail)
	}

	if !strings.Contains(logBuf.String(), "pgvector down") {
		t.Errorf("internal error not logged: %s", logBuf.String())
	}
}

func TestLoggerFromContext_DefaultsWhenUnset(t *testing.T) {
	l := LoggerFromContext(context.Background())
	if l == nil {
		t.Fatal("want non-nil default logger")
	}
}
