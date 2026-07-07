package crawler

import "strings"

// Scam type labels. Snake_case, fixed set — keep these in sync with whatever the
// analyzer/UI expects to display, and with the scam_type values used in the
// hand-written local markdown files (cmd/crawler/data/*.md). "other" is the
// catch-all when no rule matches (typical for broad overview/guide articles).
const (
	ScamImpersonationAuthority = "impersonation_authority" // công an, toà án, viện KS, cơ quan nhà nước, VNeID/CCCD
	ScamImpersonationService   = "impersonation_service"   // điện lực, y tế, nhà mạng/SIM, nhân viên ngân hàng
	ScamTechFraud              = "tech_fraud"              // deepfake, AI, mã độc/malware, hack tài khoản, app giả
	ScamRecovery               = "recovery_scam"           // dịch vụ "hỗ trợ lấy lại tiền bị lừa"
	ScamInvestmentFraud        = "investment_fraud"        // forex, crypto, chứng khoán, Ponzi, làm giàu
	ScamLoan                   = "loan_scam"               // vay online, tín dụng đen, mở thẻ tín dụng
	ScamFakeJob                = "fake_job"                // việc nhẹ lương cao, CTV chốt đơn, nhiệm vụ, XKLĐ
	ScamEcommerce              = "ecommerce_scam"          // bán hàng/đặt cọc, tour du lịch, vé máy bay giá rẻ
	ScamPackageDelivery        = "package_delivery"        // giả shipper/giao hàng, bưu kiện
	ScamPrizeGift              = "prize_gift"              // trúng thưởng, quà tri ân miễn phí
	ScamRomance                = "romance_scam"            // tình cảm, hẹn hò qua mạng
	ScamVacationContract       = "vacation_contract"       // hợp đồng/sở hữu kỳ nghỉ, timeshare
	ScamOther                  = "other"
)

// ValidScamTypes is the fixed set of allowed scam_type values, generated from
// the constants above. corpusdoc.Parse and internal/scam/enrich validate a
// document's declared/LLM-emitted scam_type against this set so a
// mislabeled or hallucinated type is rejected rather than silently stored —
// a wrong label is a retrieval-evasion vector for a poisoned document.
var ValidScamTypes = map[string]bool{
	ScamImpersonationAuthority: true,
	ScamImpersonationService:   true,
	ScamTechFraud:              true,
	ScamRecovery:               true,
	ScamInvestmentFraud:        true,
	ScamLoan:                   true,
	ScamFakeJob:                true,
	ScamEcommerce:              true,
	ScamPackageDelivery:        true,
	ScamPrizeGift:              true,
	ScamRomance:                true,
	ScamVacationContract:       true,
	ScamOther:                  true,
}

// InferScamType labels an article by keyword-matching its title and content
// against known Vietnamese scam patterns. It is intentionally rule-based (no
// LLM): cheap, deterministic, and good enough to seed the corpus. Returns
// ScamOther when nothing matches.
//
// Order is priority order: the FIRST matching case wins, so more specific /
// higher-confidence patterns are listed before broader ones. Broad overview
// articles that mention many scam types will be labelled by whichever keyword
// appears first here — that's an accepted limitation of rule-based labelling;
// hand-written markdown should set scam_type explicitly in its frontmatter.
func InferScamType(title, content string) string {
	text := strings.ToLower(title + " " + content)
	switch {
	case containsAny(text, "giả mạo công an", "giả danh công an", "công an", "cảnh sát",
		"viện kiểm sát", "tòa án", "bộ tài chính", "bộ công an", "cơ quan thuế", "cán bộ thuế",
		"vneid", "định danh điện tử", "căn cước", "cccd"):
		return ScamImpersonationAuthority
	case containsAny(text, "điện lực", "nhân viên y tế", "bệnh viện", "cấp cứu",
		"nâng cấp sim", "nhà mạng", "nhân viên ngân hàng", "giả danh ngân hàng"):
		return ScamImpersonationService
	case containsAny(text, "deepfake", "trí tuệ nhân tạo", "công nghệ cao", "mã độc", "malware",
		"phần mềm độc hại", "phần mềm gián điệp", "chiếm đoạt tài khoản", "hack tài khoản",
		"hack facebook", "hack zalo", "ứng dụng giả mạo", "app giả mạo", "trạm bts", "bts giả"):
		return ScamTechFraud
	case containsAny(text, "lấy lại tiền", "thu hồi tiền", "lấy lại tiền bị lừa", "hỗ trợ lấy lại"):
		return ScamRecovery
	case containsAny(text, "đầu tư", "forex", "crypto", "tiền ảo", "chứng khoán", "sàn giao dịch",
		"lợi nhuận cao", "ponzi", "lãi suất cao", "làm giàu"):
		return ScamInvestmentFraud
	case containsAny(text, "vay tiền", "vay online", "tín dụng đen", "vay nặng lãi", "cho vay",
		"app vay", "mở thẻ tín dụng"):
		return ScamLoan
	case containsAny(text, "việc nhẹ lương cao", "cộng tác viên online", "cộng tác viên",
		"nhấn like", "chốt đơn", "làm nhiệm vụ", "nhiệm vụ tiktok", "xuất khẩu lao động"):
		return ScamFakeJob
	case containsAny(text, "đặt cọc", "bán hàng online", "vé máy bay", "tour du lịch",
		"combo du lịch", "mua hàng online", "đặt phòng"):
		return ScamEcommerce
	case containsAny(text, "shipper", "giao hàng", "bưu kiện", "bưu phẩm"):
		return ScamPackageDelivery
	case containsAny(text, "trúng thưởng", "tặng quà miễn phí", "quà tri ân", "tri ân", "nhận thưởng"):
		return ScamPrizeGift
	case containsAny(text, "tình cảm", "hẹn hò", "kết bạn", "người nước ngoài", "gửi quà"):
		return ScamRomance
	case containsAny(text, "hợp đồng kỳ nghỉ", "resort", "timeshare", "sở hữu kỳ nghỉ"):
		return ScamVacationContract
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
