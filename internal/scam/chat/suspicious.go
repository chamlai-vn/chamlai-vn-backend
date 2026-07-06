package chat

import (
	"regexp"
	"strings"
)

// Deterministic markers that, if present in the latest user message, force the
// scam flow regardless of the router. This is the safety layer: a scam pasted
// as a "question" must never route away from detection. Starter set — tune with
// real data (see plan: looksSuspicious is a heuristic, not a classifier).
var (
	// URLs / links, including messenger + common shortener/suspicious TLDs.
	urlRE = regexp.MustCompile(`(?i)(https?://|www\.|t\.me/|bit\.ly|tinyurl|\b[a-z0-9-]+\.(top|xyz|vip|click|link|shop|online|info)\b)`)
	// A long run of digits (≥9) — bank account / card / phone patterns.
	longDigitsRE = regexp.MustCompile(`\d{9,}`)
	// A short numeric OTP-like code (4–6 digits) as a standalone token.
	otpCodeRE = regexp.MustCompile(`(?:^|\s)\d{4,6}(?:\s|$)`)

	// Money mentions and scam/lure keywords (lowercased, unaccented-tolerant via
	// substring match on the lowercased text).
	suspiciousKeywords = []string{
		"triệu", "trúng thưởng", "việc nhẹ lương cao", "nạp tiền", "chuyển khoản",
		"đặt cọc", "khóa tài khoản", "xác minh", "otp", "mã xác nhận", "mã otp",
		"hoàn tiền", "đóng băng", "vnđ", "usdt", "lãi suất", "hoa hồng",
		"số tài khoản", "stk", "quà tặng", "phí", "lệ phí",
	}
)

// looksSuspicious reports whether text contains cheap, high-signal markers of a
// scam message. A true result forces the scam flow.
func looksSuspicious(text string) bool {
	if urlRE.MatchString(text) || longDigitsRE.MatchString(text) || otpCodeRE.MatchString(text) {
		return true
	}
	lower := strings.ToLower(text)
	for _, kw := range suspiciousKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
