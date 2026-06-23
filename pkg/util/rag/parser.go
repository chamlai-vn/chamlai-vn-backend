package ragutil

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"
)

// Supported MIME types for ParseText. Kept here (rather than imported from a
// project enum) so this package stays self-contained and copy-pasteable into
// other projects.
const (
	ContentTypePlain = "text/plain"
	ContentTypePDF   = "application/pdf"
	ContentTypeDOCX  = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	ContentTypeXLSX  = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	ContentTypePPTX  = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	ContentTypeCSV   = "text/csv"
)

// ParseText extracts plain text from the given bytes based on MIME type.
// Wraps parsers in recover() to handle panicking third-party libraries gracefully.
func ParseText(mimeType string, data []byte) (text string, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("parser panic: %v", r)
		}
	}()

	switch mimeType {
	case ContentTypePlain:
		return string(data), nil
	case ContentTypePDF:
		return parsePDF(data)
	case ContentTypeDOCX:
		return parseDOCX(data)
	case ContentTypeXLSX:
		return parseXLSX(data)
	case ContentTypePPTX:
		return parsePPTX(data)
	case ContentTypeCSV:
		return parseCSV(data)
	default:
		return "", fmt.Errorf("unsupported MIME type: %s", mimeType)
	}
}

// parsePDF extracts plain text from a PDF using github.com/ledongthuc/pdf.
// Iterates all pages, concatenates extracted text with newlines, and skips
// null pages. Returns an error if the assembled text is too short — likely
// a scanned/image-based PDF that needs OCR.
func parsePDF(data []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("open pdf: %w", err)
	}
	var sb strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		text, err := p.GetPlainText(nil)
		if err != nil {
			return "", fmt.Errorf("extract page %d: %w", i, err)
		}
		sb.WriteString(text)
		sb.WriteByte('\n')
	}
	out := sb.String()
	if len(strings.Fields(out)) < 5 {
		return "", fmt.Errorf("PDF text extraction yielded too little text; file may be scanned/image-based")
	}
	return out, nil
}

// parseDOCX extracts text from a DOCX file using stdlib zip + xml.
// DOCX = ZIP archive containing word/document.xml (no third-party deps).
func parseDOCX(data []byte) (string, error) {
	if err := validateOOXMLArchive(data); err != nil {
		return "", err
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("open DOCX as ZIP: %w", err)
	}

	var docXML io.ReadCloser
	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			docXML, err = f.Open()
			if err != nil {
				return "", fmt.Errorf("open word/document.xml: %w", err)
			}
			break
		}
	}
	if docXML == nil {
		return "", fmt.Errorf("word/document.xml not found in DOCX")
	}
	defer docXML.Close()

	return extractWordXMLText(docXML)
}

// extractWordXMLText parses word/document.xml and collects <w:t> text nodes.
//
// Tables are handled explicitly: cells in a row are joined with " | " and each
// row ends with a newline, so a table row renders as "Label | 100% | 100%"
// instead of being shredded into one bare line per cell. This preserves the
// label↔value association that embeddings and the grounding LLM rely on.
// Outside tables, each paragraph (<w:p>) starts a new line.
func extractWordXMLText(r io.Reader) (string, error) {
	var sb strings.Builder
	dec := xml.NewDecoder(r)
	inText := false
	tableDepth := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("parse XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tbl":
				tableDepth++
				sb.WriteByte('\n')
			case "p":
				// Inside a table, row/cell boundaries drive layout — paragraph
				// breaks within a cell must not split the row across lines.
				if tableDepth == 0 {
					sb.WriteByte('\n')
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
				sb.WriteByte(' ')
			case "tc":
				if tableDepth > 0 {
					sb.WriteString(" | ")
				}
			case "tr":
				if tableDepth > 0 {
					sb.WriteByte('\n')
				}
			case "tbl":
				tableDepth--
				sb.WriteByte('\n')
			}
		case xml.CharData:
			if inText {
				sb.Write(t)
			}
		}
	}
	return tidyTableText(sb.String()), nil
}

// tidyTableText normalises the artifacts left by extraction. For table rows
// (lines containing the " | " cell separator) it trims each cell and drops
// trailing empty cells so a row reads "Veteran | 100% | 100%" with no dangling
// separator. For every line it collapses internal whitespace, and finally it
// squeezes runs of blank lines.
func tidyTableText(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Contains(line, "|") {
			line = tidyTableRow(line)
		} else {
			line = strings.TrimSpace(reSpaces.ReplaceAllString(line, " "))
		}
		out = append(out, line)
	}
	joined := strings.Join(out, "\n")
	return reBlankLines.ReplaceAllString(joined, "\n\n")
}

// tidyTableRow trims each pipe-delimited cell and removes trailing empty cells.
func tidyTableRow(line string) string {
	cells := strings.Split(line, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(reSpaces.ReplaceAllString(cells[i], " "))
	}
	// Drop trailing empty cells (e.g. an empty last column).
	for len(cells) > 0 && cells[len(cells)-1] == "" {
		cells = cells[:len(cells)-1]
	}
	return strings.Join(cells, " | ")
}

var (
	reSpaces     = regexp.MustCompile(`[ \t]+`)
	reBlankLines = regexp.MustCompile(`\n{3,}`)
)
