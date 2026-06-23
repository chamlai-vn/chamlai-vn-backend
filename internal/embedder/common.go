package embedder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// text-embedding-3-small outputs 1536 dims by default (truncatable via the
// dimensions param). Mirror whatever you choose in the DB vector(N) column.
const azureOpenAIDefaultDims = 1536

// InputType tells the provider how a batch of text will be used. Retrieval
// models embed corpus documents and search queries slightly differently to
// maximise match quality, so callers must say which they mean.
//
// Index the corpus with InputDocument; embed user queries with InputQuery.
// Providers that have no such distinction (e.g. Azure OpenAI) simply ignore it.
type InputType string

// consts for InputType
const (
	InputDocument InputType = "document"
	InputQuery    InputType = "query"
)

// doEmbed runs a prepared embeddings request and maps the returned vectors back
// into the caller's input order. want is the number of input texts; provider
// labels error messages. Shared by every REST-based Service so the response
// handling (status check, decode, index validation) lives in one place.
func doEmbed(client *http.Client, req *http.Request, want int, provider string) ([][]float32, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedder: %s call: %w", provider, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("embedder: %s status %d: %s", provider, resp.StatusCode, bytes.TrimSpace(msg))
	}

	var out embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("embedder: %s decode: %w", provider, err)
	}
	if len(out.Data) != want {
		return nil, fmt.Errorf("embedder: %s expected %d vectors, got %d", provider, want, len(out.Data))
	}

	vectors := make([][]float32, len(out.Data))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(vectors) {
			return nil, fmt.Errorf("embedder: %s out-of-range index %d", provider, d.Index)
		}
		vectors[d.Index] = d.Embedding
	}
	return vectors, nil
}
