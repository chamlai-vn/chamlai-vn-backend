// Package embedder turns text into vectors for retrieval.
//
// The Service interface (service.go) is the contract the rest of the codebase
// depends on. Concrete providers live in sibling files (azureopenai.go,
// voyage.go); New picks one from Config so swapping provider/model is a config
// change, not a code change.
//
// Adding a provider:
//  1. Add a <Provider>Config struct here and a field for it on Config, plus a
//     Provider constant.
//  2. Add <provider>.go with the Service impl and a constructor of the canonical
//     shape New<Provider>(cfg <Provider>Config, opts ...<Provider>Option). Apply
//     zero-value defaults inside the constructor; keep Option types per provider
//     (do not share one global Option type).
//  3. Add a case to New.
//
// OpenAI-compatible REST providers (response is data[].embedding keyed by index)
// reuse doEmbed in common.go. Providers with a different transport or response
// shape — e.g. AWS Bedrock (SigV4 + aws.Config) or Google Vertex AI (ADC token
// source) — carry the extra setup in their Config and skip doEmbed, but still
// satisfy Service.
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
	Model      string // optional; "" → voyage-4
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
