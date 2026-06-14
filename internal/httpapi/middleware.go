package httpapi

import (
	"log/slog"
	"net/http"
)

// Limiter is the rate-limiting contract the middleware depends on. It is
// declared here, at the point of use, so the limiter implementation
// (ratelimit.TokenBucket) needs no knowledge of HTTP and a per-client limiter
// could be swapped in without touching this package.
type Limiter interface {
	Allow() bool
}

// rateLimit rejects requests with 429 when the limiter denies them, using the
// shared JSON error format. Retry-After tells well-behaved clients when to retry.
func rateLimit(limiter Limiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow() {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// recoverPanic turns a panic in a downstream handler into a 500 JSON response
// instead of a dropped connection, and logs the cause. It re-panics on
// http.ErrAbortHandler, which is the documented way to abort a handler silently.
func recoverPanic(logger *slog.Logger, next http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rv := recover()
			if rv == nil {
				return
			}
			if rv == http.ErrAbortHandler {
				panic(rv) // let the server handle its own abort sentinel
			}
			logger.Error("recovered from panic", "panic", rv, "method", r.Method, "path", r.URL.Path)
			writeError(w, http.StatusInternalServerError, "internal server error")
		}()
		next.ServeHTTP(w, r)
	})
}
