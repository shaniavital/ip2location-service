// Package httpapi contains the HTTP layer: the request handlers, the JSON
// response helpers, the middleware (rate limiting, panic recovery, request
// logging), and the router that wires them together.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// errorResponse is the body returned for every error: {"error":"..."}.
// Defining it once here keeps the error wire-format consistent across the
// handler and all middleware (this is the single source of truth the rate-limit
// and panic-recovery middleware reuse).
type errorResponse struct {
	Error string `json:"error"`
}

// writeJSON encodes v as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Status and headers are already on the wire, so the response can't be
		// salvaged (this only happens if the client went away mid-write). Log
		// and move on rather than pretend to write a different status.
		slog.Error("encoding JSON response failed", "error", err)
	}
}

// writeError writes a JSON error body with the given status code.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
