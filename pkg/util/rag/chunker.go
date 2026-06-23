// Package ragutil holds reusable, dependency-free helpers for the RAG
// pipeline (chunking, and later text normalisation). Everything here is a
// plain function — no Service interface, no Config injection — so both the
// indexing path (cmd/crawler) and the query path (internal/analyzer) can use
// it without wiring.
package ragutil

import "strings"

// ChunkConfig controls how Chunk splits text. Sizes are counted in runes (not
// bytes) so Vietnamese multi-byte characters are counted as one unit each.
type ChunkConfig struct {
	// Size is the target maximum number of runes per chunk.
	Size int
	// Overlap is how many runes the start of each chunk repeats from the end
	// of the previous one, to avoid cutting a scam signal across a boundary.
	Overlap int
}

// DefaultChunkConfig returns sensible defaults for the scam corpus:
// ~512 tokens per chunk at ~4 chars/token, ~50 token overlap.
func DefaultChunkConfig() ChunkConfig {
	return ChunkConfig{Size: 2048, Overlap: 200}
}

// splitBoundaries are tried in order to end a chunk on a natural boundary
// (paragraph, line, sentence, word) instead of mid-word.
var splitBoundaries = []string{"\n\n", "\n", ". ", " "}

// Chunk splits text into overlapping chunks according to cfg.
//
// Behaviour worth knowing for the scam use case:
//   - Empty/whitespace-only text returns nil.
//   - Text shorter than cfg.Size returns a single chunk unchanged — a short
//     SMS/Zalo message is NOT force-split.
//   - Invalid cfg values are repaired in place (see normalise), so callers can
//     pass a zero ChunkConfig and still get DefaultChunkConfig behaviour.
func Chunk(text string, cfg ChunkConfig) []string {
	cfg = cfg.normalize()

	runes := []rune(text)
	total := len(runes)
	if total == 0 {
		return nil
	}
	if total <= cfg.Size {
		return []string{text}
	}

	var chunks []string
	start := 0
	for start < total {
		end := start + cfg.Size
		if end >= total {
			chunks = append(chunks, string(runes[start:]))
			break
		}

		splitAt := findBoundary(runes, start, end, cfg.Size)
		chunks = append(chunks, string(runes[start:splitAt]))

		// Next chunk starts cfg.Overlap runes before the boundary so context
		// carries over. Guard against non-advancing starts (infinite loop).
		nextStart := splitAt - cfg.Overlap
		if nextStart <= start {
			nextStart = splitAt
		}
		start = nextStart
	}
	return chunks
}

// normalize repairs invalid config: non-positive Size falls back to the
// default, and Overlap is clamped to [0, Size/2) so it can never stall the loop
// or swallow a whole chunk.
func (c ChunkConfig) normalize() ChunkConfig {
	def := DefaultChunkConfig()
	if c.Size <= 0 {
		c.Size = def.Size
	}
	if c.Overlap < 0 {
		c.Overlap = 0
	}
	if c.Overlap >= c.Size {
		c.Overlap = c.Size / 2
	}
	return c
}

// findBoundary looks backwards from end for a natural split point. Only
// boundaries at or beyond size/2 are accepted: a rare early separator (e.g. a
// lone "\n\n" near the chunk start) would otherwise collapse the chunk into a
// tiny, overlap-free fragment and repeat. Falls back to the hard rune limit if
// no suitable boundary is found.
func findBoundary(runes []rune, start, end, size int) int {
	minBoundary := size / 2
	window := string(runes[start:end])
	for _, sep := range splitBoundaries {
		idx := strings.LastIndex(window, sep)
		if idx >= minBoundary {
			// idx is a byte offset into window; convert the prefix to rune count.
			return start + len([]rune(window[:idx])) + len([]rune(sep))
		}
	}
	return end
}
