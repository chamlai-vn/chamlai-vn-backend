package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/genai"
)

const (
	// gemini-3.5-flash is the cheap eval target; swap via GEMINI_MODEL without a
	// code change.
	geminiDefaultModel     = "gemini-3.5-flash"
	geminiDefaultMaxTokens = 1024
	geminiTimeout          = 60 * time.Second
)

// Gemini is a Service backed by the Google Gemini API (Developer backend). It
// forces a single function call so the model's output always conforms to the
// request schema, mirroring the Anthropic forced-tool-use path. Safe for
// concurrent use.
type Gemini struct {
	client    *genai.Client
	model     string
	maxTokens int
}

// GeminiOption mutates the genai.ClientConfig before the client is built. Used
// by tests to point the SDK at an httptest.Server (ClientConfig.HTTPOptions.BaseURL
// / ClientConfig.HTTPClient).
type GeminiOption func(*genai.ClientConfig)

// WithGeminiClientConfig applies fn to the genai.ClientConfig before construction.
func WithGeminiClientConfig(fn func(*genai.ClientConfig)) GeminiOption { return fn }

// NewGemini builds a Gemini Service from cfg. Model and MaxTokens fall back to
// provider defaults when zero-valued. It returns an error because the genai
// client can fail to initialise (e.g. credential resolution).
func NewGemini(cfg GeminiConfig, opts ...GeminiOption) (*Gemini, error) {
	model := cfg.Model
	if model == "" {
		model = geminiDefaultModel
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = geminiDefaultMaxTokens
	}

	cc := &genai.ClientConfig{
		APIKey:  cfg.APIKey,
		Backend: genai.BackendGeminiAPI,
	}
	for _, opt := range opts {
		opt(cc)
	}

	client, err := genai.NewClient(context.Background(), cc)
	if err != nil {
		return nil, fmt.Errorf("llm: gemini: new client: %w", err)
	}
	return &Gemini{client: client, model: model, maxTokens: maxTokens}, nil
}

var _ Service = (*Gemini)(nil)

func (g *Gemini) Model() string { return g.model }

// GenerateStructured forces the model to call a single function whose parameters
// schema is req.Schema, and returns that function call's arguments as raw JSON.
func (g *Gemini) GenerateStructured(ctx context.Context, req Request) (json.RawMessage, error) {
	if req.ToolName == "" {
		return nil, fmt.Errorf("llm: gemini: empty tool name")
	}

	// Gemini accepts a raw JSON Schema for function parameters via
	// ParametersJsonSchema (any); unmarshal req.Schema into a generic value.
	var params any
	if len(req.Schema) > 0 {
		if err := json.Unmarshal(req.Schema, &params); err != nil {
			return nil, fmt.Errorf("llm: gemini: tool schema: %w", err)
		}
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = g.maxTokens
	}

	fn := &genai.FunctionDeclaration{
		Name:                 req.ToolName,
		Description:          req.ToolDesc,
		ParametersJsonSchema: params,
	}
	config := &genai.GenerateContentConfig{
		Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{fn}}},
		ToolConfig: &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode:                 genai.FunctionCallingConfigModeAny,
				AllowedFunctionNames: []string{req.ToolName},
			},
		},
		MaxOutputTokens: int32(maxTokens),
	}
	if req.System != "" {
		config.SystemInstruction = genai.NewContentFromText(req.System, genai.RoleUser)
	}
	contents := []*genai.Content{genai.NewContentFromText(req.User, genai.RoleUser)}

	ctx, cancel := context.WithTimeout(ctx, geminiTimeout)
	defer cancel()

	resp, err := g.client.Models.GenerateContent(ctx, g.model, contents, config)
	if err != nil {
		return nil, fmt.Errorf("llm: gemini: generate: %w", err)
	}

	var reason genai.FinishReason
	if len(resp.Candidates) > 0 {
		reason = resp.Candidates[0].FinishReason
	}
	// MAX_TOKENS truncation is the specific, actionable case: unlike
	// Anthropic (which can still hand back a partial-but-parseable tool_use
	// block when truncated), Gemini's own validation rejects an incomplete
	// function call outright as MALFORMED_FUNCTION_CALL — but that generic
	// reason alone doesn't tell the caller *why*. Surface the token-limit
	// cause explicitly so it's obvious MaxTokens needs raising, not that the
	// schema or prompt is wrong.
	if reason == genai.FinishReasonMaxTokens {
		return nil, fmt.Errorf("llm: gemini: response truncated at max_tokens (%d) — increase Request.MaxTokens", maxTokens)
	}

	for _, cand := range resp.Candidates {
		if cand.Content == nil {
			continue
		}
		for _, part := range cand.Content.Parts {
			if part.FunctionCall != nil && part.FunctionCall.Name == req.ToolName {
				args, err := json.Marshal(part.FunctionCall.Args)
				if err != nil {
					return nil, fmt.Errorf("llm: gemini: marshal args: %w", err)
				}
				return args, nil
			}
		}
	}

	if reason == genai.FinishReasonMalformedFunctionCall {
		return nil, fmt.Errorf("llm: gemini: model produced a malformed %q function call — often caused by running out of output tokens mid-generation; try increasing Request.MaxTokens (currently %d)", req.ToolName, maxTokens)
	}
	return nil, fmt.Errorf("llm: gemini: no %q function call in response (finish_reason=%s)", req.ToolName, reason)
}
