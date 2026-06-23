package ragutil

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// parsePPTX extracts text from slides + speaker notes of a PPTX presentation.
// Uses stdlib archive/zip + encoding/xml (no third-party lib).
//
// Format:
//
//	## Slide N
//	<body text>
//	_Notes:_ <speaker notes>
//
// Slide order is derived from the numeric suffix in filename
// (ppt/slides/slide1.xml, slide2.xml, ...). This is a pragmatic ordering —
// the canonical source is ppt/_rels/presentation.xml.rels, but PPTX files
// generally order filenames consistently with the rels file.
func parsePPTX(data []byte) (string, error) {
	if err := validateOOXMLArchive(data); err != nil {
		return "", err
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("open pptx: %w", err)
	}

	slides := extractOrderedSlideFiles(r, "ppt/slides/slide")
	notes := mapNotesBySlideIndex(r, "ppt/notesSlides/notesSlide")

	var buf strings.Builder
	for _, sf := range slides {
		text, err := readDrawingMLFromZip(sf.file)
		if err != nil {
			return "", fmt.Errorf("read slide %d: %w", sf.index, err)
		}
		text = strings.TrimSpace(text)
		notesText := strings.TrimSpace(notes[sf.index])
		if text == "" && notesText == "" {
			continue
		}
		fmt.Fprintf(&buf, "## Slide %d\n", sf.index)
		if text != "" {
			buf.WriteString(text)
			buf.WriteByte('\n')
		}
		if notesText != "" {
			fmt.Fprintf(&buf, "_Notes:_ %s\n", notesText)
		}
		buf.WriteByte('\n')
	}
	return buf.String(), nil
}

// slideFile pairs a zip entry with its parsed slide index.
type slideFile struct {
	index int
	file  *zip.File
}

// slideIndexRe matches the digit suffix in "slide123.xml" or "notesSlide123.xml".
// Case-insensitive because PPTX filenames use both "slide" (lower) and
// "notesSlide" (mixed case).
var slideIndexRe = regexp.MustCompile(`(?i)slide(\d+)\.xml$`)

// extractOrderedSlideFiles finds all ppt/slides/slideN.xml entries and returns
// them sorted by N (ascending).
func extractOrderedSlideFiles(r *zip.Reader, prefix string) []slideFile {
	var out []slideFile
	for _, f := range r.File {
		if !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		idx, ok := parseSlideIndex(f.Name)
		if !ok {
			continue
		}
		out = append(out, slideFile{index: idx, file: f})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].index < out[j].index })
	return out
}

// mapNotesBySlideIndex returns slideIndex → notes text for every notesSlideN.xml
// present in the archive.
func mapNotesBySlideIndex(r *zip.Reader, prefix string) map[int]string {
	out := make(map[int]string)
	for _, f := range r.File {
		if !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		idx, ok := parseSlideIndex(f.Name)
		if !ok {
			continue
		}
		text, err := readDrawingMLFromZip(f)
		if err != nil {
			continue
		}
		out[idx] = text
	}
	return out
}

func parseSlideIndex(name string) (int, bool) {
	m := slideIndexRe.FindStringSubmatch(name)
	if len(m) < 2 {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// readDrawingMLFromZip opens a zip entry and walks its DrawingML XML, collecting
// <a:t> text nodes. Paragraphs (</a:p>) become newlines.
func readDrawingMLFromZip(f *zip.File) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", fmt.Errorf("open %s: %w", f.Name, err)
	}
	defer rc.Close()
	return extractDrawingMLText(rc)
}

// extractDrawingMLText walks XML tokens and collects DrawingML text runs.
// DrawingML uses <a:t> for text content (vs WordML's <w:t>).
// A paragraph break (</a:p>) is emitted as "\n"; a run boundary (</a:t>) as " "
// — same convention as the DOCX parser.
func extractDrawingMLText(r io.Reader) (string, error) {
	var sb strings.Builder
	dec := xml.NewDecoder(r)
	inText := false
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
			if t.Name.Local == "t" {
				inText = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
				sb.WriteByte(' ')
			case "p":
				sb.WriteByte('\n')
			}
		case xml.CharData:
			if inText {
				sb.Write(t)
			}
		}
	}
	return sb.String(), nil
}
