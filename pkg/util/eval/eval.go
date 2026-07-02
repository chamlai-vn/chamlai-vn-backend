// Package evalutil holds reusable, dependency-free retrieval-quality metrics
// for comparing a ranked list of ids against a single known-relevant id — e.g.
// scoring vector-only vs hybrid search on a benchmark dataset. No Service
// interface, no Config injection, mirrors pkg/util/rag's shape.
package evalutil

// Judgment is the outcome of scoring one ranked result list against one
// expected-relevant id, cut off at k.
type Judgment struct {
	// Hit reports whether expectedID appeared within the first k ids.
	Hit bool
	// ReciprocalRank is 1/rank (1-based) if expectedID was found within k,
	// else 0. Summed and averaged across queries, this is MRR.
	ReciprocalRank float64
}

// Judge scores rankedIDs (best match first) against expectedID, considering
// only the first k entries. k <= 0 or k > len(rankedIDs) considers the whole
// list.
func Judge(rankedIDs []int64, expectedID int64, k int) Judgment {
	if k <= 0 || k > len(rankedIDs) {
		k = len(rankedIDs)
	}
	for i := 0; i < k; i++ {
		if rankedIDs[i] == expectedID {
			return Judgment{Hit: true, ReciprocalRank: 1.0 / float64(i+1)}
		}
	}
	return Judgment{}
}

// Aggregate summarizes many Judgments into corpus-level metrics.
type Aggregate struct {
	N int
	// RecallAtK is the fraction of queries where the expected id was found
	// within k (mean of Hit).
	RecallAtK float64
	// MRR is the mean reciprocal rank across all queries (0 for a miss).
	MRR float64
}

// Summarize aggregates judgments into Aggregate. An empty input returns the
// zero Aggregate (N=0), not a NaN — callers can check N before trusting the
// rates.
func Summarize(judgments []Judgment) Aggregate {
	if len(judgments) == 0 {
		return Aggregate{}
	}
	var hits, rrSum float64
	for _, j := range judgments {
		if j.Hit {
			hits++
		}
		rrSum += j.ReciprocalRank
	}
	n := len(judgments)
	return Aggregate{N: n, RecallAtK: hits / float64(n), MRR: rrSum / float64(n)}
}
