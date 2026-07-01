package llm

import (
	"context"
	"encoding/json"
)

// Service abstracts over LLM providers for structured generation. The only
// implementation today is Anthropic (anthropic.go); the interface exists so a
// provider swap stays a config/code-local change. Mirrors embedder.Service.
type Service interface {
	// GenerateStructured forces the model to emit JSON conforming to the tool
	// schema in req (via forced tool use) and returns the raw tool input. The
	// caller owns unmarshalling into its own DTO.
	GenerateStructured(ctx context.Context, req Request) (json.RawMessage, error)

	// Model returns the underlying model id, for logging + eval metadata.
	Model() string
}

// Request is one structured-generation call. Schema is a JSON Schema object
// ({"type":"object","properties":{...},"required":[...]}) describing the tool
// input the model must produce.
type Request struct {
	System    string          // system prompt
	User      string          // composed user prompt
	ToolName  string          // forced tool name, e.g. "record_scam_analysis"
	ToolDesc  string          // tool description (helps the model fill it well)
	Schema    json.RawMessage // JSON Schema for the tool input_schema
	MaxTokens int             // 0 → provider default
}
