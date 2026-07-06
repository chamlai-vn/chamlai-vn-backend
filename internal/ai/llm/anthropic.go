package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const (
	// claude-haiku-4-5 is the cheap dev default; swap to claude-sonnet-4-6 via
	// ANTHROPIC_MODEL for eval without a code change.
	anthropicDefaultModel     = "claude-haiku-4-5-20251001"
	anthropicDefaultMaxTokens = 1024
	anthropicTimeout          = 60 * time.Second
)

// Anthropic is a Service backed by the Anthropic Messages API. It uses forced
// tool use so the model's output always conforms to the request schema. Safe
// for concurrent use.
type Anthropic struct {
	client    anthropic.Client
	model     string
	maxTokens int
	reqOpts   []option.RequestOption // extra per-call SDK options (e.g. base URL in tests)
}

// AnthropicOption configures an Anthropic client.
type AnthropicOption func(*Anthropic)

// WithAnthropicRequestOptions appends SDK request options applied to every call
// (e.g. option.WithBaseURL / option.WithHTTPClient for tests).
func WithAnthropicRequestOptions(opts ...option.RequestOption) AnthropicOption {
	return func(a *Anthropic) { a.reqOpts = append(a.reqOpts, opts...) }
}

// NewAnthropic builds an Anthropic Service from cfg. Model and MaxTokens fall
// back to provider defaults when zero-valued.
func NewAnthropic(cfg AnthropicConfig, opts ...AnthropicOption) *Anthropic {
	model := cfg.Model
	if model == "" {
		model = anthropicDefaultModel
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = anthropicDefaultMaxTokens
	}
	a := &Anthropic{
		client:    anthropic.NewClient(option.WithAPIKey(cfg.APIKey)),
		model:     model,
		maxTokens: maxTokens,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

var _ Service = (*Anthropic)(nil)

func (a *Anthropic) Model() string { return a.model }

// GenerateStructured forces the model to call a single tool whose input schema
// is req.Schema, and returns that tool input's raw JSON.
func (a *Anthropic) GenerateStructured(ctx context.Context, req Request) (json.RawMessage, error) {
	if req.ToolName == "" {
		return nil, fmt.Errorf("llm: anthropic: empty tool name")
	}

	var schema anthropic.ToolInputSchemaParam
	if len(req.Schema) > 0 {
		if err := json.Unmarshal(req.Schema, &schema); err != nil {
			return nil, fmt.Errorf("llm: anthropic: tool schema: %w", err)
		}
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = a.maxTokens
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: int64(maxTokens),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(req.User)),
		},
		Tools: []anthropic.ToolUnionParam{{
			OfTool: &anthropic.ToolParam{
				Name:        req.ToolName,
				Description: anthropic.String(req.ToolDesc),
				InputSchema: schema,
			},
		}},
		ToolChoice: anthropic.ToolChoiceParamOfTool(req.ToolName),
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}

	ctx, cancel := context.WithTimeout(ctx, anthropicTimeout)
	defer cancel()

	msg, err := a.client.Messages.New(ctx, params, a.reqOpts...)
	if err != nil {
		return nil, fmt.Errorf("llm: anthropic: messages: %w", err)
	}

	for _, block := range msg.Content {
		if block.Type == "tool_use" {
			tu := block.AsToolUse()
			if tu.Name == req.ToolName {
				return tu.Input, nil
			}
		}
	}
	return nil, fmt.Errorf("llm: anthropic: no %q tool_use block in response (stop_reason=%s)", req.ToolName, msg.StopReason)
}

// Generate produces a free-text reply from req.System + req.User, with no tool
// use. It concatenates the text blocks of the response.
func (a *Anthropic) Generate(ctx context.Context, req Request) (string, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = a.maxTokens
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: int64(maxTokens),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(req.User)),
		},
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}

	ctx, cancel := context.WithTimeout(ctx, anthropicTimeout)
	defer cancel()

	msg, err := a.client.Messages.New(ctx, params, a.reqOpts...)
	if err != nil {
		return "", fmt.Errorf("llm: anthropic: messages: %w", err)
	}

	var sb strings.Builder
	for _, block := range msg.Content {
		if block.Type == "text" {
			sb.WriteString(block.AsText().Text)
		}
	}
	text := strings.TrimSpace(sb.String())
	if text == "" {
		return "", fmt.Errorf("llm: anthropic: empty text response (stop_reason=%s)", msg.StopReason)
	}
	return text, nil
}
