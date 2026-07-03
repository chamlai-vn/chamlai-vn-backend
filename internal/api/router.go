// Package api wires the HTTP router: the middleware stack (see
// internal/api/middleware), the unversioned root routes (internal/api/root),
// and the versioned business endpoints (internal/api/v1/...). Server
// lifecycle (timeouts, graceful shutdown) lives in server.go.
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/middleware"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/problem"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/root"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/v1/analyze"
)

// Config configures the router's cross-cutting behaviour.
type Config struct {
	// AllowOrigins is the CORS allow-list. "*" allows any origin.
	AllowOrigins []string
	// BodyLimitBytes caps request body size; requests over the limit get a
	// 413 problem response instead of an unbounded read.
	BodyLimitBytes int64
}

// NewRouter builds the HTTP router: RequestID → client-IP → RequestLogger →
// Recoverer → CORS → body-size limit, then root (unversioned) and v1 routes.
// RequestLogger wraps Recoverer deliberately, so a panic's 500 still gets
// logged with the right status (see middleware.RequestLogger's doc).
//
// Client IP uses chimw.ClientIPFromRemoteAddr — the TCP peer address, never
// a spoofable header — because this service isn't known to sit behind a
// reverse proxy yet. If one is introduced, switch to chimw.ClientIPFromXFF
// with that proxy's CIDRs (chi's plain RealIP trusts X-Forwarded-For
// unconditionally and is deprecated for exactly this reason).
func NewRouter(cfg Config, analyzeHandler *analyze.Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(
		middleware.RequestID,
		chimw.ClientIPFromRemoteAddr,
		middleware.RequestLogger,
		middleware.Recoverer,
		cors.Handler(cors.Options{
			AllowedOrigins: cfg.AllowOrigins,
			AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
			AllowedHeaders: []string{"Content-Type", middleware.RequestIDHeader},
		}),
		bodyLimit(cfg.BodyLimitBytes),
	)

	r.NotFound(problem.Handler(func(http.ResponseWriter, *http.Request) error {
		return problem.NotFound()
	}))
	r.MethodNotAllowed(problem.Handler(func(http.ResponseWriter, *http.Request) error {
		return problem.MethodNotAllowed()
	}))

	r.Get("/health", root.Health)
	r.Route("/v1", func(r chi.Router) {
		r.Post("/analyze", problem.Handler(analyzeHandler.Handle))
	})

	return r
}

// bodyLimit rejects request bodies larger than limit with a 413, surfaced
// via bind.JSON's *http.MaxBytesError handling rather than a raw connection
// reset.
func bodyLimit(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}
