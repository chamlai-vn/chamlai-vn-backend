package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter builds the HTTP router: a Logger + Recoverer middleware stack and
// the API routes. Recoverer turns a handler panic into a 500 instead of
// crashing the process. Kept flat for the two current endpoints; when API
// versioning arrives this graduates into internal/api/context (root/v1/v2).
func NewRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", h.HandleHealth)
	r.Post("/analyze", h.HandleAnalyze)

	return r
}
