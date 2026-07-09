package corpusdoc

import (
	"strings"
	"testing"
)

const validDoc = `# General information
url: https://congan.example.gov.vn/canh-bao-lua-dao
title: mạo danh cơ sở giáo dục
type: impersonation_authority
# Content
Đối tượng giả danh nhân viên nhà trường liên hệ phụ huynh.

Yêu cầu chuyển khoản đặt cọc gấp để giữ suất học bổng.
# User query
1. Có ai gọi báo con tôi trúng học bổng và yêu cầu chuyển tiền, có phải lừa đảo không?
2. Nhận tin nhắn mạo danh trường học, phải làm sao?
# Prevention
Xác minh trực tiếp với nhà trường trước khi chuyển bất kỳ khoản tiền nào.
`

func TestParse_ValidDocument(t *testing.T) {
	doc, err := Parse(validDoc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.URL != "https://congan.example.gov.vn/canh-bao-lua-dao" {
		t.Errorf("URL = %q", doc.URL)
	}
	if doc.Title != "mạo danh cơ sở giáo dục" {
		t.Errorf("Title = %q", doc.Title)
	}
	if doc.ScamType != "impersonation_authority" {
		t.Errorf("ScamType = %q", doc.ScamType)
	}
	if !strings.Contains(doc.Content, "giả danh nhân viên") {
		t.Errorf("Content missing expected text: %q", doc.Content)
	}
	if len(doc.UserQueries) != 2 {
		t.Fatalf("UserQueries = %d, want 2: %#v", len(doc.UserQueries), doc.UserQueries)
	}
	if strings.HasPrefix(doc.UserQueries[0], "1.") || strings.HasPrefix(doc.UserQueries[0], "1)") {
		t.Errorf("UserQueries[0] still has numbering: %q", doc.UserQueries[0])
	}
	if doc.Prevention == "" {
		t.Error("Prevention is empty")
	}
}

func TestParse_SectionsInAnyOrder(t *testing.T) {
	reordered := `# Prevention
Đừng chuyển tiền cho người lạ.
# Content
Nội dung cảnh báo.
# User query
1. Có phải lừa đảo không?
# General information
url: https://example.gov.vn/x
title: test
type: other
`
	doc, err := Parse(reordered)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.Prevention != "Đừng chuyển tiền cho người lạ." {
		t.Errorf("Prevention = %q", doc.Prevention)
	}
	if doc.ScamType != "other" {
		t.Errorf("ScamType = %q", doc.ScamType)
	}
}

func TestParse_RoundTrip(t *testing.T) {
	doc, err := Parse(validDoc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	reparsed, err := Parse(Serialize(doc))
	if err != nil {
		t.Fatalf("Parse(Serialize(doc)): %v", err)
	}

	if reparsed.URL != doc.URL || reparsed.Title != doc.Title || reparsed.ScamType != doc.ScamType {
		t.Errorf("round-trip mismatch: got %+v, want %+v", reparsed, doc)
	}
	if reparsed.Content != doc.Content {
		t.Errorf("Content round-trip mismatch:\ngot:  %q\nwant: %q", reparsed.Content, doc.Content)
	}
	if len(reparsed.UserQueries) != len(doc.UserQueries) {
		t.Fatalf("UserQueries round-trip: got %d, want %d", len(reparsed.UserQueries), len(doc.UserQueries))
	}
	for i := range doc.UserQueries {
		if reparsed.UserQueries[i] != doc.UserQueries[i] {
			t.Errorf("UserQueries[%d]: got %q, want %q", i, reparsed.UserQueries[i], doc.UserQueries[i])
		}
	}
	if reparsed.Prevention != doc.Prevention {
		t.Errorf("Prevention round-trip mismatch: got %q, want %q", reparsed.Prevention, doc.Prevention)
	}
}

func TestParse_AdversarialInput(t *testing.T) {
	cases := map[string]string{
		"empty input":                 "",
		"no section headers":          "just some random text\nwith no headers at all",
		"content before first header": "some stray text\n# General information\nurl: https://x.gov.vn\ntitle: t\ntype: other\n# Content\nc\n",
		"unknown section header":      "# General information\nurl: https://x.gov.vn\ntitle: t\ntype: other\n# Bogus Section\nx\n# Content\nc\n",
		"missing General information": "# Content\nsome content\n",
		"missing Content":             "# General information\nurl: https://x.gov.vn\ntitle: t\ntype: other\n",
		"empty Content":               "# General information\nurl: https://x.gov.vn\ntitle: t\ntype: other\n# Content\n   \n",
		"missing url":                 "# General information\ntitle: t\ntype: other\n# Content\nc\n",
		"missing title":               "# General information\nurl: https://x.gov.vn\ntype: other\n# Content\nc\n",
		"missing type":                "# General information\nurl: https://x.gov.vn\ntitle: t\n# Content\nc\n",
		"unknown scam type":           "# General information\nurl: https://x.gov.vn\ntitle: t\ntype: not_a_real_type\n# Content\nc\n",
		"javascript url scheme":       "# General information\nurl: javascript:alert(1)\ntitle: t\ntype: other\n# Content\nc\n",
		"file url scheme":             "# General information\nurl: file:///etc/passwd\ntitle: t\ntype: other\n# Content\nc\n",
		"malformed url":               "# General information\nurl: ://not a url\ntitle: t\ntype: other\n# Content\nc\n",
	}

	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Parse panicked: %v", r)
				}
			}()
			if _, err := Parse(input); err == nil {
				t.Errorf("Parse(%q): expected error, got nil", name)
			}
		})
	}
}

func TestParse_ToleratesFrontmatterStyleColonsInURL(t *testing.T) {
	doc, err := Parse(validDoc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !strings.HasPrefix(doc.URL, "https://") {
		t.Errorf("URL should keep its scheme colon intact, got %q", doc.URL)
	}
}

func TestReadReviewedFlag(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"reviewed true", "# General information\nurl: https://x.gov.vn\ntitle: t\ntype: other\nreviewed: true\n# Content\nc\n", true},
		{"reviewed false", "# General information\nurl: https://x.gov.vn\ntitle: t\ntype: other\nreviewed: false\n# Content\nc\n", false},
		{"reviewed key absent (fail-closed)", "# General information\nurl: https://x.gov.vn\ntitle: t\ntype: other\n# Content\nc\n", false},
		{"reviewed set to garbage (fail-closed)", "# General information\nurl: https://x.gov.vn\ntitle: t\ntype: other\nreviewed: yes\n# Content\nc\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ReadReviewedFlag(tc.text)
			if err != nil {
				t.Fatalf("ReadReviewedFlag: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReadReviewedFlag_MissingGeneralInformationErrors(t *testing.T) {
	if _, err := ReadReviewedFlag("# Content\nc\n"); err == nil {
		t.Error("expected error for missing General information section, got nil")
	}
}

func TestSlug_PrefersURLPathSegment(t *testing.T) {
	doc := Document{
		URL:   "https://congan.example.gov.vn/canh-bao-mao-danh-giao-duc-1234.html",
		Title: "mạo danh cơ sở giáo dục",
	}
	got := Slug(doc)
	if !strings.HasPrefix(got, "canh-bao-mao-danh-giao-duc-1234-") {
		t.Errorf("Slug = %q, want prefix from the URL path segment", got)
	}
}

func TestSlug_FallsBackToTitleWhenURLHasNoPath(t *testing.T) {
	doc := Document{URL: "https://example.gov.vn", Title: "canh bao lua dao"}
	got := Slug(doc)
	if !strings.HasPrefix(got, "canh-bao-lua-dao-") {
		t.Errorf("Slug = %q, want prefix derived from title", got)
	}
}

func TestSlug_IsStableAndUniquePerURL(t *testing.T) {
	docA := Document{URL: "https://example.gov.vn/same-path.html", Title: "t"}
	docB := Document{URL: "https://example.gov.vn/same-path.html?id=2", Title: "t"}

	first, second := Slug(docA), Slug(docA)
	if first != second {
		t.Errorf("Slug is not stable across calls for the same document: %q != %q", first, second)
	}
	if Slug(docA) == Slug(docB) {
		t.Error("two different URLs with the same path segment should not collide (hash suffix should differ)")
	}
}

func TestSlug_NeverEmpty(t *testing.T) {
	doc := Document{URL: "https://example.gov.vn/", Title: ""}
	if got := Slug(doc); got == "" || strings.HasPrefix(got, "-") {
		t.Errorf("Slug = %q, want a non-empty, non-dash-prefixed fallback", got)
	}
}

func TestSerializeForReview_MarksUnreviewedAndIngestRefusesUntilFlipped(t *testing.T) {
	doc, err := Parse(validDoc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	generated := SerializeForReview(doc)
	reviewed, err := ReadReviewedFlag(generated)
	if err != nil {
		t.Fatalf("ReadReviewedFlag: %v", err)
	}
	if reviewed {
		t.Error("SerializeForReview output should start as reviewed: false")
	}
	// The document itself must still parse cleanly even with the extra
	// reviewed: line mixed into General information's key:value lines.
	if _, err := Parse(generated); err != nil {
		t.Errorf("Parse(SerializeForReview(doc)): %v", err)
	}

	reviewedFile := strings.Replace(generated, "reviewed: false", "reviewed: true", 1)
	reviewed, err = ReadReviewedFlag(reviewedFile)
	if err != nil {
		t.Fatalf("ReadReviewedFlag: %v", err)
	}
	if !reviewed {
		t.Error("flipping reviewed: false -> true should be picked up")
	}
}
