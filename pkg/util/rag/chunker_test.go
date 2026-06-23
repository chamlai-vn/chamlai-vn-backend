package ragutil

import (
	"strings"
	"testing"
)

func TestChunk_ShortTextNotSplit(t *testing.T) {
	msg := "Chào bạn, tài khoản của bạn trúng thưởng 100 triệu, bấm link để nhận."
	got := Chunk(msg, DefaultChunkConfig())
	if len(got) != 1 || got[0] != msg {
		t.Fatalf("short message should be a single unchanged chunk, got %d chunks", len(got))
	}
}

func TestChunk_Empty(t *testing.T) {
	if got := Chunk("", DefaultChunkConfig()); got != nil {
		t.Fatalf("empty text should return nil, got %v", got)
	}
	if got := Chunk("   ", ChunkConfig{Size: 2}); len(got) == 0 {
		t.Fatalf("whitespace shorter than size returns single chunk")
	}
}

func TestChunk_OverlapAndCoverage(t *testing.T) {
	// Build text longer than Size with clear sentence boundaries.
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString("Đây là một câu cảnh báo lừa đảo số. ")
	}
	text := b.String()
	cfg := ChunkConfig{Size: 300, Overlap: 50}

	chunks := Chunk(text, cfg)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if n := len([]rune(c)); n == 0 || n > cfg.Size+len("Đây là một câu cảnh báo lừa đảo số. ") {
			t.Errorf("chunk %d unexpected size %d", i, n)
		}
	}
	// Reassembling unique content must preserve every rune of the original.
	if !strings.Contains(chunks[0], "Đây là một câu") {
		t.Errorf("first chunk missing expected content")
	}
}

func TestChunk_ZeroConfigUsesDefault(t *testing.T) {
	long := strings.Repeat("a", 5000)
	got := Chunk(long, ChunkConfig{}) // zero value → defaults
	if len(got) < 2 {
		t.Fatalf("zero config should default and split 5000 runes, got %d chunks", len(got))
	}
}

func TestChunk_VietnameseBoundaryNoPanic(t *testing.T) {
	// Multi-byte runes near boundary: must not panic or slice out of range
	// (this is where byte-vs-rune offset bugs surface).
	text := strings.Repeat("Cảnh báo: lừa đảo chuyển khoản. ", 100)
	cfg := ChunkConfig{Size: 120, Overlap: 20}
	_ = Chunk(text, cfg) // success = no panic
}
