package ragutil

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// newDOCX builds a minimal valid DOCX (ZIP with word/document.xml) carrying the
// given document.xml body so the parser exercises real ZIP+XML extraction.
func newDOCX(t *testing.T, documentXML string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatalf("create document.xml: %v", err)
	}
	if _, err := w.Write([]byte(documentXML)); err != nil {
		t.Fatalf("write document.xml: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

// wordPara wraps text in a <w:p><w:r><w:t> paragraph.
func wordPara(text string) string {
	return `<w:p><w:r><w:t>` + text + `</w:t></w:r></w:p>`
}

// wordCell wraps text in a table cell paragraph.
func wordCell(text string) string {
	return `<w:tc>` + wordPara(text) + `</w:tc>`
}

// wordRow wraps cells in a table row.
func wordRow(cells ...string) string {
	var sb strings.Builder
	sb.WriteString(`<w:tr>`)
	for _, c := range cells {
		sb.WriteString(wordCell(c))
	}
	sb.WriteString(`</w:tr>`)
	return sb.String()
}

func docWrap(body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body + `</w:body></w:document>`
}

func TestParseDOCX_TableRowsJoinedWithPipes(t *testing.T) {
	body := wordPara("Economic difficulty") +
		`<w:tbl>` +
		wordRow("Beginner", "100%", "100%") +
		wordRow("Expert", "90%", "90%") +
		`</w:tbl>`
	data := newDOCX(t, docWrap(body))

	got, err := parseDOCX(data)
	if err != nil {
		t.Fatalf("parseDOCX: %v", err)
	}

	if !strings.Contains(got, "Beginner | 100% | 100%") {
		t.Errorf("table row not joined with pipes:\n%q", got)
	}
	if !strings.Contains(got, "Expert | 90% | 90%") {
		t.Errorf("second table row not joined with pipes:\n%q", got)
	}
	// Each row must stay on its own line.
	if strings.Contains(got, "100% Expert") {
		t.Errorf("rows bled together (missing row break):\n%q", got)
	}
}

func TestParseDOCX_ParagraphTextPreserved(t *testing.T) {
	body := wordPara("How do I make money?") + wordPara("Take contracts.")
	data := newDOCX(t, docWrap(body))

	got, err := parseDOCX(data)
	if err != nil {
		t.Fatalf("parseDOCX: %v", err)
	}
	if !strings.Contains(got, "How do I make money?") {
		t.Errorf("paragraph text missing:\n%q", got)
	}
	if !strings.Contains(got, "Take contracts.") {
		t.Errorf("second paragraph missing:\n%q", got)
	}
}

func TestParseDOCX_CollapsesEmptyTrailingCells(t *testing.T) {
	// A row whose last cell is empty must not leave a dangling " | " separator.
	body := `<w:tbl>` + wordRow("Veteran", "100%", "") + `</w:tbl>`
	data := newDOCX(t, docWrap(body))

	got, err := parseDOCX(data)
	if err != nil {
		t.Fatalf("parseDOCX: %v", err)
	}
	for _, line := range strings.Split(got, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasSuffix(line, "|") {
			t.Errorf("line ends with dangling cell separator: %q", line)
		}
	}
	if !strings.Contains(got, "Veteran | 100%") {
		t.Errorf("expected cleaned row:\n%q", got)
	}
}
