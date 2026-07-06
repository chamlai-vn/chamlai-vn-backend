package chat

import "github.com/chamlai-vn/chamlai-vn-backend/internal/scam/analyzer"

// Intent is the routing decision for one chat turn.
type Intent string

const (
	// IntentProject — the user is asking about the ChậmLại.vn project.
	IntentProject Intent = "project"
	// IntentFounder — the user is asking about the founder, Nguyễn Văn Biên.
	IntentFounder Intent = "founder"
	// IntentScam — everything else: run the scam-detection pipeline. This is the
	// fail-safe default (see Chatter.route).
	IntentScam Intent = "scam"
)

// Roles a chat message can carry.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Message is one turn in the conversation history.
type Message struct {
	Role    string `json:"role"`    // "user" | "assistant"
	Content string `json:"content"` // the message text
}

// ChatRequest is a multi-turn conversation. The last message must be from the
// user; earlier messages are context for routing and answering.
type ChatRequest struct {
	Messages []Message
}

// ChatResponse is the reply for one turn. Intent tells the client which flow
// handled it. Reply is the conversational text (present for project/founder;
// empty for scam in v1). Analysis carries the structured verdict and is present
// only for scam turns — its red/yellow/green is the source of truth and is
// never narrated over.
type ChatResponse struct {
	Intent   Intent                   `json:"intent"`
	Reply    string                   `json:"reply"`
	Analysis *analyzer.AnalysisResult `json:"analysis"`
}
