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
// of rotation). A catch-all handler keeps router-level 404/405 errors in the
// same JSON error shape as the API handlers. requestLog wraps recoverPanic so
// recovered panics are logged with the final status.
func NewRouter(api *API, limiter Limiter, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /v1/find-country", rateLimit(limiter, http.HandlerFunc(api.findCountry)))
	mux.Handle("GET /healthz", http.HandlerFunc(api.healthz))
	mux.HandleFunc("/", routeError)

	var h http.Handler = mux
	h = recoverPanic(logger, h)
	h = requestLog(logger, h)
	return h
}

func routeError(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/v1/find-country", "/healthz":
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}
