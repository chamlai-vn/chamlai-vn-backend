package evalutil

import "testing"

func TestSummarizeVerdicts_Empty(t *testing.T) {
	c := SummarizeVerdicts(nil)
	if c.N != 0 || c.Accuracy != 0 || c.FalsePositiveRate != 0 || c.FalseNegativeRate != 0 {
		t.Errorf("got %+v, want zero VerdictConfusion", c)
	}
}

func TestSummarizeVerdicts_AllCorrect(t *testing.T) {
	judgments := []VerdictJudgment{
		{Got: VerdictRed, Expected: VerdictRed, IsScam: true},
		{Got: VerdictGreen, Expected: VerdictGreen, IsScam: false},
	}
	c := SummarizeVerdicts(judgments)
	if c.N != 2 {
		t.Errorf("N = %d, want 2", c.N)
	}
	if c.Accuracy != 1.0 {
		t.Errorf("Accuracy = %v, want 1.0", c.Accuracy)
	}
	if c.FalsePositiveRate != 0 || c.FalseNegativeRate != 0 {
		t.Errorf("FP=%v FN=%v, want both 0", c.FalsePositiveRate, c.FalseNegativeRate)
	}
}

func TestSummarizeVerdicts_FalsePositiveOnBenign(t *testing.T) {
	// A benign case scored red is a false alarm — must show up in FalsePositiveRate,
	// not silently averaged away by overall accuracy.
	judgments := []VerdictJudgment{
		{Got: VerdictRed, Expected: VerdictGreen, IsScam: false},
		{Got: VerdictGreen, Expected: VerdictGreen, IsScam: false},
	}
	c := SummarizeVerdicts(judgments)
	if c.FalsePositiveRate != 0.5 {
		t.Errorf("FalsePositiveRate = %v, want 0.5 (1 of 2 benign cases flagged red)", c.FalsePositiveRate)
	}
	if c.FalseNegativeRate != 0 {
		t.Errorf("FalseNegativeRate = %v, want 0 — no scam cases in this input", c.FalseNegativeRate)
	}
}

func TestSummarizeVerdicts_FalseNegativeOnScam(t *testing.T) {
	// A scam case scored green is a missed warning — the dangerous miss this
	// benchmark exists to catch.
	judgments := []VerdictJudgment{
		{Got: VerdictGreen, Expected: VerdictRed, IsScam: true},
		{Got: VerdictRed, Expected: VerdictRed, IsScam: true},
	}
	c := SummarizeVerdicts(judgments)
	if c.FalseNegativeRate != 0.5 {
		t.Errorf("FalseNegativeRate = %v, want 0.5 (1 of 2 scam cases missed as green)", c.FalseNegativeRate)
	}
}

func TestSummarizeVerdicts_YellowNotCountedAsFalsePositiveOrNegative(t *testing.T) {
	// yellow is a partial/hedged verdict, not a hard miss — it should lower
	// Accuracy but not count toward the hard safety-failure rates.
	judgments := []VerdictJudgment{
		{Got: VerdictYellow, Expected: VerdictGreen, IsScam: false},
		{Got: VerdictYellow, Expected: VerdictRed, IsScam: true},
	}
	c := SummarizeVerdicts(judgments)
	if c.Accuracy != 0 {
		t.Errorf("Accuracy = %v, want 0 (no exact matches)", c.Accuracy)
	}
	if c.FalsePositiveRate != 0 {
		t.Errorf("FalsePositiveRate = %v, want 0 — yellow on benign is not a false alarm", c.FalsePositiveRate)
	}
	if c.FalseNegativeRate != 0 {
		t.Errorf("FalseNegativeRate = %v, want 0 — yellow on scam is not a full miss", c.FalseNegativeRate)
	}
}

func TestSummarizeVerdicts_CountsMatrix(t *testing.T) {
	judgments := []VerdictJudgment{
		{Got: VerdictRed, Expected: VerdictRed, IsScam: true},
		{Got: VerdictYellow, Expected: VerdictRed, IsScam: true},
		{Got: VerdictGreen, Expected: VerdictGreen, IsScam: false},
	}
	c := SummarizeVerdicts(judgments)
	if c.Counts[VerdictRed][VerdictRed] != 1 {
		t.Errorf("Counts[red][red] = %d, want 1", c.Counts[VerdictRed][VerdictRed])
	}
	if c.Counts[VerdictRed][VerdictYellow] != 1 {
		t.Errorf("Counts[red][yellow] = %d, want 1", c.Counts[VerdictRed][VerdictYellow])
	}
	if c.Counts[VerdictGreen][VerdictGreen] != 1 {
		t.Errorf("Counts[green][green] = %d, want 1", c.Counts[VerdictGreen][VerdictGreen])
	}
}
