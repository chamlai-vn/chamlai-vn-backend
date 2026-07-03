// Package root holds unversioned routes — currently just the liveness probe.
// Versioned business endpoints live under internal/api/v1/...
package root

import (
	"net/http"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/respond"
)

// Health is a liveness probe. It makes no DB or LLM calls, so it stays green
// even when downstream dependencies are down — it only proves the server
// itself is up.
func Health(w http.ResponseWriter, r *http.Request) {
	respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
