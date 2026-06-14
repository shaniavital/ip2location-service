package httpapi

import (
	"log/slog"
	"net/http"
)

// NewRouter builds the routes and wraps them in the middleware chain, returning
// the handler to serve.
//
// The rate limiter is applied per-route to the API endpoint only: /healthz is
// deliberately left unlimited so that a load balancer's health probe is never
// rejected under load (which would wrongly pull a busy-but-healthy instance out
// of rotation). recoverPanic and requestLog wrap every route — so health checks
// are still logged, and the access log records rate-limited 429s. Method-aware
// patterns (Go 1.22+) yield 405/404 automatically for the wrong method or path.
func NewRouter(api *API, limiter Limiter, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /v1/find-country", rateLimit(limiter, http.HandlerFunc(api.findCountry)))
	mux.Handle("GET /healthz", http.HandlerFunc(api.healthz))

	var h http.Handler = mux
	h = requestLog(logger, h)
	h = recoverPanic(logger, h)
	return h
}
