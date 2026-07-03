package middleware

import (
	"log/slog"
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/problem"
)

// RequestLogger logs one structured line per request: method, path, status,
// response size, latency, and (via the logger installed by RequestID)
// request_id. Must run after RequestID (needs the context logger) and after
// the client-IP middleware (so chimw.GetClientIP has something to return),
// and should wrap Recoverer so a panic's 500 still gets logged with the
// right status — see router.go for the actual ordering.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}

		logger := problem.LoggerFromContext(r.Context())
		logger.LogAttrs(r.Context(), levelForStatus(status), "http_request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", status),
			slog.Int("bytes", ww.BytesWritten()),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			slog.String("ip", clientIP(r)),
		)
	})
}

// clientIP reads the IP set by chimw.ClientIPFromRemoteAddr (or whichever
// chimw.ClientIPFrom* the router installed), falling back to the raw
// RemoteAddr if none was set — e.g. in tests that call this middleware
// directly without the client-IP middleware in front of it.
func clientIP(r *http.Request) string {
	if ip := chimw.GetClientIP(r.Context()); ip != "" {
		return ip
	}
	return r.RemoteAddr
}

func levelForStatus(status int) slog.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return slog.LevelError
	case status >= http.StatusBadRequest:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}
