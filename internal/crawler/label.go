package crawler

import "strings"

// Scam type labels. Snake_case, fixed set — keep these in sync with whatever the
// analyzer/UI expects to display. "other" is the catch-all when no rule matches.
// Add new categories here (e.g. package_delivery, otp_phishing) as the corpus
// grows; order in InferScamType is priority order when keywords overlap.
const (
	ScamVacationContract       = "vacation_contract"
	ScamFakeJob                = "fake_job"
	ScamImpersonationAuthority = "impersonation_authority"
	ScamInvestmentFraud        = "investment_fraud"
	ScamRomance                = "romance_scam"
	ScamOther                  = "other"
)

// InferScamType labels an article by keyword-matching its title and content
// against known Vietnamese scam patterns. It is intentionally rule-based (no
// LLM): cheap, deterministic, and good enough to seed the corpus. Returns
// ScamOther when nothing matches. First matching rule wins.
func InferScamType(title, content string) string {
	text := strings.ToLower(title + " " + content)
	switch {
	case containsAny(text, "hợp đồng kỳ nghỉ", "resort", "timeshare", "sở hữu kỳ nghỉ"):
		return ScamVacationContract
	case containsAny(text, "việc nhẹ lương cao", "cộng tác viên online", "cộng tác viên", "nhấn like", "chốt đơn"):
		return ScamFakeJob
	case containsAny(text, "giả mạo công an", "giả danh công an", "công an", "cảnh sát", "viện kiểm sát", "tòa án"):
		return ScamImpersonationAuthority
	case containsAny(text, "đầu tư", "forex", "crypto", "tiền ảo", "sàn giao dịch", "lợi nhuận cao"):
		return ScamInvestmentFraud
	case containsAny(text, "tình cảm", "hẹn hò", "kết bạn", "người nước ngoài", "gửi quà"):
		return ScamRomance
	default:
		return ScamOther
	}
}

// containsAny reports whether text contains any of subs. text is expected to be
// already lower-cased by the caller.
func containsAny(text string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(text, sub) {
			return true
		}
	}
	return false
}
