package embedder

import (
	"context"
)

// Service abstracts over different embedding providers (Azure OpenAI, Voyage AI, etc).
type Service interface {
	// Embed returns one vector per input text, in the same order as texts.
	// inputType signals document-vs-query intent to providers that support it.
	Embed(ctx context.Context, texts []string, inputType InputType) ([][]float32, error)

	// Dimensions reports the vector size produced. Callers assert this matches
	// the DB column (chunks.embedding vector(N)) so a model swap can't silently
	// corrupt the index.
	Dimensions() int

	// Model returns the underlying model/deployment id, for logging + eval metadata.
	Model() string
}
