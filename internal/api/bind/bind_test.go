package bind

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/problem"
)

type testDTO struct {
	Text string `json:"text" validate:"required,max=10"`
}

func newRequest(body string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
}

func TestJSON_OK(t *testing.T) {
	got, err := JSON[testDTO](newRequest(`{"text":"hello"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Text != "hello" {
		t.Errorf("text = %q", got.Text)
	}
}

func TestJSON_MalformedBody(t *testing.T) {
	_, err := JSON[testDTO](newRequest(`{"text":`))
	_ = assertBadRequest(t, err)
}

func TestJSON_EmptyBody(t *testing.T) {
	_, err := JSON[testDTO](newRequest(``))
	_ = assertBadRequest(t, err)
}

func TestJSON_UnknownField(t *testing.T) {
	_, err := JSON[testDTO](newRequest(`{"text":"hello","extra":1}`))
	_ = assertBadRequest(t, err)
}

func TestJSON_TrailingData(t *testing.T) {
	_, err := JSON[testDTO](newRequest(`{"text":"hello"}{"text":"again"}`))
	_ = assertBadRequest(t, err)
}

func TestJSON_WrongType(t *testing.T) {
	_, err := JSON[testDTO](newRequest(`{"text":123}`))
	_ = assertBadRequest(t, err)
}

func TestJSON_ValidationRequired(t *testing.T) {
	_, err := JSON[testDTO](newRequest(`{"text":""}`))
	p := assertBadRequest(t, err)
	if !strings.Contains(p.Detail, "text") {
		t.Errorf("detail should mention json field name %q: %q", "text", p.Detail)
	}
}

func TestJSON_ValidationMax(t *testing.T) {
	_, err := JSON[testDTO](newRequest(`{"text":"this is way too long"}`))
	_ = assertBadRequest(t, err)
}

func TestJSON_MaxBytesError(t *testing.T) {
	req := newRequest(`{"text":"hello world, this is longer than the limit"}`)
	req.Body = http.MaxBytesReader(httptest.NewRecorder(), req.Body, 10)

	_, err := JSON[testDTO](req)
	p, ok := err.(*problem.Problem)
	if !ok {
		t.Fatalf("err = %T, want *problem.Problem", err)
	}
	if p.Status != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", p.Status)
	}
}

func assertBadRequest(t *testing.T, err error) *problem.Problem {
	t.Helper()
	p, ok := err.(*problem.Problem)
	if !ok {
		t.Fatalf("err = %T (%v), want *problem.Problem", err, err)
	}
	if p.Status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", p.Status)
	}
	return p
}
