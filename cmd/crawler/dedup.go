package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/chamlai-vn/chamlai-vn-backend/config"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/infra/store"
)

// runAudit wires a database only (no embedder/LLM) and lists cross-document
// near-duplicate chunk pairs for human review. It never mutates anything and
// never suggests which side to delete — the operator decides that from the
// report (see -mode=prune).
func runAudit(ctx context.Context, cfg config.Configuration, threshold float64, scamType string, previewLen int) {
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	pairs, err := st.FindDuplicateChunks(ctx, threshold, scamType, previewLen)
	if err != nil {
		log.Fatalf("audit: %v", err)
	}
	renderAudit(os.Stdout, pairs, threshold)
}

// runPrune deletes an explicit set of chunk ids. It is a dry-run unless apply
// is true: without -apply it prints what WOULD be deleted and exits without
// touching the database.
func runPrune(ctx context.Context, cfg config.Configuration, ids []int64, apply bool) {
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	res, err := st.DeleteChunks(ctx, ids, !apply)
	if err != nil {
		log.Fatalf("prune: %v", err)
	}
	renderPrune(os.Stdout, res)
}

// parseChunkIDs parses a comma-separated chunk-id list ("41, 55 , 88") into a
// de-duplicated slice in first-seen order. Whitespace around each id is
// trimmed and blank fields are ignored; any non-numeric field is a hard error
// so a typo aborts before the tool touches the database.
func parseChunkIDs(s string) ([]int64, error) {
	var ids []int64
	seen := make(map[int64]struct{})
	for _, field := range strings.Split(s, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		id, err := strconv.ParseInt(field, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid chunk id %q", field)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

// renderAudit writes the duplicate-pair report grouped by scam_type. pairs are
// assumed already ordered by scam_type then similarity desc (as
// store.FindDuplicateChunks returns them).
func renderAudit(w io.Writer, pairs []store.DuplicatePair, threshold float64) {
	if len(pairs) == 0 {
		fmt.Fprintf(w, "no duplicate pairs found (threshold %.3f)\n", threshold)
		return
	}

	involved := make(map[int64]struct{})
	types := make(map[string]struct{})
	var lastType string
	for _, p := range pairs {
		if p.ScamType != lastType {
			// Count pairs in this group for the header.
			n := 0
			for _, q := range pairs {
				if q.ScamType == p.ScamType {
					n++
				}
			}
			fmt.Fprintf(w, "\n[%s]  (%d pair%s)\n", p.ScamType, n, plural(n))
			lastType = p.ScamType
		}
		fmt.Fprintf(w, "  %.3f  %-7s  chunk %d (doc %d  %s)  ~  chunk %d (doc %d  %s)\n",
			p.Similarity, p.Kind, p.AID, p.ADocID, p.AURL, p.BID, p.BDocID, p.BURL)
		fmt.Fprintf(w, "         A: %q\n", oneLine(p.APreview))
		fmt.Fprintf(w, "         B: %q\n", oneLine(p.BPreview))
		involved[p.AID] = struct{}{}
		involved[p.BID] = struct{}{}
		types[p.ScamType] = struct{}{}
	}

	fmt.Fprintf(w, "\n%d pair%s across %d scam_type%s. Chunk ids involved: %s\n",
		len(pairs), plural(len(pairs)), len(types), plural(len(types)), joinIDs(sortedKeys(involved)))
}

// renderPrune writes the outcome (or dry-run preview) of a DeleteChunks call.
func renderPrune(w io.Writer, res store.DeletionResult) {
	if res.DryRun {
		fmt.Fprintf(w, "DRY RUN — nothing was deleted. Re-run with -apply to delete.\n")
		fmt.Fprintf(w, "would delete %d chunk%s: %s\n", len(res.Deleted), plural(len(res.Deleted)), joinIDs(res.Deleted))
	} else {
		fmt.Fprintf(w, "deleted %d chunk%s: %s\n", len(res.Deleted), plural(len(res.Deleted)), joinIDs(res.Deleted))
	}

	if len(res.Missing) > 0 {
		fmt.Fprintf(w, "WARNING: %d requested id%s not found: %s\n",
			len(res.Missing), plural(len(res.Missing)), joinIDs(res.Missing))
	}
	for _, d := range res.EmptiedDocs {
		fmt.Fprintf(w, "WARNING: document %d (%s) is left with 0 chunks\n", d.DocumentID, d.URL)
	}
	for _, d := range res.ContentlessDocs {
		fmt.Fprintf(w, "WARNING: document %d (%s) is left with %d chunk%s but no content chunk\n",
			d.DocumentID, d.URL, d.Remaining, plural(d.Remaining))
	}
}

// oneLine collapses runs of whitespace (including embedded newlines from a
// multi-line content chunk) into single spaces so a preview stays on one line.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func joinIDs(ids []int64) string {
	if len(ids) == 0 {
		return "(none)"
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ", ")
}

func sortedKeys(m map[int64]struct{}) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
