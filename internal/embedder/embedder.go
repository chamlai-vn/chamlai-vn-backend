// Package embedder turns text into vectors for retrieval.
//
// The Service interface (service.go) is the contract the rest of the codebase
// depends on. Concrete providers live in sibling files (azureopenai.go,
// voyage.go); New picks one from Config so swapping provider/model is a config
// change, not a code change.
package embedder

import "fmt"

// Provider selects which embedding backend New constructs.
type Provider string

const (
	ProviderAzure  Provider = "azure"  // Azure OpenAI (text-embedding-3-*)
	ProviderVoyage Provider = "voyage" // Voyage AI (voyage-3.5, ...)
)

// AzureConfig holds the Azure OpenAI connection settings (from env).
type AzureConfig struct {
	APIKey     string // AZURE_OPENAI_API_KEY
	Endpoint   string // AZURE_OPENAI_ENDPOINT
	Deployment string // AZURE_EMBED_DEPLOYMENT
	APIVersion string // AZURE_OPENAI_API_VERSION
	Dimensions int    // optional; 0 → provider default (1536)
}

// VoyageConfig holds the Voyage AI connection settings (from env).
type VoyageConfig struct {
	APIKey     string // VOYAGE_API_KEY
	Model      string // optional; "" → voyage-3.5
	Dimensions int    // optional; 0 → 1024
}

// Config selects and configures an embedding provider
type Config struct {
	Provider Provider
	Azure    AzureConfig
	Voyage   VoyageConfig
}

// New builds the Service for the configured provider.
func New(cfg Config) (Service, error) {
	switch cfg.Provider {
	case ProviderAzure:
		return NewAzureOpenAI(cfg.Azure), nil

	case ProviderVoyage:
		return NewVoyage(cfg.Voyage), nil

	default:
		return nil, fmt.Errorf("embedder: unknown provider %q", cfg.Provider)
	}
}
