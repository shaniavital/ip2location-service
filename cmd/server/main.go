// Command server runs the ip2location HTTP service: it loads configuration,
// wires the datastore, rate limiter and HTTP handlers together, and serves until
// it receives a termination signal, then shuts down gracefully.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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

// run holds the real startup logic and returns an error instead of calling
// os.Exit, which keeps the wiring testable and lets deferred cleanup run.
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
	// Opportunistic cleanup: the CSV store has nothing to close, but a future
	// network-backed store implements io.Closer, and its connection pool is
	// drained here on shutdown.
	if closer, ok := locator.(io.Closer); ok {
		defer func() {
			if cerr := closer.Close(); cerr != nil {
				logger.Error("closing datastore failed", "error", cerr)
			}
		}()
	}

	limiter := ratelimit.NewTokenBucket(cfg.RateLimitRPS, ratelimit.WithCapacity(float64(cfg.RateLimitBurst)))
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

	// Cancel ctx when an interrupt/terminate signal arrives.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ListenAndServe blocks, so run it in a goroutine and report a genuine
	// startup failure (anything other than the expected ErrServerClosed) back.
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server starting",
			"addr", cfg.ServerAddr,
			"datastore", cfg.DatastoreType,
			"rate_limit_rps", cfg.RateLimitRPS,
			"rate_limit_burst", cfg.RateLimitBurst,
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("server failed: %w", err)
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining connections")
	}

	// Give in-flight requests a bounded window to complete.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	logger.Info("server stopped cleanly")
	return nil
}
