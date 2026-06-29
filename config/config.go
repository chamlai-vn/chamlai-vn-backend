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
)

// Configuration is the full set of env-driven settings for the service.
type Configuration struct {
	// DatabaseURL is the Postgres+pgvector DSN. Defaults to the local docker
	// compose credentials (user/pass/db all "chamlai").
	DatabaseURL string `env:"DATABASE_URL" envDefault:"postgres://chamlai:chamlai@localhost:5432/chamlai?sslmode=disable"`

	// EmbedProvider selects the embedding backend ("voyage" | "azure").
	EmbedProvider string `env:"EMBED_PROVIDER" envDefault:"voyage"`

	// Voyage AI (default provider).
	VoyageAPIKey     string `env:"VOYAGE_API_KEY"`
	VoyageModel      string `env:"VOYAGE_MODEL"`
	VoyageDimensions int    `env:"VOYAGE_DIMENSIONS"`

	// Azure OpenAI (alternative provider).
	AzureOpenAIAPIKey     string `env:"AZURE_OPENAI_API_KEY"`
	AzureOpenAIEndpoint   string `env:"AZURE_OPENAI_ENDPOINT"`
	AzureEmbedDeployment  string `env:"AZURE_EMBED_DEPLOYMENT"`
	AzureOpenAIAPIVersion string `env:"AZURE_OPENAI_API_VERSION"`
	AzureEmbedDimensions  int    `env:"AZURE_EMBED_DIMENSIONS"`
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
