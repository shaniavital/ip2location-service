package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/shaniavital/ip2location-service/internal/geo"
	"github.com/shaniavital/ip2location-service/internal/httpapi"
)

// stubLimiter returns a fixed Allow result.
type stubLimiter struct{ allow bool }

func (s stubLimiter) Allow() bool { return s.allow }

// panicLocator panics on lookup, to exercise the recovery middleware.
type panicLocator struct{}

func (panicLocator) Find(_ context.Context, _ netip.Addr) (geo.Location, error) {
	panic("boom")
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRouter_RateLimited(t *testing.T) {
	api := httpapi.NewAPI(stubLocator{loc: geo.Location{Country: "US", City: "Mountain View"}}, discardLogger())
	router := httpapi.NewRouter(api, stubLimiter{allow: false}, discardLogger())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/find-country?ip=8.8.8.8", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if ra := rec.Header().Get("Retry-After"); ra != "1" {
		t.Errorf("Retry-After = %q, want %q", ra, "1")
	}

	var got struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body %q: %v", rec.Body.String(), err)
	}
	if got.Error == "" {
		t.Error("rate-limited response should carry a JSON error message")
	}
}

func TestRouter_RateLimitAllowsThrough(t *testing.T) {
	api := httpapi.NewAPI(stubLocator{loc: geo.Location{Country: "US", City: "Mountain View"}}, discardLogger())
	router := httpapi.NewRouter(api, stubLimiter{allow: true}, discardLogger())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/find-country?ip=8.8.8.8", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRouter_RecoversFromPanic(t *testing.T) {
	api := httpapi.NewAPI(panicLocator{}, discardLogger())
	router := httpapi.NewRouter(api, stubLimiter{allow: true}, discardLogger())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/find-country?ip=8.8.8.8", nil)

	// Must not propagate the panic out of ServeHTTP.
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	var got struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body %q: %v", rec.Body.String(), err)
	}
	if got.Error == "" {
		t.Error("recovered response should carry a JSON error message")
	}
}
