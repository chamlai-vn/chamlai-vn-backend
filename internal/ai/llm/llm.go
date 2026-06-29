// Package llm provides structured text generation backed by an LLM provider.
//
// The Service interface (service.go) is the contract the rest of the codebase
// depends on; the only provider today is Anthropic (anthropic.go). New picks a
// provider from Config so swapping it is a config change, not a code change.
// This mirrors internal/ai/embedder: New lives here (the package-named file),
// the Service interface in service.go.
//
// Adding a provider: add a <Provider>Config struct + a field on Config + a
// Provider constant here, then a <provider>.go with the Service impl and a
// constructor of the shape New<Provider>(cfg <Provider>Config, opts ...).
package llm

import "fmt"

// Provider selects which LLM backend New constructs.
type Provider string

// ProviderAnthropic is the only backend today (Claude Messages API).
const ProviderAnthropic Provider = "anthropic"

// AnthropicConfig holds the Anthropic connection settings (from env).
type AnthropicConfig struct {
	APIKey    string // ANTHROPIC_API_KEY
	Model     string // optional; "" → provider default (Haiku)
	MaxTokens int    // optional; 0 → provider default
}

// Config selects and configures an LLM provider.
type Config struct {
	Provider  Provider
	Anthropic AnthropicConfig
}

// New builds the Service for the configured provider. Key presence is the
// caller's responsibility (validated at the command boundary, like cmd/seed),
// so missing-key failures surface as a clear API error at call time rather than
// coupling every command to every provider's secrets.
func New(cfg Config) (Service, error) {
	switch cfg.Provider {
	case ProviderAnthropic:
		return NewAnthropic(cfg.Anthropic), nil
	default:
		return nil, fmt.Errorf("llm: unknown provider %q", cfg.Provider)
	}
}
