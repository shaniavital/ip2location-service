// Command server runs the ip2location HTTP service: it loads configuration,
// wires the datastore, rate limiter and HTTP handlers together, and serves.
package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/shaniavital/ip2location-service/internal/config"
	"github.com/shaniavital/ip2location-service/internal/geo"
	"github.com/shaniavital/ip2location-service/internal/httpapi"
	"github.com/shaniavital/ip2location-service/internal/ratelimit"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

// run holds the startup logic and returns an error instead of calling os.Exit,
// which keeps the wiring in one readable place.
func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger) // package-level helpers (e.g. the JSON writer) log here too

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	locator, err := geo.New(cfg.DatastoreType, cfg.DatastoreDSN)
	if err != nil {
		return fmt.Errorf("initializing datastore: %w", err)
	}

	limiter := ratelimit.NewTokenBucket(cfg.RateLimitRPS)
	api := httpapi.NewAPI(locator, logger)
	router := httpapi.NewRouter(api, limiter, logger)

	srv := &http.Server{
		Addr:    cfg.ServerAddr,
		Handler: router,
		// Explicit timeouts: the net/http defaults are none, which leaves the
		// server open to slow-client (Slowloris) connection exhaustion.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	logger.Info("server starting",
		"addr", cfg.ServerAddr,
		"datastore", cfg.DatastoreType,
		"rate_limit_rps", cfg.RateLimitRPS,
	)
	return srv.ListenAndServe()
}
