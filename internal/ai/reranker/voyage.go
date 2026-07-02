package reranker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// voyageEndpoint is a var (not const) so tests can point it at an
// httptest.Server.
var voyageEndpoint = "https://api.voyageai.com/v1/rerank"

const voyageDefaultModel = "rerank-2.5"

var errEmptyDocuments = errors.New("reranker: no documents")

// Voyage is a Service backed by the Voyage AI rerank API. No official Go SDK,
// so this calls REST directly. Safe for concurrent use.
type Voyage struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

// VoyageOption configures a Voyage client.
type VoyageOption func(*Voyage)

// WithVoyageHTTPClient injects a custom http.Client.
func WithVoyageHTTPClient(c *http.Client) VoyageOption {
	return func(v *Voyage) { v.httpClient = c }
}

// NewVoyage builds a Voyage reranker from cfg. Model falls back to the
// provider default when zero-valued.
func NewVoyage(cfg VoyageConfig, opts ...VoyageOption) *Voyage {
	model := cfg.Model
	if model == "" {
		model = voyageDefaultModel
	}
	v := &Voyage{
		apiKey:     cfg.APIKey,
		model:      model,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

var _ Service = (*Voyage)(nil)

func (v *Voyage) Model() string { return v.model }

type voyageRerankRequest struct {
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	Model     string   `json:"model"`
	TopK      int      `json:"top_k,omitempty"`
}

type voyageRerankResponse struct {
	Data []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"data"`
}

// Rerank implements Service. On HTTP 429 it retries up to 4 times with
// exponential backoff starting at 20 s (≈ 3 RPM free-tier window), mirroring
// embedder.Voyage.Embed. Truncation of over-long query/documents is left to
// the API's default truncation=true — well within limits for corpus chunks.
func (v *Voyage) Rerank(ctx context.Context, query string, documents []string, topK int) ([]Result, error) {
	if len(documents) == 0 {
		return nil, errEmptyDocuments
	}

	const maxAttempts = 5
	delay := 20 * time.Second

	for attempt := 0; attempt < maxAttempts; attempt++ {
		results, err := v.rerankOnce(ctx, query, documents, topK)
		if err == nil {
			return results, nil
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

func (v *Voyage) rerankOnce(ctx context.Context, query string, documents []string, topK int) ([]Result, error) {
	reqBody := voyageRerankRequest{
		Query:     query,
		Documents: documents,
		Model:     v.model,
	}
	if topK > 0 {
		reqBody.TopK = topK
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("reranker: voyage marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, voyageEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("reranker: voyage request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+v.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reranker: voyage call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("reranker: voyage status %d: %s", resp.StatusCode, bytes.TrimSpace(msg))
	}

	var out voyageRerankResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("reranker: voyage decode: %w", err)
	}

	seen := make(map[int]bool, len(out.Data))
	results := make([]Result, len(out.Data))
	for i, d := range out.Data {
		if d.Index < 0 || d.Index >= len(documents) {
			return nil, fmt.Errorf("reranker: voyage out-of-range index %d", d.Index)
		}
		if seen[d.Index] {
			return nil, fmt.Errorf("reranker: voyage duplicate index %d", d.Index)
		}
		seen[d.Index] = true
		results[i] = Result{Index: d.Index, RelevanceScore: d.RelevanceScore}
	}
	return results, nil
}
