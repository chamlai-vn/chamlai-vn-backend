package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

const (
	// gpt-5.4-mini is the cheap eval target; swap via OPENAI_MODEL without a code
	// change. Default max tokens is higher than the other providers' because
	// "mini" reasoning models can spend tokens before emitting the tool call.
	openaiDefaultModel     = "gpt-5.4-mini"
	openaiDefaultMaxTokens = 2048
	openaiTimeout          = 60 * time.Second
)

// OpenAI is a Service backed by the OpenAI Chat Completions API. It forces a
// single tool call so the model's output always conforms to the request schema,
// mirroring the Anthropic forced-tool-use path. Safe for concurrent use.
type OpenAI struct {
	client    openai.Client
	model     string
	maxTokens int
	reqOpts   []option.RequestOption // extra per-call SDK options (e.g. base URL in tests)
}

// OpenAIOption configures an OpenAI client.
type OpenAIOption func(*OpenAI)

// WithOpenAIRequestOptions appends SDK request options applied to every call
// (e.g. option.WithBaseURL / option.WithHTTPClient for tests).
func WithOpenAIRequestOptions(opts ...option.RequestOption) OpenAIOption {
	return func(o *OpenAI) { o.reqOpts = append(o.reqOpts, opts...) }
}

// NewOpenAI builds an OpenAI Service from cfg. Model and MaxTokens fall back to
// provider defaults when zero-valued.
func NewOpenAI(cfg OpenAIConfig, opts ...OpenAIOption) *OpenAI {
	model := cfg.Model
	if model == "" {
		model = openaiDefaultModel
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = openaiDefaultMaxTokens
	}
	o := &OpenAI{
		client:    openai.NewClient(option.WithAPIKey(cfg.APIKey)),
		model:     model,
		maxTokens: maxTokens,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

var _ Service = (*OpenAI)(nil)

func (o *OpenAI) Model() string { return o.model }

// GenerateStructured forces the model to call a single function whose parameters
// schema is req.Schema, and returns that call's arguments as raw JSON.
func (o *OpenAI) GenerateStructured(ctx context.Context, req Request) (json.RawMessage, error) {
	if req.ToolName == "" {
		return nil, fmt.Errorf("llm: openai: empty tool name")
	}

	var params shared.FunctionParameters
	if len(req.Schema) > 0 {
		if err := json.Unmarshal(req.Schema, &params); err != nil {
			return nil, fmt.Errorf("llm: openai: tool schema: %w", err)
		}
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = o.maxTokens
	}

	fn := shared.FunctionDefinitionParam{Name: req.ToolName, Parameters: params}
	if req.ToolDesc != "" {
		fn.Description = openai.String(req.ToolDesc)
	}

	messages := make([]openai.ChatCompletionMessageParamUnion, 0, 2)
	if req.System != "" {
		messages = append(messages, openai.SystemMessage(req.System))
	}
	messages = append(messages, openai.UserMessage(req.User))

	params2 := openai.ChatCompletionNewParams{
		Model:               openai.ChatModel(o.model),
		Messages:            messages,
		MaxCompletionTokens: openai.Int(int64(maxTokens)),
		Tools:               []openai.ChatCompletionToolParam{{Function: fn}},
		ToolChoice: openai.ChatCompletionToolChoiceOptionParamOfChatCompletionNamedToolChoice(
			openai.ChatCompletionNamedToolChoiceFunctionParam{Name: req.ToolName},
		),
	}

	ctx, cancel := context.WithTimeout(ctx, openaiTimeout)
	defer cancel()

	resp, err := o.client.Chat.Completions.New(ctx, params2, o.reqOpts...)
	if err != nil {
		return nil, fmt.Errorf("llm: openai: completions: %w", err)
	}

	for _, choice := range resp.Choices {
		for _, tc := range choice.Message.ToolCalls {
			if tc.Function.Name == req.ToolName {
				return json.RawMessage(tc.Function.Arguments), nil
			}
		}
	}

	var reason string
	if len(resp.Choices) > 0 {
		reason = string(resp.Choices[0].FinishReason)
	}
	return nil, fmt.Errorf("llm: openai: no %q tool call in response (finish_reason=%s)", req.ToolName, reason)
}
