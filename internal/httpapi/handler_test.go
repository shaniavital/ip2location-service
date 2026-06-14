package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/shaniavital/ip2location-service/internal/geo"
	"github.com/shaniavital/ip2location-service/internal/httpapi"
)

// stubLocator is a configurable geo.Locator for handler tests.
type stubLocator struct {
	loc geo.Location
	err error
}

func (s stubLocator) Find(_ context.Context, _ netip.Addr) (geo.Location, error) {
	return s.loc, s.err
}

func newTestAPI(loc stubLocator) http.Handler {
	// Discard logs so test output stays clean. An allow-all limiter lets these
	// handler-focused tests exercise the real router without rate-limit noise.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return httpapi.NewRouter(httpapi.NewAPI(loc, logger), stubLimiter{allow: true}, logger)
}

func TestFindCountry_Success(t *testing.T) {
	api := newTestAPI(stubLocator{loc: geo.Location{Country: "US", City: "Mountain View"}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/find-country?ip=8.8.8.8", nil)
	api.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want JSON", ct)
	}

	var got struct {
		Country string `json:"country"`
		City    string `json:"city"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body %q: %v", rec.Body.String(), err)
	}
	if got.Country != "US" || got.City != "Mountain View" {
		t.Errorf("body = %+v, want {US Mountain View}", got)
	}
}

func TestFindCountry_Errors(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		stub       stubLocator
		wantStatus int
	}{
		{
			name:       "missing ip param",
			target:     "/v1/find-country",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty ip param",
			target:     "/v1/find-country?ip=",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid ip",
			target:     "/v1/find-country?ip=not-an-ip",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "ip not found",
			target:     "/v1/find-country?ip=8.8.8.8",
			stub:       stubLocator{err: geo.ErrNotFound},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "datastore error",
			target:     "/v1/find-country?ip=8.8.8.8",
			stub:       stubLocator{err: errors.New("boom")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newTestAPI(tt.stub)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tt.target, nil)
			api.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			// Every error must carry a non-empty JSON {"error":"..."} body.
			var got struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decoding error body %q: %v", rec.Body.String(), err)
			}
			if got.Error == "" {
				t.Errorf("error body is empty, want a message")
			}
		})
	}
}

func TestHealthz(t *testing.T) {
	api := newTestAPI(stubLocator{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	api.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

func TestFindCountry_MethodNotAllowed(t *testing.T) {
	api := newTestAPI(stubLocator{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/find-country?ip=8.8.8.8", nil)
	api.ServeHTTP(rec, req)

	// Router-level errors still use the API's JSON error contract.
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if allow := rec.Header().Get("Allow"); allow != "GET, HEAD" {
		t.Errorf("Allow = %q, want %q", allow, "GET, HEAD")
	}
	requireJSONError(t, rec)
}

func TestRouter_NotFoundReturnsJSON(t *testing.T) {
	api := newTestAPI(stubLocator{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	api.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	requireJSONError(t, rec)
}

func requireJSONError(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want JSON", ct)
	}
	var got struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding error body %q: %v", rec.Body.String(), err)
	}
	if got.Error == "" {
		t.Fatal("error body is empty, want a message")
	}
}
