package api

import (
	"context"
	"net/http"
	"time"
)

const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	// writeTimeout is deliberately generous: /v1/analyze waits on the LLM,
	// which can take tens of seconds worst-case. A tighter timeout would
	// truncate a legitimate in-flight response instead of a stuck one.
	writeTimeout = 120 * time.Second
	idleTimeout  = 120 * time.Second

	shutdownTimeout = 10 * time.Second
)

// NewServer wraps h in an *http.Server with production timeouts. cfg.Addr is
// the only field NewServer reads — AllowOrigins/BodyLimitBytes are for
// NewRouter.
func NewServer(cfg Config, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              cfg.Addr,
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// Run starts srv and blocks until ctx is cancelled, then drains in-flight
// requests (up to shutdownTimeout) before returning. Callers cancel ctx on
// SIGINT/SIGTERM (see cmd/api). A non-nil error other than
// http.ErrServerClosed means the server failed to start or stopped
// unexpectedly.
func Run(ctx context.Context, srv *http.Server) error {
	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
