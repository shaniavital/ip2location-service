package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"net/netip"

	"github.com/shaniavital/ip2location-service/internal/geo"
)

// API holds the dependencies for the HTTP handlers.
type API struct {
	locator geo.Locator
	logger  *slog.Logger
}

// NewAPI builds an API backed by the given locator. A nil logger falls back to
// the slog default so the handlers never panic on a missing dependency.
func NewAPI(locator geo.Locator, logger *slog.Logger) *API {
	if logger == nil {
		logger = slog.Default()
	}
	return &API{locator: locator, logger: logger}
}

// findCountryResponse is the success body for /v1/find-country.
type findCountryResponse struct {
	Country string `json:"country"`
	City    string `json:"city"`
}

// findCountry resolves the ?ip= query parameter to a location.
//
//	200 -> {"country":"..","city":".."}
//	400 -> missing or unparseable ip
//	404 -> ip is valid but not in the datastore
//	500 -> unexpected datastore error
func (a *API) findCountry(w http.ResponseWriter, r *http.Request) {
	rawIP := r.URL.Query().Get("ip")
	if rawIP == "" {
		writeError(w, http.StatusBadRequest, "missing required query parameter: ip")
		return
	}

	ip, err := netip.ParseAddr(rawIP)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid ip address")
		return
	}

	loc, err := a.locator.Find(r.Context(), ip)
	switch {
	case errors.Is(err, geo.ErrNotFound):
		writeError(w, http.StatusNotFound, "no location found for the given ip")
		return
	case err != nil:
		// Log the detail server-side; return a generic message so we don't leak
		// internals to the client.
		a.logger.Error("locator lookup failed", "ip", ip.String(), "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, findCountryResponse{Country: loc.Country, City: loc.City})
}

// healthz is a liveness probe.
func (a *API) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
