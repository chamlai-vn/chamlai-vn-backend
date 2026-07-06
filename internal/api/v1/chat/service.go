// Package chat is the HTTP layer over the conversational use case: it decodes a
// multi-turn request, routes it into one of three flows (project / founder /
// scam) via internal/scam/chat, and returns the reply as JSON. Construction +
// the Handler struct live in service.go (start here); the request DTO in
// type.go; the endpoint in chat.go. Mirrors internal/api/v1/analyze.
package chat

import (
	"context"

	scamchat "github.com/chamlai-vn/chamlai-vn-backend/internal/scam/chat"
)

// Chatter is the narrow slice of the chat use case the handler needs.
// *scamchat.Chatter satisfies it; tests supply a fake.
type Chatter interface {
	Reply(ctx context.Context, req scamchat.ChatRequest) (*scamchat.ChatResponse, error)
}

// Handler serves POST /v1/chat. Its collaborator is injected (no global state).
// Safe for concurrent use if it is.
type Handler struct {
	chatter Chatter
}

// New builds a Handler over the chat use case.
func New(chatter Chatter) *Handler {
	return &Handler{chatter: chatter}
}
