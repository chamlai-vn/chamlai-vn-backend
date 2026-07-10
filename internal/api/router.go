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
	httpSwagger "github.com/swaggo/http-swagger/v2"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/middleware"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/problem"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/root"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/v1/analyze"

	_ "github.com/chamlai-vn/chamlai-vn-backend/internal/api/swagger" // swagger spec, registered via init()
)

// Handlers groups the per-feature handlers NewRouter mounts. Add a field
// (and a corresponding Mount call in NewRouter) for each new v1 feature —
// this keeps NewRouter's signature stable as the API grows past one
// endpoint, instead of adding one constructor parameter per handler.
type Handlers struct {
	Analyze *analyze.Handler
}

// Config configures the HTTP layer: what address to listen on (server.go)
// and the router's cross-cutting behaviour.
type Config struct {
	// Addr is the address NewServer listens on, e.g. ":8080".
	Addr string
	// AllowOrigins is the CORS allow-list. "*" allows any origin.
	AllowOrigins []string
	// BodyLimitBytes caps request body size; requests over the limit get a
	// 413 problem response instead of an unbounded read.
	BodyLimitBytes int64
	// SwaggerUI mounts GET /swagger/* when true. Meant for development only
	// — the spec isn't access-controlled.
	SwaggerUI bool
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
//
// Each feature owns its URL structure via its own Routes() sub-router (see
// analyze.Handler.Routes); NewRouter only decides where to Mount it. Adding
// a feature means adding a field to Handlers and one Mount call here, not
// growing this function's parameter list.
func NewRouter(cfg Config, h Handlers) http.Handler {
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

	r.Mount("/", root.Routes())
	r.Route("/v1", func(r chi.Router) {
		r.Mount("/analyze", h.Analyze.Routes())
	})

	if cfg.SwaggerUI {
		r.Get("/swagger/*", httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))
	}

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
