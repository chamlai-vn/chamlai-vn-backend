// Package enrich turns one raw crawled scam-warning page into a
// corpusdoc.Document via an LLM forced tool call: it cleans/summarizes the
// content, classifies the scam type, and generates the doc2query
// "# User query" lines (mostly victim-voice questions, a minority of
// verbatim scam-message text) and prevention advice that the structured
// corpus format needs. It is the generate-side counterpart to
// internal/scam/ingest — crawler stays LLM-free (fetch+parse only); this
// package is where the LLM enters the pipeline.
//
// Construction in service.go (Enricher, New); DTOs and tool schema in
// type.go; prompts (system/user text, sanitization) in prompt.go; behaviour
// here.
package enrich

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/llm"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/crawler"
	"github.com/chamlai-vn/chamlai-vn-backend/pkg/util/corpusdoc"
)

// Enrich turns input into a corpusdoc.Document. input.URL is copied through
// verbatim and is never influenced by the LLM's output — the LLM only ever
// sees it for context, never as a field it can set, so a poisoned page
// can't spoof its own provenance (see docs/plans/2026-07-07-... Security).
//
// The LLM's scam_type output is validated against crawler.ValidScamTypes
// before this returns — defense in depth alongside the check corpusdoc.Parse
// does again when the reviewed file is later ingested (a mislabeled scam
// pattern is a label-evasion vector, so this is checked twice, not once).
func (e *Enricher) Enrich(ctx context.Context, input Input) (corpusdoc.Document, error) {
	raw, err := e.llm.GenerateStructured(ctx, llm.Request{
		System:   buildSystemPrompt(),
		User:     buildUserPrompt(input),
		ToolName: enrichToolName,
		ToolDesc: enrichToolDesc,
		Schema:   buildToolSchema(sortedValidScamTypes()),
	})
	if err != nil {
		return corpusdoc.Document{}, fmt.Errorf("enrich: generate: %w", err)
	}

	var res result
	if err := json.Unmarshal(raw, &res); err != nil {
		return corpusdoc.Document{}, fmt.Errorf("enrich: unmarshal result: %w", err)
	}
	if !crawler.ValidScamTypes[res.ScamType] {
		return corpusdoc.Document{}, fmt.Errorf("enrich: model returned unknown scam_type %q", res.ScamType)
	}

	return corpusdoc.Document{
		URL:         input.URL,
		Title:       res.Title,
		Content:     res.Content,
		ScamType:    res.ScamType,
		UserQueries: res.UserQueries,
		Prevention:  res.Prevention,
	}, nil
}

// sortedValidScamTypes returns crawler.ValidScamTypes' keys sorted, for a
// deterministic tool schema (map iteration order is randomized).
func sortedValidScamTypes() []string {
	out := make([]string, 0, len(crawler.ValidScamTypes))
	for t := range crawler.ValidScamTypes {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
