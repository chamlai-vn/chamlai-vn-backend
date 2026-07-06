package chat

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/bind"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/problem"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/respond"
	scamchat "github.com/chamlai-vn/chamlai-vn-backend/internal/scam/chat"
)

// Handle routes a conversational turn. The pipeline: decode → validate →
// chat.Reply (which routes into the project / founder / scam flow) → JSON.
// Errors are returned rather than written directly — problem.Handler translates
// them to application/problem+json.
//
// @Summary      Chat with the ChậmLại.vn assistant
// @Description  Routes a multi-turn message into one of three flows: questions about the project, about the founder, or (default) scam detection. The last message must be from the user.
// @Tags         chat
// @Accept       json
// @Produce      json
// @Param        request  body      Request                true  "Conversation history"
// @Success      200      {object}  chat.ChatResponse
// @Failure      400      {object}  problem.Problem  "malformed body, invalid messages, or last message not from user"
// @Failure      500      {object}  problem.Problem  "routing, retrieval, scoring, or answering failed"
// @Router       /v1/chat [post]
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) error {
	req, err := bind.JSON[Request](r)
	if err != nil {
		return err
	}

	messages := make([]scamchat.Message, len(req.Messages))
	for i, m := range req.Messages {
		messages[i] = scamchat.Message{Role: m.Role, Content: m.Content}
	}

	resp, err := h.chatter.Reply(r.Context(), scamchat.ChatRequest{Messages: messages})
	if err != nil {
		if errors.Is(err, scamchat.ErrNoUserMessage) {
			return problem.BadRequest("tin nhắn cuối cùng phải là của người dùng và không được rỗng")
		}
		return problem.Internal().WithErr(fmt.Errorf("chat reply: %w", err))
	}

	respond.JSON(w, http.StatusOK, resp)
	return nil
}
