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
//
// @Summary      Liveness probe
// @Tags         health
// @Produce      json
// @Success      200  {object}  map[string]string
// @Router       /health [get]
func Health(w http.ResponseWriter, r *http.Request) {
	respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
