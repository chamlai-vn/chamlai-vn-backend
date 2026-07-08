package enrich

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Input is the raw crawled material Enrich turns into a corpusdoc.Document.
type Input struct {
	// URL is the crawler's own fetched URL. It is never overwritten by the
	// LLM's output — see Enrich's doc comment for why URL provenance matters.
	URL string
	// Title/Content are the raw crawled fields; the LLM may return a
	// cleaner/clearer title and lightly cleaned content.
	Title   string
	Content string
	// SuggestedScamType is a hint (e.g. from crawler.InferScamType), not
	// authoritative — the LLM classifies from the full content and its own
	// scam_type output is what gets validated and used.
	SuggestedScamType string
}

// result is the raw shape of the forced tool call — internal to this
// package. Enrich maps it (after validating ScamType) onto corpusdoc.Document.
type result struct {
	Title       string   `json:"title"`
	Content     string   `json:"content"`
	ScamType    string   `json:"scam_type"`
	UserQueries []string `json:"user_queries"`
	Prevention  string   `json:"prevention"`
}

const (
	enrichToolName = "record_corpus_document"
	enrichToolDesc = "Ghi lại tài liệu cảnh báo lừa đảo đã được chuẩn hoá dưới dạng dữ liệu có cấu trúc."
)

// enrichToolSchemaTemplate is the JSON Schema for the tool input. Its one
// %s is the sorted list of valid scam_type values, filled in at request time
// by buildToolSchema — that keeps the enum in sync with
// crawler.ValidScamTypes without duplicating it as a second hardcoded list.
const enrichToolSchemaTemplate = `{
  "type": "object",
  "properties": {
    "title": {
      "type": "string",
      "description": "Tóm tắt ngắn gọn thủ đoạn lừa đảo (tiếng Việt), dùng làm tiêu đề tài liệu."
    },
    "content": {
      "type": "string",
      "description": "Nội dung cảnh báo đã được làm sạch/tổng hợp từ nguồn gốc, giữ nguyên các chi tiết quan trọng (thủ đoạn, dấu hiệu nhận biết). Không thêm thông tin bịa đặt."
    },
    "scam_type": {
      "type": "string",
      "enum": [%s],
      "description": "Loại lừa đảo phù hợp nhất với nội dung, chọn đúng một trong các giá trị liệt kê."
    },
    "user_queries": {
      "type": "array",
      "items": {"type": "string"},
      "minItems": 3,
      "maxItems": 6,
      "description": "3-6 câu hỏi/tin nhắn khác nhau (mỗi câu một khía cạnh/tình huống khác nhau của thủ đoạn này, không lặp lại ý) mà một nạn nhân thật có thể hỏi hoặc gửi. Đa số viết theo giọng nạn nhân đang hỏi ('có phải lừa đảo không', 'tôi nên làm gì'); một hoặc hai câu có thể là nguyên văn tin nhắn/cuộc gọi mà kẻ lừa đảo có thể gửi (không phải câu hỏi), vì nạn nhân thường dán lại nguyên văn thay vì diễn giải."
    },
    "prevention": {
      "type": "string",
      "description": "Hướng dẫn phòng tránh cụ thể, ngắn gọn (tiếng Việt) cho loại lừa đảo này."
    }
  },
  "required": ["title", "content", "scam_type", "user_queries", "prevention"]
}`

// buildToolSchema fills enrichToolSchemaTemplate's scam_type enum from
// validTypes (sorted for deterministic prompts/tests).
func buildToolSchema(validTypes []string) json.RawMessage {
	quoted := make([]string, len(validTypes))
	for i, t := range validTypes {
		b, _ := json.Marshal(t)
		quoted[i] = string(b)
	}
	return json.RawMessage(fmt.Sprintf(enrichToolSchemaTemplate, strings.Join(quoted, ", ")))
}
