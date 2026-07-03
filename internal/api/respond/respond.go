// Package respond writes successful JSON responses. It is the counterpart to
// package problem, which handles the error path.
package respond

import (
	"encoding/json"
	"net/http"
)

// JSON serialises v as JSON with the given status. The header is written
// before the body, so a well-formed value always produces a valid response.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
