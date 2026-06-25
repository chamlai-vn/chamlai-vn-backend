package ingest

import (
	"context"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/embedder"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/store"
	ragutil "github.com/chamlai-vn/chamlai-vn-backend/pkg/util/rag"
)

// defaultBatchSize caps how many chunks are embedded per provider call. Voyage
// accepts up to 1000 inputs per request; we stay well under that and split
// larger documents across calls. Most scam articles produce only a handful of
// chunks, so this rarely splits.
const defaultBatchSize = 128

// Store is the persistence the indexer needs. *store.Store satisfies it; tests
// supply a fake. Kept narrow on purpose — the indexer only ever stores a whole
// document with its chunks in one atomic unit.
type Store interface {
	InsertDocumentWithChunks(ctx context.Context, url, title, content, scamType, source string, chunks []store.Chunk) (int64, error)
}

// Indexer runs the chunk → embed → store pipeline. Safe for concurrent use if
// its embedder and store are (both are).
type Indexer struct {
	emb       embedder.Service
	store     Store
	chunkCfg  ragutil.ChunkConfig
	batchSize int
}

// Option configures an Indexer. Zero-value defaults are applied in New.
type Option func(*Indexer)

// WithChunkConfig overrides the chunking parameters (default
// ragutil.DefaultChunkConfig).
func WithChunkConfig(cfg ragutil.ChunkConfig) Option {
	return func(ix *Indexer) { ix.chunkCfg = cfg }
}

// WithBatchSize overrides how many chunks are embedded per provider call.
// Non-positive values are ignored.
func WithBatchSize(n int) Option {
	return func(ix *Indexer) {
		if n > 0 {
			ix.batchSize = n
		}
	}
}

// New builds an Indexer over emb and st. Unset options fall back to the corpus
// defaults (DefaultChunkConfig, defaultBatchSize).
func New(emb embedder.Service, st Store, opts ...Option) *Indexer {
	ix := &Indexer{
		emb:       emb,
		store:     st,
		chunkCfg:  ragutil.DefaultChunkConfig(),
		batchSize: defaultBatchSize,
	}
	for _, opt := range opts {
		opt(ix)
	}
	return ix
}
