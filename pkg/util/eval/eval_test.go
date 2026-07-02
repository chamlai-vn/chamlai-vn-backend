package evalutil

import "testing"

func TestJudge_HitAtRankOne(t *testing.T) {
	j := Judge([]int64{5, 1, 9}, 5, 3)
	if !j.Hit || j.ReciprocalRank != 1.0 {
		t.Errorf("got %+v, want Hit=true ReciprocalRank=1.0", j)
	}
}

func TestJudge_HitAtLaterRank(t *testing.T) {
	j := Judge([]int64{5, 1, 9}, 9, 3)
	if !j.Hit || j.ReciprocalRank != 1.0/3.0 {
		t.Errorf("got %+v, want Hit=true ReciprocalRank=1/3", j)
	}
}

func TestJudge_MissWithinK(t *testing.T) {
	j := Judge([]int64{5, 1, 9}, 42, 3)
	if j.Hit || j.ReciprocalRank != 0 {
		t.Errorf("got %+v, want zero-value Judgment", j)
	}
}

func TestJudge_BeyondKCountsAsMiss(t *testing.T) {
	// expectedID is present but outside the top-k cutoff.
	j := Judge([]int64{5, 1, 9, 42}, 42, 2)
	if j.Hit {
		t.Errorf("got Hit=true, want miss — id 42 is at rank 4, cutoff k=2")
	}
}

func TestJudge_KLargerThanListUsesWholeList(t *testing.T) {
	j := Judge([]int64{5, 1}, 1, 10)
	if !j.Hit || j.ReciprocalRank != 0.5 {
		t.Errorf("got %+v, want Hit=true ReciprocalRank=0.5", j)
	}
}

func TestJudge_ZeroKUsesWholeList(t *testing.T) {
	j := Judge([]int64{5, 1}, 1, 0)
	if !j.Hit {
		t.Errorf("got Hit=false, want true — k<=0 should fall back to the whole list")
	}
}

func TestJudge_EmptyList(t *testing.T) {
	j := Judge(nil, 1, 5)
	if j.Hit || j.ReciprocalRank != 0 {
		t.Errorf("got %+v, want zero-value Judgment for empty list", j)
	}
}

func TestSummarize_Empty(t *testing.T) {
	agg := Summarize(nil)
	if agg.N != 0 || agg.RecallAtK != 0 || agg.MRR != 0 {
		t.Errorf("got %+v, want zero Aggregate", agg)
	}
}

func TestSummarize_MixedHitsAndMisses(t *testing.T) {
	judgments := []Judgment{
		{Hit: true, ReciprocalRank: 1.0}, // rank 1
		{Hit: true, ReciprocalRank: 0.5}, // rank 2
		{Hit: false, ReciprocalRank: 0},  // miss
		{Hit: false, ReciprocalRank: 0},  // miss
	}
	agg := Summarize(judgments)
	if agg.N != 4 {
		t.Errorf("N = %d, want 4", agg.N)
	}
	if agg.RecallAtK != 0.5 {
		t.Errorf("RecallAtK = %v, want 0.5 (2 hits / 4 queries)", agg.RecallAtK)
	}
	wantMRR := (1.0 + 0.5) / 4.0
	if agg.MRR != wantMRR {
		t.Errorf("MRR = %v, want %v", agg.MRR, wantMRR)
	}
}
