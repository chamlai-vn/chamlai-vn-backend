package main

import "github.com/chamlai-vn/chamlai-vn-backend/internal/scam/analyzer"

// Style is one of four ways a test-case message is phrased, so the dataset
// stress-tests short queries, long narration, verbatim scammer messages, and
// third-person retellings roughly equally rather than only one register.
const (
	StyleShortQuestion   = "short_question"
	StyleLongNarration   = "long_narration"
	StyleVerbatimMessage = "verbatim_message"
	StyleThirdPerson     = "third_person"
)

// AllStyles is the fixed rotation dataset.go cycles through per scam type
// (and per benign case) so styles land roughly evenly, not by chance.
var AllStyles = []string{StyleShortQuestion, StyleLongNarration, StyleVerbatimMessage, StyleThirdPerson}

// Arm names — used as file-name components (see arms.go) and report labels.
const (
	ArmRAGHybrid        = "rag-hybrid"
	ArmGenericWebSearch = "generic-websearch"
)

// TestCase is one ground-truth test message. Ground truth (IsScam,
// ExpectedVerdict, ScamType, Style) is assigned deterministically by the
// harness in dataset.go, never guessed by the generator model — the
// generator (Haiku) only supplies Text and KeyFacts. Unlike the retrieval
// benchmark's dataset, TestCase does not reference a chunk_id: it is
// independent of any corpus snapshot, so it stays reviewable and reusable
// across corpus changes.
type TestCase struct {
	ID              string `json:"id"`
	Text            string `json:"text"`
	IsScam          bool   `json:"is_scam"`
	ExpectedVerdict string `json:"expected_verdict"` // "red" | "green"
	ScamType        string `json:"scam_type"`        // "" for benign cases
	Style           string `json:"style"`
	// KeyFacts are the points a good answer should mention — the rubric fed
	// to the judge. Deliberately does NOT include ExpectedVerdict: the judge
	// scores explanation quality, not verdict correctness (that's what the
	// objective confusion matrix in pkg/util/eval is for) — see judge.go.
	KeyFacts []string `json:"key_facts"`
}

// ArmOutput is what one arm produced for one test case. Persisted as its own
// file per (case, arm) pair — see arms.go's checkpoint layout — so concurrent
// workers never write the same path. Only ever written on success: a hard
// failure (API error, bad JSON) is logged and retried on the next -run
// invocation rather than checkpointed, mirroring cmd/crawler's
// ingester.handleFile (a failure never writes an output file, only a
// completed result does) — see runArmPool.
type ArmOutput struct {
	CaseID string                  `json:"case_id"`
	Arm    string                  `json:"arm"`
	Result analyzer.AnalysisResult `json:"result"`
	// RawText is the generic arm's natural-language answer before it was
	// structured into Result — what a real user would actually see. Empty
	// for the rag-hybrid arm (its Result IS the natural output).
	RawText string `json:"raw_text,omitempty"`
	// Sources are URLs the generic arm's web search cited. Always empty for
	// rag-hybrid.
	Sources []string `json:"sources,omitempty"`
	// SearchFailed marks a generic-arm case where web search returned empty,
	// errored, or hit max_uses_exceeded — Result still reflects the model's
	// best answer from internal knowledge, just flagged as unverified. This
	// is a complete, checkpointable outcome, unlike a hard error above.
	SearchFailed bool  `json:"search_failed,omitempty"`
	LatencyMS    int64 `json:"latency_ms"`
}

// JudgeVerdict is the fixed per-answer scoring shape the product requires:
// strengths, weaknesses, reasoning, and a 0-10 score (rounded to 2 decimals
// when aggregated in report.go). This format does not change based on how
// the score was collected — see judge.go for the dual-order collection
// method that replaced independent blind scoring.
type JudgeVerdict struct {
	Strengths  []string `json:"strengths"`
	Weaknesses []string `json:"weaknesses"`
	Reasoning  string   `json:"reasoning"`
	Score      float64  `json:"score"`
}

// JudgeCallResult is one Opus call's raw output: the judge sees both answers
// in a single prompt (labeled "A"/"B", the caller controls which arm is
// which — see judge.go's dual-order scheme), scores each independently in
// the required format, and states an overall preference.
type JudgeCallResult struct {
	AnswerA         JudgeVerdict `json:"answer_a"`
	AnswerB         JudgeVerdict `json:"answer_b"`
	PreferredAnswer string       `json:"preferred_answer"` // "A" | "B" | "tie"
}

// JudgedCase is the order-independent result for one case, after both
// dual-order calls (RAG=A/Generic=B, then RAG=B/Generic=A) are decoded back
// to arms. OrderConsistent reports whether both calls agreed on which arm
// was better — a false value means the "which arm wins" signal for this
// case is not reliable and is reported as "tie" rather than silently
// averaged away. judge.go writes this same shape to two different files per
// case: judged/<id>.json (the primary Opus judge) and, for a validation
// subset, judged/<id>.cross.json (a cross-family judge, e.g. Gemini) — file
// presence alone distinguishes them, so no extra field is needed here.
type JudgedCase struct {
	CaseID         string       `json:"case_id"`
	RAGVerdict     JudgeVerdict `json:"rag_verdict"`
	GenericVerdict JudgeVerdict `json:"generic_verdict"`
	// Preferred is the arm name ("rag-hybrid"/"generic-websearch") that won
	// both orderings, or "tie" if they disagreed or the judge said tie.
	Preferred       string `json:"preferred"`
	OrderConsistent bool   `json:"order_consistent"`
}

// RunMeta is provenance for one full benchmark run, written incrementally by
// each phase (-gen fills the dataset fields, -run fills the arm-model
// fields, -judge fills the judge fields) so a report can never be misread as
// measuring a different configuration than it actually ran.
type RunMeta struct {
	Timestamp   string `json:"timestamp"`
	GitSHA      string `json:"git_sha"`
	DatasetHash string `json:"dataset_hash"`
	NCases      int    `json:"n_cases"`

	GeneratorModel string `json:"generator_model,omitempty"`

	RAGModel         string `json:"rag_model,omitempty"`
	RAGTopK          int    `json:"rag_top_k,omitempty"`
	RerankerEnabled  bool   `json:"reranker_enabled,omitempty"`
	GenericModel     string `json:"generic_model,omitempty"`
	WebSearchMaxUses int    `json:"web_search_max_uses,omitempty"`

	JudgeModel       string `json:"judge_model,omitempty"`
	CrossFamilyModel string `json:"cross_family_model,omitempty"`
	CrossFamilyN     int    `json:"cross_family_n,omitempty"`
}
