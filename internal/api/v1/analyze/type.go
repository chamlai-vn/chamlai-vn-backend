package analyze

// Request is the POST /v1/analyze request body.
type Request struct {
	// Text is the suspicious message to score. Required; capped well above
	// any real chat message to keep embedding/LLM cost bounded.
	Text string `json:"text" validate:"required,max=5000"`
}
