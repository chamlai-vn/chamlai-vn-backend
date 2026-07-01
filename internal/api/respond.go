package api

import (
	"encoding/json"
	"net/http"
)

// writeJSON serialises v as JSON with the given status. The header is written
// before the body, so a well-formed value always produces a valid response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes an errorResponse envelope with the given status.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
