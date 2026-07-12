// Package problem implements HTTP error responses as RFC 9457 Problem
// Details (application/problem+json). It is the single place errors are
// translated into JSON: handlers return an error (or a *Problem) and never
// write an error body themselves.
package problem

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
)

// contentType is the RFC 9457 media type for a problem response.
const contentType = "application/problem+json; charset=utf-8"

// Problem is the RFC 9457 response body. Type is left as "about:blank" —
// there is no per-error-kind documentation to link to yet; Title then carries
// the standard HTTP status text as RFC 9457 requires for "about:blank".
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`

	// err is the underlying internal error, if any. Never serialised — it is
	// only for the logger (see Handler) to record what actually went wrong.
	err error
}

// Error satisfies the error interface so handlers can `return problem.BadRequest(...)`.
func (p *Problem) Error() string {
	if p.Detail != "" {
		return p.Detail
	}
	return p.Title
}

// WithErr attaches the underlying internal error for logging. It does not
// appear in the JSON response.
func (p *Problem) WithErr(err error) *Problem {
	p.err = err
	return p
}

func newProblem(status int, detail string) *Problem {
	return &Problem{
		Type:   "about:blank",
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
	}
}

// BadRequest is a 400: malformed or invalid request body/params. detail is
// user-facing Vietnamese.
func BadRequest(detail string) *Problem { return newProblem(http.StatusBadRequest, detail) }

// NotFound is a 404, used for unmatched routes.
func NotFound() *Problem {
	return newProblem(http.StatusNotFound, "không tìm thấy tài nguyên")
}

// MethodNotAllowed is a 405, used when the route exists but the method doesn't.
func MethodNotAllowed() *Problem {
	return newProblem(http.StatusMethodNotAllowed, "phương thức không được hỗ trợ")
}

// TooLarge is a 413: request body exceeded the configured limit.
func TooLarge() *Problem {
	return newProblem(http.StatusRequestEntityTooLarge, "nội dung yêu cầu quá lớn")
}

// TooManyRequests is a 429: the caller exceeded a rate limit or the service's
// paid-pipeline budget was exhausted. detail is user-facing Vietnamese and
// should stay generic — it must not let a caller distinguish "you personally
// are throttled" from "the daily global budget is exhausted", which would
// hand an attacker a success oracle for a budget-exhaustion attack.
func TooManyRequests(detail string) *Problem {
	return newProblem(http.StatusTooManyRequests, detail)
}

// Unavailable is a 503: a required collaborator (e.g. the budget store) could
// not be reached, so the request was rejected rather than risk running an
// unbudgeted paid call. Distinguishing this from a generic 500 lets
// operators alert on "budget store unreachable" separately from other
// internal errors.
func Unavailable() *Problem {
	return newProblem(http.StatusServiceUnavailable, "hệ thống tạm thời không khả dụng, vui lòng thử lại sau")
}

// Internal is a 500. detail is a fixed, coarse Vietnamese message — the real
// cause never reaches the client. Attach the real error with WithErr so the
// Handler adapter can log it.
func Internal() *Problem {
	return newProblem(http.StatusInternalServerError, "đã có lỗi xảy ra, vui lòng thử lại sau")
}

// Write serialises p as an application/problem+json response. The header is
// written before the body, so a well-formed Problem always produces a valid
// response.
func Write(w http.ResponseWriter, p *Problem) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// HandlerFunc is an HTTP handler that reports failure by returning an error,
// instead of writing an error response itself.
type HandlerFunc func(http.ResponseWriter, *http.Request) error

// Handler adapts a HandlerFunc into an http.HandlerFunc: nil error means the
// handler already wrote a successful response; a *Problem is written as-is;
// any other error is logged (with the request's logger, so it carries
// request_id) and reported to the client as a generic 500 — internal detail
// never leaks.
func Handler(fn HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := fn(w, r)
		if err == nil {
			return
		}

		p, ok := err.(*Problem)
		if !ok {
			p = Internal().WithErr(err)
		}

		if p.Status >= http.StatusInternalServerError {
			logErr := p.err
			if logErr == nil {
				logErr = p
			}
			LoggerFromContext(r.Context()).Error("request failed", "error", logErr, "status", p.Status)
		}

		Write(w, p)
	}
}

// loggerCtxKey is unexported so only this package's helpers can set/read it —
// the middleware package installs a logger via ContextWithLogger, Handler
// reads it back via LoggerFromContext. Living here (rather than in a
// dedicated context package) avoids an import cycle: middleware needs
// problem for Write/Internal already, and Handler needs a logger.
type loggerCtxKey struct{}

// ContextWithLogger returns a copy of ctx carrying l as the request-scoped
// logger.
func ContextWithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerCtxKey{}, l)
}

// LoggerFromContext returns the logger installed by ContextWithLogger, or
// slog.Default() if none was installed (e.g. in tests that call handlers
// directly without the middleware stack).
func LoggerFromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerCtxKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}
