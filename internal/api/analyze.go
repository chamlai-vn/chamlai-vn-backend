package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

// HandleAnalyze scores a suspicious message. The pipeline is two steps — the
// analyzer does NOT retrieve — mirroring cmd/seed: decode → validate →
// retriever.Retrieve → analyzer.Score. The verdict is analyzer.AnalysisResult,
// returned as-is: its JSON tags already are the public response shape, so no
// separate response DTO.
func (h *Handler) HandleAnalyze(w http.ResponseWriter, r *http.Request) {
	var req AnalyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "body JSON không hợp lệ")
		return
	}

	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeError(w, http.StatusBadRequest, "text không được rỗng")
		return
	}

	ctx := r.Context()
	chunks, err := h.retriever.Retrieve(ctx, text, h.topK)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lỗi truy hồi mẫu lừa đảo")
		return
	}

	result, err := h.scorer.Score(ctx, text, chunks)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lỗi phân tích")
		return
	}

	writeJSON(w, http.StatusOK, result)
}
