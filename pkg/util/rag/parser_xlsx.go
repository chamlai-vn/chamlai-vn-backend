package ragutil

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// parseXLSX extracts text from all visible sheets of an XLSX workbook.
// Hidden sheets are skipped (presumed intentional drafts).
//
// Format:
//
//	## Sheet: <name>
//	cell1\tcell2\tcell3
//	...
//
// Uses excelize's default GetRows which returns evaluated cell values (not raw
// formulas).
func parseXLSX(data []byte) (string, error) {
	if err := validateOOXMLArchive(data); err != nil {
		return "", err
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	var buf strings.Builder
	for _, sheet := range f.GetSheetList() {
		visible, err := f.GetSheetVisible(sheet)
		if err != nil {
			// Treat probe errors as non-fatal — fall through and try to read.
			visible = true
		}
		if !visible {
			continue
		}

		rows, err := f.GetRows(sheet)
		if err != nil {
			return "", fmt.Errorf("get rows of sheet %q: %w", sheet, err)
		}
		if len(rows) == 0 {
			continue
		}

		nonEmpty := false
		var sheetBuf strings.Builder
		for _, row := range rows {
			row = trimTrailingEmpty(row)
			if len(row) == 0 {
				continue
			}
			nonEmpty = true
			sheetBuf.WriteString(strings.Join(row, "\t"))
			sheetBuf.WriteByte('\n')
		}
		if !nonEmpty {
			continue
		}

		fmt.Fprintf(&buf, "## Sheet: %s\n", sheet)
		buf.WriteString(sheetBuf.String())
		buf.WriteByte('\n')
	}
	return buf.String(), nil
}

// trimTrailingEmpty drops trailing empty/whitespace-only cells from a row.
// Excelize pads short rows with "" up to the workbook's max column — without
// trimming, we'd serialize hundreds of leading tab characters per row.
func trimTrailingEmpty(row []string) []string {
	end := len(row)
	for end > 0 && strings.TrimSpace(row[end-1]) == "" {
		end--
	}
	return row[:end]
}
