package chat

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// ErrNoUserMessage means the conversation didn't end with a non-empty user
// message. The HTTP layer maps this to a 400.
var ErrNoUserMessage = fmt.Errorf("chat: last message must be a non-empty user message")

// Reply routes one conversation turn and produces the response. The last
// message must be from the user. Routing always fails safe toward scam
// detection (see route): a scam can never be routed away from being checked.
func (c *Chatter) Reply(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	latest, ok := latestUserMessage(req.Messages)
	if !ok {
		return nil, ErrNoUserMessage
	}
	history := recentHistory(req.Messages, c.historyWindow)

	switch c.route(ctx, history, latest) {
	case IntentProject:
		return c.answer(ctx, IntentProject, history, latest)
	case IntentFounder:
		return c.answer(ctx, IntentFounder, history, latest)
	default:
		return c.detectScam(ctx, latest)
	}
}

// route applies the deterministic fail-safe policy. Suspicious markers, any
// router failure, an unknown intent, or a low-confidence project/founder guess
// all collapse to scam — the safety-critical property lives in this code, not
// in the model's discretion.
func (c *Chatter) route(ctx context.Context, history []Message, latest string) Intent {
	if looksSuspicious(latest) {
		return IntentScam
	}
	intent, confidence, err := c.classifier.Classify(ctx, history, latest)
	if err != nil {
		// Never let a router failure skip scam detection.
		slog.WarnContext(ctx, "chat router failed, defaulting to scam", "error", err)
		return IntentScam
	}
	switch intent {
	case IntentProject, IntentFounder:
		if confidence >= c.confidence {
			return intent
		}
		return IntentScam
	default:
		return IntentScam
	}
}

// answer handles the project/founder flows.
func (c *Chatter) answer(ctx context.Context, intent Intent, history []Message, latest string) (*ChatResponse, error) {
	reply, err := c.answerer.Answer(ctx, intent, history, latest)
	if err != nil {
		return nil, fmt.Errorf("chat: answer: %w", err)
	}
	return &ChatResponse{Intent: intent, Reply: reply}, nil
}

// detectScam runs the retrieve→score pipeline on the latest user message and
// returns the structured verdict untouched (its risk level is the source of
// truth; no conversational text overwrites it in v1).
func (c *Chatter) detectScam(ctx context.Context, latest string) (*ChatResponse, error) {
	chunks, err := c.retriever.HybridSearch(ctx, latest, c.topK)
	if err != nil {
		return nil, fmt.Errorf("chat: retrieve: %w", err)
	}
	result, err := c.scorer.Score(ctx, latest, chunks)
	if err != nil {
		return nil, fmt.Errorf("chat: score: %w", err)
	}
	return &ChatResponse{Intent: IntentScam, Analysis: result}, nil
}

// latestUserMessage returns the trimmed content of the final message when it is
// a non-empty user message.
func latestUserMessage(messages []Message) (string, bool) {
	if len(messages) == 0 {
		return "", false
	}
	last := messages[len(messages)-1]
	if last.Role != RoleUser {
		return "", false
	}
	text := strings.TrimSpace(last.Content)
	if text == "" {
		return "", false
	}
	return text, true
}

// recentHistory returns up to window messages preceding the latest one.
func recentHistory(messages []Message, window int) []Message {
	if len(messages) <= 1 {
		return nil
	}
	prior := messages[:len(messages)-1]
	if len(prior) > window {
		prior = prior[len(prior)-window:]
	}
	return prior
}
