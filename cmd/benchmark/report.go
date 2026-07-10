package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"os"
	"path/filepath"
	"sort"

	evalutil "github.com/chamlai-vn/chamlai-vn-backend/pkg/util/eval"
)

const (
	resultsFileName = "results.json"
	summaryFileName = "summary.csv"
	reportFileName  = "report.html"
	nExamplesEach   = 3
)

// caseResult is the full per-case record written to results.json — enough
// to audit any single row of the benchmark by hand. Pointer fields are nil
// when that phase hasn't produced output for this case yet (e.g. -report
// run before -judge finished).
type caseResult struct {
	Case           TestCase    `json:"case"`
	RAG            *ArmOutput  `json:"rag,omitempty"`
	Generic        *ArmOutput  `json:"generic,omitempty"`
	Judged         *JudgedCase `json:"judged,omitempty"`
	CrossJudged    *JudgedCase `json:"cross_judged,omitempty"`
	RAGCorrect     bool        `json:"rag_correct"`
	GenericCorrect bool        `json:"generic_correct"`
}

type scamTypeRow struct {
	ScamType       string
	N              int
	RAGCorrect     int
	GenericCorrect int
}

type exampleTranscript struct {
	CaseID         string
	Text           string
	Expected       string
	RAGVerdict     string
	GenericVerdict string
	Preferred      string
	RAGScore       float64
	GenericScore   float64
}

// reportData is everything report.html's template needs, precomputed so the
// template stays presentation-only.
type reportData struct {
	Meta RunMeta
	N    int

	RAGConfusion     evalutil.VerdictConfusion
	GenericConfusion evalutil.VerdictConfusion

	NJudged             int
	RAGAvgScore         float64
	GenericAvgScore     float64
	Wins                map[string]int // arm name (or "tie") -> count
	OrderConsistentRate float64

	CrossFamilyN             int
	CrossFamilyAgreementRate float64

	ByScamType []scamTypeRow
	Examples   []exampleTranscript
}

// runReport reads everything -gen/-run/-judge produced in runDir and writes
// results.json (full detail), summary.csv (one row per case), and
// report.html (self-contained, no external resources) — the artifact meant
// to be read by someone who isn't going to open results.json.
func runReport(runDir string) {
	cases, err := readDataset(runDir)
	if err != nil {
		log.Fatalf("report: %v", err)
	}
	meta := readMeta(runDir)

	results := make([]caseResult, 0, len(cases))
	var ragJudgments, genericJudgments []evalutil.VerdictJudgment
	var ragScores, genericScores []float64
	wins := map[string]int{}
	var nJudged, nOrderConsistent, nCrossJudged, nCrossAgree int
	byScamType := map[string]*scamTypeRow{}

	for _, tc := range cases {
		cr := caseResult{Case: tc}

		if out, err := readArmOutput(armFilePath(runDir, tc.ID, ArmRAGHybrid)); err == nil {
			out := out
			cr.RAG = &out
			cr.RAGCorrect = out.Result.RiskLevel == tc.ExpectedVerdict
			ragJudgments = append(ragJudgments, evalutil.VerdictJudgment{
				Got: out.Result.RiskLevel, Expected: tc.ExpectedVerdict, IsScam: tc.IsScam,
			})
		}
		if out, err := readArmOutput(armFilePath(runDir, tc.ID, ArmGenericWebSearch)); err == nil {
			out := out
			cr.Generic = &out
			cr.GenericCorrect = out.Result.RiskLevel == tc.ExpectedVerdict
			genericJudgments = append(genericJudgments, evalutil.VerdictJudgment{
				Got: out.Result.RiskLevel, Expected: tc.ExpectedVerdict, IsScam: tc.IsScam,
			})
		}

		if jc, err := readJudgedCase(judgedFilePath(runDir, tc.ID, "")); err == nil {
			jc := jc
			cr.Judged = &jc
			nJudged++
			ragScores = append(ragScores, jc.RAGVerdict.Score)
			genericScores = append(genericScores, jc.GenericVerdict.Score)
			wins[jc.Preferred]++
			if jc.OrderConsistent {
				nOrderConsistent++
			}
		}
		if jc, err := readJudgedCase(judgedFilePath(runDir, tc.ID, crossFileSuffix)); err == nil {
			jc := jc
			cr.CrossJudged = &jc
			nCrossJudged++
			if cr.Judged != nil && cr.Judged.Preferred == jc.Preferred {
				nCrossAgree++
			}
		}

		if tc.ScamType != "" {
			row := byScamType[tc.ScamType]
			if row == nil {
				row = &scamTypeRow{ScamType: tc.ScamType}
				byScamType[tc.ScamType] = row
			}
			row.N++
			if cr.RAGCorrect {
				row.RAGCorrect++
			}
			if cr.GenericCorrect {
				row.GenericCorrect++
			}
		}

		results = append(results, cr)
	}

	data := reportData{
		Meta:             meta,
		N:                len(cases),
		RAGConfusion:     evalutil.SummarizeVerdicts(ragJudgments),
		GenericConfusion: evalutil.SummarizeVerdicts(genericJudgments),
		NJudged:          nJudged,
		Wins:             wins,
		Examples:         pickExamples(results),
	}
	if nJudged > 0 {
		data.RAGAvgScore = mean(ragScores)
		data.GenericAvgScore = mean(genericScores)
		data.OrderConsistentRate = float64(nOrderConsistent) / float64(nJudged)
	}
	if nCrossJudged > 0 {
		data.CrossFamilyN = nCrossJudged
		data.CrossFamilyAgreementRate = float64(nCrossAgree) / float64(nCrossJudged)
	}
	for _, row := range byScamType {
		data.ByScamType = append(data.ByScamType, *row)
	}
	sort.Slice(data.ByScamType, func(i, j int) bool { return data.ByScamType[i].ScamType < data.ByScamType[j].ScamType })

	if err := writeResultsJSON(runDir, results); err != nil {
		log.Fatalf("report: %v", err)
	}
	if err := writeSummaryCSV(runDir, results); err != nil {
		log.Fatalf("report: %v", err)
	}
	if err := writeReportHTML(runDir, data); err != nil {
		log.Fatalf("report: %v", err)
	}

	log.Printf("report: wrote %s, %s, %s in %s", resultsFileName, summaryFileName, reportFileName, runDir)
}

// pickExamples returns up to nExamplesEach cases where the judge scored
// rag-hybrid highest above generic-websearch, and up to nExamplesEach where
// it scored lowest below it — the most persuasive (or most concerning)
// transcripts for a non-technical reader, picked objectively rather than by
// hand.
func pickExamples(results []caseResult) []exampleTranscript {
	type scored struct {
		cr   caseResult
		diff float64
	}
	var scoredCases []scored
	for _, cr := range results {
		if cr.Judged == nil {
			continue
		}
		scoredCases = append(scoredCases, scored{cr, cr.Judged.RAGVerdict.Score - cr.Judged.GenericVerdict.Score})
	}
	sort.Slice(scoredCases, func(i, j int) bool { return scoredCases[i].diff > scoredCases[j].diff })

	toExample := func(cr caseResult) exampleTranscript {
		ex := exampleTranscript{
			CaseID:       cr.Case.ID,
			Text:         cr.Case.Text,
			Expected:     cr.Case.ExpectedVerdict,
			Preferred:    cr.Judged.Preferred,
			RAGScore:     cr.Judged.RAGVerdict.Score,
			GenericScore: cr.Judged.GenericVerdict.Score,
		}
		if cr.RAG != nil {
			ex.RAGVerdict = cr.RAG.Result.RiskLevel
		}
		if cr.Generic != nil {
			ex.GenericVerdict = cr.Generic.Result.RiskLevel
		}
		return ex
	}

	n := len(scoredCases)
	top := min(nExamplesEach, n)
	examples := make([]exampleTranscript, 0, 2*nExamplesEach)
	for i := 0; i < top; i++ {
		examples = append(examples, toExample(scoredCases[i].cr))
	}
	bottomStart := max(n-nExamplesEach, top)
	for i := bottomStart; i < n; i++ {
		examples = append(examples, toExample(scoredCases[i].cr))
	}
	return examples
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func writeResultsJSON(runDir string, results []caseResult) error {
	raw, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal results: %w", err)
	}
	return os.WriteFile(filepath.Join(runDir, resultsFileName), raw, 0o644)
}

func writeSummaryCSV(runDir string, results []caseResult) error {
	f, err := os.Create(filepath.Join(runDir, summaryFileName))
	if err != nil {
		return fmt.Errorf("create %s: %w", summaryFileName, err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{
		"case_id", "scam_type", "style", "expected",
		"rag_verdict", "generic_verdict", "rag_correct", "generic_correct",
		"rag_score", "generic_score", "preferred", "order_consistent",
	}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, cr := range results {
		row := []string{
			cr.Case.ID, cr.Case.ScamType, cr.Case.Style, cr.Case.ExpectedVerdict,
			verdictOrEmpty(cr.RAG), verdictOrEmpty(cr.Generic),
			boolStr(cr.RAGCorrect), boolStr(cr.GenericCorrect),
			scoreOrEmpty(cr.Judged, true), scoreOrEmpty(cr.Judged, false),
			preferredOrEmpty(cr.Judged), orderConsistentOrEmpty(cr.Judged),
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

func verdictOrEmpty(out *ArmOutput) string {
	if out == nil {
		return ""
	}
	return out.Result.RiskLevel
}
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
func scoreOrEmpty(jc *JudgedCase, rag bool) string {
	if jc == nil {
		return ""
	}
	if rag {
		return fmt.Sprintf("%.2f", jc.RAGVerdict.Score)
	}
	return fmt.Sprintf("%.2f", jc.GenericVerdict.Score)
}
func preferredOrEmpty(jc *JudgedCase) string {
	if jc == nil {
		return ""
	}
	return jc.Preferred
}
func orderConsistentOrEmpty(jc *JudgedCase) string {
	if jc == nil {
		return ""
	}
	return boolStr(jc.OrderConsistent)
}

var reportFuncs = template.FuncMap{
	"pct":    func(f float64) string { return fmt.Sprintf("%.1f%%", f*100) },
	"round2": func(f float64) string { return fmt.Sprintf("%.2f", f) },
}

func writeReportHTML(runDir string, data reportData) error {
	tmpl, err := template.New("report").Funcs(reportFuncs).Parse(reportTemplate)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}
	f, err := os.Create(filepath.Join(runDir, reportFileName))
	if err != nil {
		return fmt.Errorf("create %s: %w", reportFileName, err)
	}
	defer f.Close()
	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("render template: %w", err)
	}
	return nil
}

const reportTemplate = `<!doctype html>
<html lang="vi">
<head>
<meta charset="utf-8">
<title>RAG Value Benchmark — {{.Meta.Timestamp}}</title>
<style>
  body { font-family: -apple-system, "Segoe UI", sans-serif; max-width: 960px; margin: 2rem auto; padding: 0 1rem; color: #1a1a1a; line-height: 1.5; }
  h1, h2 { border-bottom: 2px solid #eee; padding-bottom: .3rem; }
  table { border-collapse: collapse; width: 100%; margin: 1rem 0; }
  th, td { border: 1px solid #ddd; padding: .4rem .6rem; text-align: left; font-size: .9rem; }
  th { background: #f5f5f5; }
  .headline { display: flex; gap: 2rem; flex-wrap: wrap; margin: 1rem 0; }
  .stat { background: #f9f9f9; border-radius: 8px; padding: 1rem 1.5rem; min-width: 160px; }
  .stat .big { font-size: 1.8rem; font-weight: 700; }
  .stat .label { color: #666; font-size: .85rem; }
  .arm-rag { color: #0a6b2d; }
  .arm-generic { color: #a15c00; }
  .meta { color: #666; font-size: .85rem; }
  .transcript { background: #f9f9f9; border-radius: 8px; padding: 1rem; margin: .75rem 0; white-space: pre-wrap; }
  code { background: #eee; padding: .1rem .3rem; border-radius: 4px; }
</style>
</head>
<body>

<h1>RAG Value Benchmark</h1>
<p class="meta">
  {{.N}} case(s) · chạy lúc {{.Meta.Timestamp}} · git {{.Meta.GitSHA}} · dataset {{.Meta.DatasetHash}}<br>
  rag-hybrid: <code>{{.Meta.RAGModel}}</code> (topK={{.Meta.RAGTopK}}, reranker={{.Meta.RerankerEnabled}}) ·
  generic-websearch: <code>{{.Meta.GenericModel}}</code> (max_uses={{.Meta.WebSearchMaxUses}}) ·
  judge: <code>{{.Meta.JudgeModel}}</code>
</p>

<h2>Headline</h2>
<div class="headline">
  <div class="stat"><div class="big arm-rag">{{round2 .RAGAvgScore}}</div><div class="label">rag-hybrid — điểm judge TB (0–10)</div></div>
  <div class="stat"><div class="big arm-generic">{{round2 .GenericAvgScore}}</div><div class="label">generic-websearch — điểm judge TB (0–10)</div></div>
  <div class="stat"><div class="big">{{index .Wins "rag-hybrid"}} / {{index .Wins "tie"}} / {{index .Wins "generic-websearch"}}</div><div class="label">win / tie / loss (rag vs generic)</div></div>
  <div class="stat"><div class="big">{{pct .OrderConsistentRate}}</div><div class="label">order-consistent ({{.NJudged}} case đã chấm)</div></div>
</div>
{{if .CrossFamilyN}}
<p class="meta">Cross-family validation ({{.CrossFamilyN}} case, judge khác họ): đồng ý với Opus judge {{pct .CrossFamilyAgreementRate}} thời gian.</p>
{{end}}

<h2>Độ chính xác verdict (khách quan)</h2>
<table>
<tr><th>Arm</th><th>Accuracy</th><th>False-positive rate<br><span class="meta">(benign → red)</span></th><th>False-negative rate<br><span class="meta">(scam → green)</span></th></tr>
<tr><td class="arm-rag">rag-hybrid</td><td>{{pct .RAGConfusion.Accuracy}}</td><td>{{pct .RAGConfusion.FalsePositiveRate}}</td><td>{{pct .RAGConfusion.FalseNegativeRate}}</td></tr>
<tr><td class="arm-generic">generic-websearch</td><td>{{pct .GenericConfusion.Accuracy}}</td><td>{{pct .GenericConfusion.FalsePositiveRate}}</td><td>{{pct .GenericConfusion.FalseNegativeRate}}</td></tr>
</table>

<h2>Theo scam_type</h2>
<table>
<tr><th>scam_type</th><th>N</th><th>rag-hybrid đúng</th><th>generic-websearch đúng</th></tr>
{{range .ByScamType}}<tr><td>{{.ScamType}}</td><td>{{.N}}</td><td>{{.RAGCorrect}}/{{.N}}</td><td>{{.GenericCorrect}}/{{.N}}</td></tr>
{{end}}
</table>

<h2>Ví dụ minh hoạ</h2>
{{range .Examples}}
<div class="transcript">
<strong>{{.CaseID}}</strong> — expected: {{.Expected}} · rag: {{.RAGVerdict}} ({{round2 .RAGScore}}) · generic: {{.GenericVerdict}} ({{round2 .GenericScore}}) · preferred: {{.Preferred}}
<hr>
{{.Text}}
</div>
{{end}}

</body>
</html>
`
