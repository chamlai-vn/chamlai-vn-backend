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
	"strings"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/llm"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/crawler"
	"github.com/chamlai-vn/chamlai-vn-backend/pkg/util/corpusdoc"
)

// enrichMaxTokens overrides the provider's own default (1024, tuned for
// analyzer's much smaller red/yellow/green verdict). Enrich has to generate a
// cleaned/summarized article body PLUS title, scam_type, 3-6 doc2query lines,
// and prevention advice in one JSON object — 1024 tokens is not enough
// headroom for a real article and was silently truncating output: Anthropic
// returned a structurally-valid-but-incomplete tool call (fields after the
// cutoff point defaulted to "") instead of erroring, and Gemini rejected the
// truncated call outright with finish_reason=MALFORMED_FUNCTION_CALL. See the
// stop/finish-reason checks below for the second half of this fix — this
// constant alone should mean that path rarely triggers in practice.
const enrichMaxTokens = 4096

// Enrich turns input into a corpusdoc.Document. input.URL is copied through
// verbatim and is never influenced by the LLM's output — the LLM only ever
// sees it for context, never as a field it can set, so a poisoned page
// can't spoof its own provenance (see docs/plans/2026-07-07-... Security).
//
// The LLM's scam_type output is validated against crawler.ValidScamTypes
// before this returns — defense in depth alongside the check corpusdoc.Parse
// does again when the reviewed file is later ingested (a mislabeled scam
// pattern is a label-evasion vector, so this is checked twice, not once).
// Title/Content/UserQueries/Prevention are also checked non-empty: a
// truncated or otherwise incomplete tool call can be syntactically valid
// JSON while missing the fields generated last (see enrichMaxTokens) — this
// catches that before a broken file ever reaches the reviewer, on any
// provider, not just the truncation case this was written for.
func (e *Enricher) Enrich(ctx context.Context, input Input) (corpusdoc.Document, error) {
	raw, err := e.llm.GenerateStructured(ctx, llm.Request{
		System:    buildSystemPrompt(),
		User:      buildUserPrompt(input),
		ToolName:  enrichToolName,
		ToolDesc:  enrichToolDesc,
		Schema:    buildToolSchema(sortedValidScamTypes()),
		MaxTokens: enrichMaxTokens,
	})
	if err != nil {
		return corpusdoc.Document{}, fmt.Errorf("enrich: generate: %w", err)
	}

	var res result
	if err := json.Unmarshal(raw, &res); err != nil {
		return corpusdoc.Document{}, fmt.Errorf("enrich: unmarshal result: %w", err)
	}
	res = normalizeEscapedNewlines(res)
	if err := validateResult(res); err != nil {
		return corpusdoc.Document{}, fmt.Errorf("enrich: %w", err)
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

// normalizeEscapedNewlines fixes a real, observed model quirk: when a field
// (almost always "content", since it's the only one asked to span multiple
// paragraphs) needs a line/paragraph break, the model sometimes writes the
// literal two characters '\' 'n' as text instead of a properly JSON-escaped
// newline — json.Unmarshal then faithfully decodes that as the literal
// string "\n" (backslash-n), not an actual newline byte. This breaks
// ingest's structure-based chunking, which splits Content on real "\n\n"
// paragraph boundaries: a document with only literal backslash-n text
// collapses into one giant paragraph instead of chunking naturally.
// Replacing the literal escape sequence with a real newline is safe — a
// genuine literal "\n" in Vietnamese scam-warning prose is not a realistic
// case to preserve.
func normalizeEscapedNewlines(res result) result {
	fix := func(s string) string {
		s = strings.ReplaceAll(s, `\r\n`, "\n")
		s = strings.ReplaceAll(s, `\n`, "\n")
		return strings.ReplaceAll(s, `\r`, "\n")
	}
	res.Title = fix(res.Title)
	res.Content = fix(res.Content)
	res.Prevention = fix(res.Prevention)
	for i, q := range res.UserQueries {
		res.UserQueries[i] = fix(q)
	}
	return res
}

// validateResult rejects a result missing any required field. json.Unmarshal
// happily accepts a syntactically valid but incomplete tool call (e.g. one
// truncated mid-generation at the token limit) — every field defaults to its
// zero value rather than erroring, so this is the only thing standing
// between a truncated/malformed response and a broken review file.
func validateResult(res result) error {
	if strings.TrimSpace(res.Title) == "" {
		return fmt.Errorf("model returned an empty title")
	}
	if strings.TrimSpace(res.Content) == "" {
		return fmt.Errorf("model returned empty content")
	}
	if !crawler.ValidScamTypes[res.ScamType] {
		return fmt.Errorf("model returned unknown scam_type %q", res.ScamType)
	}
	if len(res.UserQueries) == 0 {
		return fmt.Errorf("model returned no user_queries")
	}
	if strings.TrimSpace(res.Prevention) == "" {
		return fmt.Errorf("model returned empty prevention")
	}
	return nil
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
