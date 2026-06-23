package ragutil

import (
	"strings"
	"testing"
)

func TestParseCSV_HeaderAndRows(t *testing.T) {
	in := []byte("name,age\nAlice,30\nBob,25\n")
	got, err := parseCSV(in)
	if err != nil {
		t.Fatalf("parseCSV: %v", err)
	}
	if !strings.Contains(got, "name\tage") {
		t.Errorf("missing tab-separated header:\n%s", got)
	}
	if !strings.Contains(got, "Alice\t30") {
		t.Errorf("missing first data row:\n%s", got)
	}
	if !strings.Contains(got, "Bob\t25") {
		t.Errorf("missing second data row:\n%s", got)
	}
}

func TestParseCSV_StripsBOM(t *testing.T) {
	in := append([]byte{0xEF, 0xBB, 0xBF}, []byte("name,age\nAlice,30\n")...)
	got, err := parseCSV(in)
	if err != nil {
		t.Fatalf("parseCSV: %v", err)
	}
	if strings.ContainsRune(got, '\uFEFF') {
		t.Errorf("BOM leaked into output:\n%q", got)
	}
	if !strings.HasPrefix(got, "name\tage") {
		t.Errorf("expected header to start output after BOM strip:\n%q", got)
	}
}

func TestParseCSV_QuotedFieldWithComma(t *testing.T) {
	in := []byte("name,role\n\"Doe, John\",HR\n")
	got, err := parseCSV(in)
	if err != nil {
		t.Fatalf("parseCSV: %v", err)
	}
	if !strings.Contains(got, "Doe, John\tHR") {
		t.Errorf("quoted field with comma not preserved:\n%s", got)
	}
}

func TestParseCSV_SkipsEmptyRows(t *testing.T) {
	in := []byte("a,b\n\n,\nx,y\n")
	got, err := parseCSV(in)
	if err != nil {
		t.Fatalf("parseCSV: %v", err)
	}
	// Two non-empty rows expected: header + (x,y). The ",," row trims to empty.
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 non-empty lines, got %d:\n%s", len(lines), got)
	}
	if lines[0] != "a\tb" || lines[1] != "x\ty" {
		t.Errorf("unexpected content:\n%s", got)
	}
}

func TestParseCSV_LazyQuotesToleratesIrregular(t *testing.T) {
	// Stray quote in the middle of a field — strict mode would error, lazy
	// mode passes it through verbatim.
	in := []byte("a,b\nfoo\"bar,baz\n")
	got, err := parseCSV(in)
	if err != nil {
		t.Fatalf("parseCSV: %v", err)
	}
	if !strings.Contains(got, "foo\"bar\tbaz") {
		t.Errorf("lazy-quotes row not parsed:\n%s", got)
	}
}

func TestParseCSV_RejectsOversize(t *testing.T) {
	big := make([]byte, maxCSVSize+1)
	for i := range big {
		big[i] = 'a'
	}
	_, err := parseCSV(big)
	if err == nil {
		t.Fatalf("expected error for oversize input")
	}
	if !strings.Contains(err.Error(), "exceeds limit") {
		t.Errorf("error should mention size cap, got: %v", err)
	}
}
