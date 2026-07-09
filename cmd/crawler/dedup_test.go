package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/infra/store"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/model"
)

func TestParseChunkIDs(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []int64
		wantErr bool
	}{
		{name: "simple", in: "41,55,88", want: []int64{41, 55, 88}},
		{name: "trims and skips blanks", in: " 41 , ,55,\t88 ", want: []int64{41, 55, 88}},
		{name: "dedupes first-seen order", in: "88,41,88,55,41", want: []int64{88, 41, 55}},
		{name: "empty string", in: "", want: nil},
		{name: "only separators", in: " , , ", want: nil},
		{name: "non-numeric errors", in: "41,abc,55", wantErr: true},
		{name: "float errors", in: "41.5", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseChunkIDs(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRenderAuditEmpty(t *testing.T) {
	var b strings.Builder
	renderAudit(&b, nil, 0.95)
	out := b.String()
	if !strings.Contains(out, "no duplicate pairs found") {
		t.Fatalf("empty report missing sentinel: %q", out)
	}
	if !strings.Contains(out, "0.950") {
		t.Fatalf("empty report missing threshold: %q", out)
	}
}

func TestRenderAuditGroupsAndFooter(t *testing.T) {
	pairs := []store.DuplicatePair{
		{
			ScamType: "thu-hoi-von", Kind: model.ChunkKindContent, Similarity: 0.983,
			AID: 41, ADocID: 12, AURL: "https://a", APreview: "Kẻ gian\nmạo danh",
			BID: 88, BDocID: 27, BURL: "https://b", BPreview: "Đối tượng giả danh",
		},
		{
			ScamType: "thu-hoi-von", Kind: model.ChunkKindQuery, Similarity: 0.961,
			AID: 55, ADocID: 12, AURL: "https://a", APreview: "làm sao lấy lại tiền",
			BID: 91, BDocID: 27, BURL: "https://b", BPreview: "cách lấy lại tiền",
		},
		{
			ScamType: "vay-tien", Kind: model.ChunkKindContent, Similarity: 0.972,
			AID: 5, ADocID: 3, AURL: "https://c", APreview: "vay nhanh",
			BID: 9, BDocID: 4, BURL: "https://d", BPreview: "vay siêu tốc",
		},
	}
	var b strings.Builder
	renderAudit(&b, pairs, 0.95)
	out := b.String()

	if !strings.Contains(out, "[thu-hoi-von]  (2 pairs)") {
		t.Errorf("missing thu-hoi-von group header: %q", out)
	}
	if !strings.Contains(out, "[vay-tien]  (1 pair)") {
		t.Errorf("missing vay-tien singular header: %q", out)
	}
	// Preview newline collapsed to a single line.
	if !strings.Contains(out, `A: "Kẻ gian mạo danh"`) {
		t.Errorf("preview newline not collapsed: %q", out)
	}
	// Footer lists every involved chunk id, sorted ascending, de-duplicated.
	if !strings.Contains(out, "Chunk ids involved: 5, 9, 41, 55, 88, 91") {
		t.Errorf("footer id list wrong: %q", out)
	}
	if !strings.Contains(out, "3 pairs across 2 scam_types") {
		t.Errorf("footer totals wrong: %q", out)
	}
}

func TestRenderPruneDryRun(t *testing.T) {
	var b strings.Builder
	renderPrune(&b, store.DeletionResult{DryRun: true, Deleted: []int64{41, 88}})
	out := b.String()
	if !strings.Contains(out, "DRY RUN") || !strings.Contains(out, "-apply") {
		t.Errorf("dry-run banner missing: %q", out)
	}
	if !strings.Contains(out, "would delete 2 chunks: 41, 88") {
		t.Errorf("would-delete line wrong: %q", out)
	}
}

func TestRenderPruneWarnings(t *testing.T) {
	res := store.DeletionResult{
		Deleted:         []int64{41},
		Missing:         []int64{999},
		EmptiedDocs:     []store.DocRemainder{{DocumentID: 7, URL: "https://e", Remaining: 0}},
		ContentlessDocs: []store.DocRemainder{{DocumentID: 12, URL: "https://f", Remaining: 3}},
	}
	var b strings.Builder
	renderPrune(&b, res)
	out := b.String()

	if !strings.Contains(out, "deleted 1 chunk: 41") {
		t.Errorf("applied delete line wrong: %q", out)
	}
	if !strings.Contains(out, "not found: 999") {
		t.Errorf("missing-id warning absent: %q", out)
	}
	if !strings.Contains(out, "document 7 (https://e) is left with 0 chunks") {
		t.Errorf("emptied-doc warning absent: %q", out)
	}
	if !strings.Contains(out, "document 12 (https://f) is left with 3 chunks but no content chunk") {
		t.Errorf("contentless-doc warning absent: %q", out)
	}
}
