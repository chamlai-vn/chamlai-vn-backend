// Package middleware holds the cross-cutting HTTP concerns every route goes
// through: request ID propagation, structured request logging, and panic
// recovery. Each file is one middleware; router.go in the parent package
// wires them into the chi stack.
package middleware

import (
	"log/slog"
	"net/http"

	ulidutil "github.com/chamlai-vn/chamlai-vn-backend/pkg/util/ulid"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/problem"
)

// RequestIDHeader is the header a client-supplied or server-generated
// request ID travels under, both ways. Exported so the router can allow it
// through CORS.
const RequestIDHeader = "X-Request-Id"

// RequestID accepts a client-supplied X-Request-Id (validated, so a client
// can't smuggle log-injection payloads) or mints a new ULID, echoes it back
// on the response, and attaches a logger pre-tagged with it to the request
// context — every log emitted downstream (including problem.Handler's error
// log) carries it for free.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := sanitizeRequestID(r.Header.Get(RequestIDHeader))
		if id == "" {
			id = ulidutil.NewString()
		}
		w.Header().Set(RequestIDHeader, id)

		logger := slog.Default().With("request_id", id)
		ctx := problem.ContextWithLogger(r.Context(), logger)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// sanitizeRequestID accepts short, printable-ASCII IDs only. A client header
// that fails this (too long, control characters, non-ASCII) is discarded in
// favor of a freshly generated ID, rather than trusted verbatim into logs.
func sanitizeRequestID(id string) string {
	if id == "" || len(id) > 64 {
		return ""
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c < 0x20 || c > 0x7e {
			return ""
		}
	}
	return id
}
