package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/problem"
)

// Recoverer turns a handler panic into a logged 500 problem+json response
// instead of crashing the process. Unlike chi's own Recoverer (which writes
// plain text), this goes through problem.Write so panics stay consistent
// with every other error response.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rvr := recover()
			if rvr == nil {
				return
			}
			if rvr == http.ErrAbortHandler { //nolint:errorlint // sentinel is compared by value upstream too
				// The client disconnected mid-response; net/http expects
				// this to propagate, not be swallowed.
				panic(rvr)
			}

			err := fmt.Errorf("panic: %v\n%s", rvr, debug.Stack())
			problem.LoggerFromContext(r.Context()).Error("panic recovered", "error", err)
			problem.Write(w, problem.Internal().WithErr(err))
		}()

		next.ServeHTTP(w, r)
	})
}
