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

// AzureOpenAI is a Service backed by Azure OpenAI embeddings. Azure has no
// document-vs-query distinction, so InputType is accepted but ignored.
// Safe for concurrent use.
type AzureOpenAI struct {
	apiKey     string
	endpoint   string // resource base, e.g. https://my-res.openai.azure.com
	deployment string // deployment name, e.g. text-embedding-3-small
	apiVersion string
	dimensions int
	httpClient *http.Client
}

// AzureOption configures an Azure client.
type AzureOption func(*AzureOpenAI)

// WithAzureHTTPClient injects a custom http.Client.
func WithAzureHTTPClient(c *http.Client) AzureOption {
	return func(a *AzureOpenAI) { a.httpClient = c }
}

// NewAzureOpenAI builds an Azure OpenAI embedder.
func NewAzureOpenAI(cfg AzureConfig, opts ...AzureOption) *AzureOpenAI {
	dims := cfg.Dimensions
	if dims == 0 {
		dims = azureOpenAIDefaultDims
	}
	a := &AzureOpenAI{
		apiKey:     cfg.APIKey,
		endpoint:   strings.TrimRight(cfg.Endpoint, "/"),
		deployment: cfg.Deployment,
		apiVersion: cfg.APIVersion,
		dimensions: dims,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

var _ Service = (*AzureOpenAI)(nil)

func (a *AzureOpenAI) Dimensions() int { return a.dimensions }
func (a *AzureOpenAI) Model() string   { return a.deployment }

// Embed implements Service. inputType is ignored (Azure has no such concept).
func (a *AzureOpenAI) Embed(ctx context.Context, texts []string, _ InputType) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, errEmptyInput
	}

	reqBody := azureRequest{Input: texts}
	// Send dimensions only when diverging from default (older embedding models
	// don't support the param).
	if a.dimensions != azureOpenAIDefaultDims {
		reqBody.Dimensions = a.dimensions
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("embedder: azure marshal: %w", err)
	}

	url := fmt.Sprintf("%s/openai/deployments/%s/embeddings?api-version=%s",
		a.endpoint, a.deployment, a.apiVersion)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embedder: azure request: %w", err)
	}
	req.Header.Set("api-key", a.apiKey)
	req.Header.Set("Content-Type", "application/json")

	return doEmbed(a.httpClient, req, len(texts), "azure")
}
