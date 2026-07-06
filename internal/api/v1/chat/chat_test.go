package chat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/problem"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/analyzer"
	scamchat "github.com/chamlai-vn/chamlai-vn-backend/internal/scam/chat"
)

// fakeChatter records the request it saw and returns a canned response/error.
type fakeChatter struct {
	gotReq scamchat.ChatRequest
	resp   *scamchat.ChatResponse
	err    error
}

func (f *fakeChatter) Reply(_ context.Context, req scamchat.ChatRequest) (*scamchat.ChatResponse, error) {
	f.gotReq = req
	return f.resp, f.err
}

func TestHandle_OK(t *testing.T) {
	ch := &fakeChatter{resp: &scamchat.ChatResponse{
		Intent:   scamchat.IntentScam,
		Analysis: &analyzer.AnalysisResult{RiskLevel: analyzer.RiskRed},
	}}
	h := New(ch)

	rr := httptest.NewRecorder()
	body := `{"messages":[{"role":"user","content":"tin này có lừa không"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(body))
	if err := h.Handle(rr, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if len(ch.gotReq.Messages) != 1 || ch.gotReq.Messages[0].Content != "tin này có lừa không" {
		t.Errorf("chatter got messages = %+v", ch.gotReq.Messages)
	}

	var got scamchat.ChatResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Intent != scamchat.IntentScam || got.Analysis == nil {
		t.Errorf("body = %+v", got)
	}
}

func TestHandle_BadInput(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"malformed json", `{"messages":`},
		{"empty messages", `{"messages":[]}`},
		{"missing content", `{"messages":[{"role":"user"}]}`},
		{"invalid role", `{"messages":[{"role":"system","content":"x"}]}`},
		{"unknown field", `{"foo":1,"messages":[{"role":"user","content":"x"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch := &fakeChatter{err: errors.New("should not be called")}
			h := New(ch)

			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(tc.body))
			err := h.Handle(rr, req)

			p, ok := err.(*problem.Problem)
			if !ok {
				t.Fatalf("err = %T (%v), want *problem.Problem", err, err)
			}
			if p.Status != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", p.Status)
			}
			if ch.gotReq.Messages != nil {
				t.Error("chatter was called on invalid input")
			}
		})
	}
}

func TestHandle_NoUserMessage_400(t *testing.T) {
	ch := &fakeChatter{err: scamchat.ErrNoUserMessage}
	h := New(ch)

	rr := httptest.NewRecorder()
	// passes struct validation (roles valid) but last message is assistant.
	body := `{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(body))
	err := h.Handle(rr, req)

	p, ok := err.(*problem.Problem)
	if !ok {
		t.Fatalf("err = %T (%v), want *problem.Problem", err, err)
	}
	if p.Status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", p.Status)
	}
}

func TestHandle_ChatterError_500(t *testing.T) {
	ch := &fakeChatter{err: errors.New("claude down")}
	h := New(ch)

	rr := httptest.NewRecorder()
	body := `{"messages":[{"role":"user","content":"xin chào"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(body))
	err := h.Handle(rr, req)

	p, ok := err.(*problem.Problem)
	if !ok {
		t.Fatalf("err = %T (%v), want *problem.Problem", err, err)
	}
	if p.Status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", p.Status)
	}
}
