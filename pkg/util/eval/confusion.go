package evalutil

// Verdict levels a scam-scoring pipeline can emit. Mirrors
// internal/scam/analyzer's RiskRed/RiskYellow/RiskGreen constants; duplicated
// here (as plain strings) rather than imported so this package stays
// dependency-free, per the package doc.
const (
	VerdictRed    = "red"
	VerdictYellow = "yellow"
	VerdictGreen  = "green"
)

// VerdictJudgment is one scored test case: what a pipeline actually returned
// (Got) against the case's known-correct verdict (Expected). IsScam
// distinguishes the two safety-relevant failure modes that a flat accuracy
// number hides: a scam case scored green is a missed warning (false
// negative); a benign case scored red is a false alarm (false positive).
type VerdictJudgment struct {
	Got      string
	Expected string
	IsScam   bool
}

// VerdictConfusion aggregates many VerdictJudgments into a 3x3 confusion
// matrix (Expected -> Got -> count) plus the headline safety rates.
type VerdictConfusion struct {
	N int
	// Counts[expected][got] is how many cases with that expected verdict were
	// scored as got. Both keys are one of VerdictRed/VerdictYellow/VerdictGreen.
	Counts map[string]map[string]int
	// Accuracy is the fraction of cases where Got == Expected exactly.
	Accuracy float64
	// FalsePositiveRate is, among benign cases (IsScam=false), the fraction
	// scored VerdictRed — a false alarm on a real message.
	FalsePositiveRate float64
	// FalseNegativeRate is, among scam cases (IsScam=true), the fraction
	// scored VerdictGreen — a missed warning on a real scam.
	FalseNegativeRate float64
}

// SummarizeVerdicts aggregates judgments into a VerdictConfusion. An empty
// input returns the zero value (N=0), not NaN rates — callers should check N
// before trusting the rates, same convention as Summarize.
func SummarizeVerdicts(judgments []VerdictJudgment) VerdictConfusion {
	counts := map[string]map[string]int{
		VerdictRed:    {},
		VerdictYellow: {},
		VerdictGreen:  {},
	}
	if len(judgments) == 0 {
		return VerdictConfusion{Counts: counts}
	}

	var correct, benignN, benignFP, scamN, scamFN float64
	for _, j := range judgments {
		if counts[j.Expected] == nil {
			counts[j.Expected] = map[string]int{}
		}
		counts[j.Expected][j.Got]++

		if j.Got == j.Expected {
			correct++
		}
		if j.IsScam {
			scamN++
			if j.Got == VerdictGreen {
				scamFN++
			}
		} else {
			benignN++
			if j.Got == VerdictRed {
				benignFP++
			}
		}
	}

	n := len(judgments)
	c := VerdictConfusion{
		N:        n,
		Counts:   counts,
		Accuracy: correct / float64(n),
	}
	if benignN > 0 {
		c.FalsePositiveRate = benignFP / benignN
	}
	if scamN > 0 {
		c.FalseNegativeRate = scamFN / scamN
	}
	return c
}
