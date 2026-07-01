// Package config loads runtime configuration from the environment.
//
// Layering mirrors a standard 12-factor setup: an optional .env file (selected
// by `make switch.local`, which copies .env.<name> -> .env) is loaded first,
// then real OS environment variables override it. caarlos0/env maps the result
// onto the Configuration struct via `env` tags.
//
// Configuration is the env-facing type. Domain/wiring types such as
// embedder.Config stay free of env tags so the core packages don't depend on
// how config happens to be sourced; the adapter methods here (e.g. Embedder)
// translate env config into those wiring types.
package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/embedder"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/llm"
)

// Configuration is the full set of env-driven settings for the service.
type Configuration struct {
	DatabaseURL string `env:"DATABASE_URL,required"`

	// HTTP server.
	Port string `env:"PORT" envDefault:"8080"`

	// Provider selection
	EmbedProvider string `env:"EMBED_PROVIDER" envDefault:"voyage"`
	LLMProvider   string `env:"LLM_PROVIDER" envDefault:"anthropic"`

	// Voyage AI (default embedder).
	VoyageAPIKey     string `env:"VOYAGE_API_KEY"`
	VoyageModel      string `env:"VOYAGE_MODEL" envDefault:"voyage-law-2"`
	VoyageDimensions int    `env:"VOYAGE_DIMENSIONS" envDefault:"1024"`

	// Azure OpenAI (alternative provider).
	AzureOpenAIAPIKey     string `env:"AZURE_OPENAI_API_KEY"`
	AzureOpenAIEndpoint   string `env:"AZURE_OPENAI_ENDPOINT"`
	AzureEmbedDeployment  string `env:"AZURE_EMBED_DEPLOYMENT" envDefault:"text-embedding-3-small"`
	AzureOpenAIAPIVersion string `env:"AZURE_OPENAI_API_VERSION"`
	AzureEmbedDimensions  int    `env:"AZURE_EMBED_DIMENSIONS" envDefault:"1536"`

	// Anthropic Claude (default LLM).
	AnthropicAPIKey    string `env:"ANTHROPIC_API_KEY"`
	AnthropicModel     string `env:"ANTHROPIC_MODEL" envDefault:"claude-haiku-4-5-20251001"`
	AnthropicMaxTokens int    `env:"ANTHROPIC_MAX_TOKENS" envDefault:"1024"`
}

// Load reads an optional .env file then overlays OS environment variables and
// parses everything into a Configuration.
//
// A missing .env is not an error (real env / docker may supply everything);
// any other dotenv read error is surfaced.
func Load() (Configuration, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Configuration{}, fmt.Errorf("config: loading .env: %w", err)
	}

	var cfg Configuration
	if err := env.Parse(&cfg); err != nil {
		return Configuration{}, fmt.Errorf("config: parsing env: %w", err)
	}
	return cfg, nil
}

// Embedder adapts the env config into the embedder package's wiring type.
// Empty fields fall through to the provider constructors' zero-value defaults.
func (c Configuration) Embedder() embedder.Config {
	return embedder.Config{
		Provider: embedder.Provider(c.EmbedProvider),
		Voyage: embedder.VoyageConfig{
			APIKey:     c.VoyageAPIKey,
			Model:      c.VoyageModel,
			Dimensions: c.VoyageDimensions,
		},
		Azure: embedder.AzureConfig{
			APIKey:     c.AzureOpenAIAPIKey,
			Endpoint:   c.AzureOpenAIEndpoint,
			Deployment: c.AzureEmbedDeployment,
			APIVersion: c.AzureOpenAIAPIVersion,
			Dimensions: c.AzureEmbedDimensions,
		},
	}
}

// LLM adapts the env config into the llm package's wiring type. Empty fields
// fall through to the provider constructor's zero-value defaults.
func (c Configuration) LLM() llm.Config {
	return llm.Config{
		Provider: llm.Provider(c.LLMProvider),
		Anthropic: llm.AnthropicConfig{
			APIKey:    c.AnthropicAPIKey,
			Model:     c.AnthropicModel,
			MaxTokens: c.AnthropicMaxTokens,
		},
	}
}
