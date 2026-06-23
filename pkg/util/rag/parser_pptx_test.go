package ragutil

import (
	"strings"
	"testing"
)

func TestParsePPTX_SlidesAndNotes(t *testing.T) {
	const slide1XML = `<?xml version="1.0"?>
<p:sld xmlns:p="x" xmlns:a="y">
  <p:txBody>
    <a:p><a:r><a:t>Welcome to ChậmLại</a:t></a:r></a:p>
    <a:p><a:r><a:t>Onboarding deck</a:t></a:r></a:p>
  </p:txBody>
</p:sld>`
	const slide2XML = `<?xml version="1.0"?>
<p:sld xmlns:p="x" xmlns:a="y">
  <p:txBody><a:p><a:r><a:t>First day checklist</a:t></a:r></a:p></p:txBody>
</p:sld>`
	const notes2XML = `<?xml version="1.0"?>
<p:notes xmlns:p="x" xmlns:a="y">
  <p:txBody><a:p><a:r><a:t>Remind HR to send welcome email</a:t></a:r></a:p></p:txBody>
</p:notes>`

	data := buildZip(t, map[string]string{
		"ppt/slides/slide1.xml":           slide1XML,
		"ppt/slides/slide2.xml":           slide2XML,
		"ppt/notesSlides/notesSlide2.xml": notes2XML,
	}, false)

	got, err := parsePPTX(data)
	if err != nil {
		t.Fatalf("parsePPTX: %v", err)
	}

	if !strings.Contains(got, "## Slide 1") {
		t.Errorf("expected Slide 1 header in output:\n%s", got)
	}
	if !strings.Contains(got, "Welcome to ChậmLại") {
		t.Errorf("expected slide 1 body text:\n%s", got)
	}
	if !strings.Contains(got, "## Slide 2") {
		t.Errorf("expected Slide 2 header:\n%s", got)
	}
	if !strings.Contains(got, "First day checklist") {
		t.Errorf("expected slide 2 body:\n%s", got)
	}
	if !strings.Contains(got, "_Notes:_ Remind HR to send welcome email") {
		t.Errorf("expected speaker notes prefix:\n%s", got)
	}

	// Order: Slide 1 must appear before Slide 2.
	if strings.Index(got, "## Slide 1") > strings.Index(got, "## Slide 2") {
		t.Errorf("slides out of order:\n%s", got)
	}
}

func TestParsePPTX_SkipsEmptySlide(t *testing.T) {
	const empty = `<?xml version="1.0"?><p:sld xmlns:p="x" xmlns:a="y"/>`
	const populated = `<?xml version="1.0"?>
<p:sld xmlns:p="x" xmlns:a="y"><p:txBody><a:p><a:r><a:t>Real content</a:t></a:r></a:p></p:txBody></p:sld>`

	data := buildZip(t, map[string]string{
		"ppt/slides/slide1.xml": empty,
		"ppt/slides/slide2.xml": populated,
	}, false)

	got, err := parsePPTX(data)
	if err != nil {
		t.Fatalf("parsePPTX: %v", err)
	}
	if strings.Contains(got, "## Slide 1") {
		t.Errorf("empty slide should be skipped, got:\n%s", got)
	}
	if !strings.Contains(got, "## Slide 2") {
		t.Errorf("non-empty slide missing:\n%s", got)
	}
}

func TestParseSlideIndex(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
		ok   bool
	}{
		{"slide", "ppt/slides/slide7.xml", 7, true},
		{"notes", "ppt/notesSlides/notesSlide42.xml", 42, true},
		{"non-numeric", "ppt/slides/slidesMaster.xml", 0, false},
		{"no match", "docProps/app.xml", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, ok := parseSlideIndex(tc.in)
			if ok != tc.ok || n != tc.want {
				t.Errorf("parseSlideIndex(%q) = (%d, %v), want (%d, %v)", tc.in, n, ok, tc.want, tc.ok)
			}
		})
	}
}
