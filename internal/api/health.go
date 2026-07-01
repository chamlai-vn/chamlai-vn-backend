package api

import "net/http"

// HandleHealth is a liveness probe. It makes no DB or LLM calls, so it stays
// green even when downstream dependencies are down — it only proves the server
// itself is up.
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
