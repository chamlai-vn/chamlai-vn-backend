package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const voyageEndpoint = "https://api.voyageai.com/v1/embeddings"

// voyage-context-4 outputs 1024 dims by default — the size to mirror in the
// chunks.embedding vector(N) column when using Voyage.
const (
	voyageDefaultModel = "voyage-context-4"
	voyageDefaultDims  = 1024
)

// Voyage is a Service backed by the Voyage AI embeddings API (Anthropic-
// recommended for RAG). No official Go SDK, so this calls REST directly.
// Safe for concurrent use.
type Voyage struct {
	apiKey     string
	model      string
	dimensions int
	httpClient *http.Client
}

// VoyageOption configures a Voyage client.
type VoyageOption func(*Voyage)

// WithVoyageHTTPClient injects a custom http.Client.
func WithVoyageHTTPClient(c *http.Client) VoyageOption {
	return func(v *Voyage) { v.httpClient = c }
}

// NewVoyage builds a Voyage embedder from cfg. Model and Dimensions fall back
// to provider defaults when zero-valued.
func NewVoyage(cfg VoyageConfig, opts ...VoyageOption) *Voyage {
	model := cfg.Model
	if model == "" {
		model = voyageDefaultModel
	}
	dims := cfg.Dimensions
	if dims == 0 {
		dims = voyageDefaultDims
	}
	v := &Voyage{
		apiKey:     cfg.APIKey,
		model:      model,
		dimensions: dims,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

var _ Service = (*Voyage)(nil)

func (v *Voyage) Dimensions() int { return v.dimensions }
func (v *Voyage) Model() string   { return v.model }

// Embed implements Service. On HTTP 429 it retries up to 4 times with
// exponential backoff starting at 20 s (≈ 3 RPM free-tier window).
func (v *Voyage) Embed(ctx context.Context, texts []string, inputType InputType) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, errEmptyInput
	}

	const maxAttempts = 5
	delay := 20 * time.Second

	for attempt := 0; attempt < maxAttempts; attempt++ {
		vecs, err := v.embedOnce(ctx, texts, inputType)
		if err == nil {
			return vecs, nil
		}
		if !strings.Contains(err.Error(), "status 429") || attempt == maxAttempts-1 {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		delay *= 2
	}
	panic("unreachable")
}

func (v *Voyage) embedOnce(ctx context.Context, texts []string, inputType InputType) ([][]float32, error) {
	reqBody := voyageRequest{
		Input:     texts,
		Model:     v.model,
		InputType: string(inputType),
	}
	// Send output_dimension only when it diverges from the model default, to
	// avoid asking a model for an unsupported size.
	if v.dimensions != voyageDefaultDims {
		reqBody.OutputDimension = v.dimensions
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("embedder: voyage marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, voyageEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embedder: voyage request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+v.apiKey)
	req.Header.Set("Content-Type", "application/json")

	return doEmbed(v.httpClient, req, len(texts), "voyage")
}
