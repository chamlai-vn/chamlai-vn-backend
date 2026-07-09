package store

import (
	"context"
	"fmt"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/model"
)

// DuplicatePair is one cross-document near-duplicate chunk pair found by
// FindDuplicateChunks: two chunks of the SAME kind and SAME scam_type but
// DIFFERENT documents, whose embeddings are cosine-similar at or above the
// requested threshold. A ordered before B (a.id < b.id) so each unordered pair
// is reported once. Read-model only — never written back.
type DuplicatePair struct {
	ScamType   string
	Kind       model.ChunkKind
	Similarity float64 // 1 - cosine distance, in (threshold, 1]

	AID      int64
	ADocID   int64
	AURL     string
	APreview string

	BID      int64
	BDocID   int64
	BURL     string
	BPreview string
}

// FindDuplicateChunks lists cross-document near-duplicate chunk pairs, grouped
// by scam_type and ordered most-similar-first within each group. Only chunks of
// the same kind (content↔content, query↔query) and the same documents.scam_type
// are compared; within-document pairs are excluded (the ingest pipeline already
// dedupes query chunks inside one document — see internal/scam/ingest).
//
// simThreshold is a cosine SIMILARITY in (0,1]; pgvector's <=> operator returns
// cosine DISTANCE (1 - similarity) and normalises internally, so correctness
// does not depend on the stored vectors being unit-norm. previewLen caps the
// per-side content preview (character count, UTF-8 safe via LEFT). scamType
// restricts the scan to one type when non-empty; "" scans all.
//
// The comparison is an exact self-join, not an ANN lookup — the HNSW index is
// not used here. That is fine for offline corpus hygiene at current corpus
// sizes; revisit only if a single scam_type grows to many thousands of chunks.
func (s *Store) FindDuplicateChunks(ctx context.Context, simThreshold float64, scamType string, previewLen int) ([]DuplicatePair, error) {
	distThreshold := 1 - simThreshold
	rows, err := s.pool.Query(ctx,
		`SELECT da.scam_type, a.kind,
		        1 - (a.embedding <=> b.embedding) AS similarity,
		        a.id, a.document_id, da.url, LEFT(a.content, $2) AS a_preview,
		        b.id, b.document_id, db.url, LEFT(b.content, $2) AS b_preview
		 FROM chunks a
		 JOIN documents da ON da.id = a.document_id
		 JOIN chunks b     ON b.kind = a.kind AND b.id > a.id
		 JOIN documents db ON db.id = b.document_id
		 WHERE da.scam_type = db.scam_type
		   AND a.document_id <> b.document_id
		   AND (a.embedding <=> b.embedding) <= $1
		   AND ($3 = '' OR da.scam_type = $3)
		 ORDER BY da.scam_type, similarity DESC, a.id, b.id`,
		distThreshold, previewLen, scamType)
	if err != nil {
		return nil, fmt.Errorf("store: find duplicate chunks: %w", err)
	}
	defer rows.Close()

	var out []DuplicatePair
	for rows.Next() {
		var p DuplicatePair
		var kind string
		if err := rows.Scan(&p.ScamType, &kind, &p.Similarity,
			&p.AID, &p.ADocID, &p.AURL, &p.APreview,
			&p.BID, &p.BDocID, &p.BURL, &p.BPreview); err != nil {
			return nil, fmt.Errorf("store: scan duplicate pair: %w", err)
		}
		p.Kind = model.ChunkKind(kind)
		out = append(out, p)
	}
	return out, rows.Err()
}

// DocRemainder is one document's post-delete chunk situation, used to warn the
// operator that a prune left a document degraded.
type DocRemainder struct {
	DocumentID int64
	URL        string
	Remaining  int // chunks left on the document after the delete
}

// DeletionResult reports what a DeleteChunks call did (or, under dryRun, would
// do). Deleted holds the ids removed by the DELETE; Missing holds requested ids
// that were not present. EmptiedDocs are documents left with zero chunks;
// ContentlessDocs are documents left with chunks but no content chunk (the
// full body still lives in documents.content and the document row survives —
// deleting chunks never cascades to the document).
type DeletionResult struct {
	Deleted         []int64
	Missing         []int64
	EmptiedDocs     []DocRemainder
	ContentlessDocs []DocRemainder
	DryRun          bool
}

// DeleteChunks deletes the given chunk ids and reports the outcome. It never
// chooses which chunks to remove — callers pass an explicit id list (typically
// picked from a FindDuplicateChunks report).
//
// Everything happens in one transaction so the delete is all-or-nothing, and
// the post-delete document warnings (EmptiedDocs/ContentlessDocs) are computed
// from the real in-transaction state. When dryRun is true the transaction is
// rolled back, so the report reflects exactly what WOULD happen without
// mutating anything.
func (s *Store) DeleteChunks(ctx context.Context, ids []int64, dryRun bool) (DeletionResult, error) {
	res := DeletionResult{DryRun: dryRun}
	if len(ids) == 0 {
		return res, fmt.Errorf("store: no chunk ids to delete")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return res, fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Delete and learn which ids actually existed and which documents they
	// belonged to.
	rows, err := tx.Query(ctx,
		`DELETE FROM chunks WHERE id = ANY($1) RETURNING id, document_id`, ids)
	if err != nil {
		return res, fmt.Errorf("store: delete chunks: %w", err)
	}
	deletedSet := make(map[int64]struct{}, len(ids))
	affectedDocs := make(map[int64]struct{})
	for rows.Next() {
		var id, docID int64
		if err := rows.Scan(&id, &docID); err != nil {
			rows.Close()
			return res, fmt.Errorf("store: scan deleted chunk: %w", err)
		}
		res.Deleted = append(res.Deleted, id)
		deletedSet[id] = struct{}{}
		affectedDocs[docID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return res, fmt.Errorf("store: iterate deleted chunks: %w", err)
	}

	for _, id := range ids {
		if _, ok := deletedSet[id]; !ok {
			res.Missing = append(res.Missing, id)
		}
	}

	// Post-delete state of every affected document (LEFT JOIN so a document
	// whose chunks are now all gone still appears with remaining = 0).
	if len(affectedDocs) > 0 {
		docIDs := make([]int64, 0, len(affectedDocs))
		for id := range affectedDocs {
			docIDs = append(docIDs, id)
		}
		statRows, err := tx.Query(ctx,
			`SELECT d.id, d.url,
			        count(c.id) AS remaining,
			        count(c.id) FILTER (WHERE c.kind = 'content') AS content_remaining
			 FROM documents d
			 LEFT JOIN chunks c ON c.document_id = d.id
			 WHERE d.id = ANY($1)
			 GROUP BY d.id, d.url
			 ORDER BY d.id`, docIDs)
		if err != nil {
			return res, fmt.Errorf("store: document stats: %w", err)
		}
		for statRows.Next() {
			var dr DocRemainder
			var contentRemaining int
			if err := statRows.Scan(&dr.DocumentID, &dr.URL, &dr.Remaining, &contentRemaining); err != nil {
				statRows.Close()
				return res, fmt.Errorf("store: scan document stats: %w", err)
			}
			switch {
			case dr.Remaining == 0:
				res.EmptiedDocs = append(res.EmptiedDocs, dr)
			case contentRemaining == 0:
				res.ContentlessDocs = append(res.ContentlessDocs, dr)
			}
		}
		if err := statRows.Err(); err != nil {
			return res, fmt.Errorf("store: iterate document stats: %w", err)
		}
	}

	if dryRun {
		return res, nil // deferred Rollback discards the delete
	}
	if err := tx.Commit(ctx); err != nil {
		return res, fmt.Errorf("store: commit: %w", err)
	}
	return res, nil
}
