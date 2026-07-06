package chat

// Message is one turn in the request conversation history.
type Message struct {
	// Role is who sent the message: "user" or "assistant".
	Role string `json:"role" validate:"required,oneof=user assistant"`
	// Content is the message text. Capped to bound embedding/LLM cost.
	Content string `json:"content" validate:"required,max=10000"`
}

// Request is the POST /v1/chat request body: a multi-turn conversation whose
// last message must be from the user (enforced in the domain layer).
type Request struct {
	// Messages is the ordered conversation history, oldest first. Required and
	// capped to keep prompt size bounded.
	Messages []Message `json:"messages" validate:"required,min=1,max=50,dive"`
}
