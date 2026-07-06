package chat

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// maxPromptBytes caps the composed prompt to bound cost/latency.
	maxPromptBytes = 40_000
	// latestOpen/Close fence the message being routed/answered. Prompts tell the
	// model everything inside is data, not instructions.
	latestOpen  = "<tin_nhan>"
	latestClose = "</tin_nhan>"
)

// flattenHistory renders the recent conversation as a sanitized transcript. Kept
// simple (role-labelled lines) rather than a message array — the LLM only needs
// enough context to route/answer a follow-up.
func flattenHistory(history []Message) string {
	if len(history) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Lịch sử hội thoại gần đây:\n")
	for _, m := range history {
		label := "Người dùng"
		if m.Role == RoleAssistant {
			label = "Trợ lý"
		}
		fmt.Fprintf(&sb, "%s: %s\n", label, sanitizeForPrompt(m.Content))
	}
	return sb.String()
}

// composeUserPrompt builds "<transcript>\n<instruction>\n<fenced latest>" for a
// single turn, truncated to maxPromptBytes.
func composeUserPrompt(history []Message, latest, instruction string) string {
	var sb strings.Builder
	if t := flattenHistory(history); t != "" {
		sb.WriteString(t)
		sb.WriteString("\n")
	}
	sb.WriteString(instruction)
	sb.WriteString("\n")
	sb.WriteString(latestOpen + "\n")
	sb.WriteString(sanitizeForPrompt(latest))
	sb.WriteString("\n" + latestClose)
	return truncateBytes(sb.String(), maxPromptBytes)
}

// sanitizeForPrompt strips control characters and neutralises prompt-injection
// attempts, including our own fence tags so a message can't close the block.
// Mirrors analyzer.sanitizeForPrompt (kept local to avoid cross-package coupling
// on an unexported helper).
func sanitizeForPrompt(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\t' || r >= 32 {
			b.WriteRune(r)
		}
	}
	result := b.String()
	injectionPatterns := []string{
		latestOpen, latestClose,
		"<|im_start|>", "<|im_end|>", "[INST]", "[/INST]",
	}
	for _, p := range injectionPatterns {
		result = strings.ReplaceAll(result, p, "")
	}
	return result
}

// truncateBytes trims s to at most n bytes without splitting a UTF-8 rune.
func truncateBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + "..."
}
