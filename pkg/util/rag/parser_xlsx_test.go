package ragutil

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func newWorkbook(t *testing.T, build func(f *excelize.File)) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	build(f)
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write workbook: %v", err)
	}
	return buf.Bytes()
}

func TestParseXLSX_HeaderAndTabSeparated(t *testing.T) {
	data := newWorkbook(t, func(f *excelize.File) {
		// Rename default sheet for predictability.
		_ = f.SetSheetName("Sheet1", "Salary")
		_ = f.SetCellValue("Salary", "A1", "Level")
		_ = f.SetCellValue("Salary", "B1", "Min")
		_ = f.SetCellValue("Salary", "C1", "Max")
		_ = f.SetCellValue("Salary", "A2", "Senior")
		_ = f.SetCellValue("Salary", "B2", 2000)
		_ = f.SetCellValue("Salary", "C2", 4000)
	})

	got, err := parseXLSX(data)
	if err != nil {
		t.Fatalf("parseXLSX: %v", err)
	}
	if !strings.Contains(got, "## Sheet: Salary") {
		t.Errorf("missing sheet header:\n%s", got)
	}
	if !strings.Contains(got, "Level\tMin\tMax") {
		t.Errorf("expected tab-separated header row:\n%s", got)
	}
	if !strings.Contains(got, "Senior\t2000\t4000") {
		t.Errorf("expected tab-separated data row:\n%s", got)
	}
}

func TestParseXLSX_SkipsHiddenSheet(t *testing.T) {
	data := newWorkbook(t, func(f *excelize.File) {
		_ = f.SetSheetName("Sheet1", "Visible")
		_ = f.SetCellValue("Visible", "A1", "ok")

		_, _ = f.NewSheet("Hidden")
		_ = f.SetCellValue("Hidden", "A1", "should not appear")
		_ = f.SetSheetVisible("Hidden", false)
	})

	got, err := parseXLSX(data)
	if err != nil {
		t.Fatalf("parseXLSX: %v", err)
	}
	if !strings.Contains(got, "## Sheet: Visible") {
		t.Errorf("missing visible sheet:\n%s", got)
	}
	if strings.Contains(got, "Hidden") || strings.Contains(got, "should not appear") {
		t.Errorf("hidden sheet content leaked:\n%s", got)
	}
}

func TestTrimTrailingEmpty(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"none", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"all empty", []string{"", "", ""}, []string{}},
		{"trailing", []string{"a", "b", "", "  "}, []string{"a", "b"}},
		{"empty slice", []string{}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := trimTrailingEmpty(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d (got=%v)", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("idx %d: got %q want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}
