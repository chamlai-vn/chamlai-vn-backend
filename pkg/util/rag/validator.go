package ragutil

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	minWords    = 20
	maxWords    = 500_000
	minReadable = 0.5
	maxTextSize = 10 << 20 // 10 MB hard cap on extracted text

	// OOXML archive (DOCX/XLSX/PPTX) safety limits — all three formats are ZIP-based.
	maxOOXMLEntries      = 500
	maxOOXMLUncompressed = 200 << 20 // 200 MB
	maxOOXMLRatio        = 100       // suspicious if uncompressed/compressed > 100:1

	// CSV safety limit — plain text, no zip-bomb risk, so we cap raw bytes only.
	// Extracted-text size is enforced separately by ValidateContent (maxTextSize).
	maxCSVSize = 50 << 20 // 50 MB
)

// validateCSVSize rejects CSV inputs above the raw-byte cap before parsing.
func validateCSVSize(data []byte) error {
	if len(data) > maxCSVSize {
		return fmt.Errorf("csv size exceeds limit: %d > %d", len(data), maxCSVSize)
	}
	return nil
}

// validateOOXMLArchive opens data as a ZIP archive and rejects zip-bombs / path
// traversal attempts BEFORE any parser walks the entries.
//
// Called from parseDOCX, parseXLSX, parsePPTX before doing any real work.
func validateOOXMLArchive(data []byte) error {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("not a valid OOXML (ZIP) archive: %w", err)
	}
	if len(r.File) > maxOOXMLEntries {
		return fmt.Errorf("archive entries exceed limit: %d > %d", len(r.File), maxOOXMLEntries)
	}
	var totalUncompressed uint64
	compressedLen := uint64(len(data))
	for _, f := range r.File {
		// Path traversal guard — defence-in-depth even though stdlib zip is mostly safe.
		if strings.Contains(f.Name, "..") || strings.HasPrefix(f.Name, "/") || strings.HasPrefix(f.Name, `\`) {
			return fmt.Errorf("archive contains suspicious path: %q", f.Name)
		}
		totalUncompressed += f.UncompressedSize64
		if totalUncompressed > maxOOXMLUncompressed {
			return fmt.Errorf("archive uncompressed size exceeds limit: %d > %d", totalUncompressed, maxOOXMLUncompressed)
		}
	}
	if compressedLen > 0 && totalUncompressed/compressedLen > maxOOXMLRatio {
		return fmt.Errorf("archive compression ratio suspicious: %d:1", totalUncompressed/compressedLen)
	}
	return nil
}

// ValidateContent checks that extracted text is worth embedding: valid UTF-8,
// within size/word bounds, and not mostly garbage (control bytes / mojibake).
// Useful as a quality gate before chunking + embedding crawled articles.
func ValidateContent(text string) error {
	if len(text) > maxTextSize {
		return fmt.Errorf("extracted text exceeds 10 MB")
	}
	if !utf8.ValidString(text) {
		return fmt.Errorf("extracted text is not valid UTF-8")
	}

	words := strings.Fields(text)
	if len(words) == 0 {
		return fmt.Errorf("extracted text is empty")
	}
	if len(words) < minWords {
		return fmt.Errorf("text too short: %d words (minimum %d)", len(words), minWords)
	}
	if len(words) > maxWords {
		return fmt.Errorf("text too long: %d words (maximum %d)", len(words), maxWords)
	}

	readable := countReadable(text)
	ratio := float64(readable) / float64(len([]rune(text)))
	if ratio < minReadable {
		return fmt.Errorf("text quality too low: %.0f%% readable characters (minimum 50%%)", ratio*100)
	}
	return nil
}

// countReadable counts printable Unicode runes (letters, digits, punctuation, space).
func countReadable(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsPrint(r) || unicode.IsSpace(r) {
			n++
		}
	}
	return n
}
