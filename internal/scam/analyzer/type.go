package analyzer

import "encoding/json"

// Risk levels the analyzer can return.
const (
	RiskRed    = "red"    // clear scam signals
	RiskYellow = "yellow" // some suspicious signals, not conclusive
	RiskGreen  = "green"  // no significant scam signals
)

// AnalysisResult is the scam-scoring verdict for a suspicious message. Disclaimer
// is set server-side in Score (never trusted from the model); Sources is also
// assembled server-side in Score (see matchSources in score.go), never from
// the model directly.
type AnalysisResult struct {
	RiskLevel          string   `json:"risk_level"` // "red" | "yellow" | "green"
	RedFlags           []string `json:"red_flags"`
	MatchedPatterns    []string `json:"matched_patterns"`
	Sources            []Source `json:"sources"` // matched source documents backing the verdict; [] when nothing matched
	RecommendedActions []string `json:"recommended_actions"`
	Disclaimer         string   `json:"disclaimer"`
}

// Source is one document backing the verdict — a matched, published scam
// warning the user can click through to. It is assembled server-side by
// mapping the model's matched_source_indices (1-based positions into the
// reference block it was shown) back to the retrieved documents; the LLM tool
// schema is deliberately not asked for URLs or document IDs (see
// AnalysisToolSchema below).
type Source struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// The forced tool the model must call. Disclaimer is intentionally NOT in the
// schema — it is mandatory and constant, so Score sets it server-side rather
// than spending tokens (and risking the model omitting it).
//
// Exported (rather than the more common unexported-const pattern in this
// package) because cmd/benchmark's generic-AI arm needs this exact schema to
// structure its own answer into a comparable AnalysisResult — duplicating it
// there would risk silent drift from what Score actually asks the model for.
const (
	AnalysisToolName = "record_scam_analysis"
	AnalysisToolDesc = "Ghi lại kết quả phân tích rủi ro lừa đảo của tin nhắn đáng ngờ dưới dạng dữ liệu có cấu trúc."
)

// AnalysisToolSchema is the JSON Schema for the tool input. Descriptions are in
// Vietnamese to steer the model toward Vietnamese, user-facing content.
var AnalysisToolSchema = json.RawMessage(`{
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
    "matched_source_indices": {
      "type": "array",
      "items": {"type": "integer"},
      "description": "Số thứ tự [n] của các mẫu tham chiếu mà bạn THỰC SỰ dựa vào để đưa ra kết luận (khớp với đánh số [1], [2], ... trong khối tham chiếu; ví dụ dùng mẫu [1] và [3] thì trả về [1, 3]). Chỉ dùng số có trong danh sách tham chiếu được cung cấp. Để mảng rỗng nếu không dựa vào mẫu tham chiếu nào."
    },
    "recommended_actions": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Các hành động nên làm tiếp theo cho người dùng (tiếng Việt)."
    }
  },
  "required": ["risk_level", "red_flags", "matched_patterns", "matched_source_indices", "recommended_actions"]
}`)
