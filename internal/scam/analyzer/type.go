package analyzer

import "encoding/json"

// Risk levels the analyzer can return.
const (
	RiskRed    = "red"    // clear scam signals
	RiskYellow = "yellow" // some suspicious signals, not conclusive
	RiskGreen  = "green"  // no significant scam signals
)

// AnalysisResult is the scam-scoring verdict for a suspicious message. Disclaimer
// is set server-side in Score (never trusted from the model).
type AnalysisResult struct {
	RiskLevel          string   `json:"risk_level"` // "red" | "yellow" | "green"
	RedFlags           []string `json:"red_flags"`
	MatchedPatterns    []string `json:"matched_patterns"`
	RecommendedActions []string `json:"recommended_actions"`
	Disclaimer         string   `json:"disclaimer"`
}

// The forced tool the model must call. Disclaimer is intentionally NOT in the
// schema — it is mandatory and constant, so Score sets it server-side rather
// than spending tokens (and risking the model omitting it).
const (
	analysisToolName = "record_scam_analysis"
	analysisToolDesc = "Ghi lại kết quả phân tích rủi ro lừa đảo của tin nhắn đáng ngờ dưới dạng dữ liệu có cấu trúc."
)

// analysisToolSchema is the JSON Schema for the tool input. Descriptions are in
// Vietnamese to steer the model toward Vietnamese, user-facing content.
var analysisToolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "risk_level": {
      "type": "string",
      "enum": ["red", "yellow", "green"],
      "description": "Mức độ rủi ro lừa đảo: red = dấu hiệu lừa đảo rõ ràng; yellow = có dấu hiệu đáng ngờ nhưng chưa chắc chắn; green = không có dấu hiệu lừa đảo đáng kể."
    },
    "red_flags": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Danh sách các dấu hiệu cảnh báo cụ thể tìm thấy trong tin nhắn (tiếng Việt). Để mảng rỗng nếu không có."
    },
    "matched_patterns": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Tên các chiêu trò lừa đảo đã biết khớp với tin nhắn này, chỉ chọn từ các mẫu tham chiếu được cung cấp. Để mảng rỗng nếu không có mẫu nào khớp."
    },
    "recommended_actions": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Các hành động nên làm tiếp theo cho người dùng (tiếng Việt)."
    }
  },
  "required": ["risk_level", "red_flags", "matched_patterns", "recommended_actions"]
}`)
