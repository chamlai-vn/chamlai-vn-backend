package ragutil

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// parseCSV extracts text from a CSV file. Rows are joined by tabs and
// separated by newlines, mirroring parseXLSX. CSV has a single implicit
// "sheet" so no `## Sheet:` header is emitted.
//
// Scope: UTF-8 only, comma delimiter only. UTF-8 BOM is stripped.
// LazyQuotes tolerates Excel-exported irregularities; rows may have a
// variable column count. TSV / semicolon / non-UTF-8 encodings are not
// supported and will surface as either parse errors or downstream
// ValidateContent failures.
func parseCSV(data []byte) (string, error) {
	if err := validateCSVSize(data); err != nil {
		return "", err
	}
	data = bytes.TrimPrefix(data, utf8BOM)

	r := csv.NewReader(bytes.NewReader(data))
	r.LazyQuotes = true
	r.FieldsPerRecord = -1

	var buf strings.Builder
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("parse csv: %w", err)
		}
		row = trimTrailingEmpty(row)
		if len(row) == 0 {
			continue
		}
		buf.WriteString(strings.Join(row, "\t"))
		buf.WriteByte('\n')
	}
	return buf.String(), nil
}
