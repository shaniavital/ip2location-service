// Package config loads and validates the service configuration from
// environment variables (12-factor style), applying defaults and failing fast
// when a required value is missing or invalid.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	defaultServerAddr      = ":8080"
	defaultDatastoreType   = "csv"
	defaultShutdownTimeout = 10 * time.Second
)

// Config holds the fully-resolved service configuration. All fields are
// comparable, which keeps tests simple (a plain == against the expected value).
type Config struct {
	ServerAddr      string        // listen address, e.g. ":8080"
	DatastoreType   string        // selects the Locator implementation (the "driver")
	DatastoreDSN    string        // opaque, driver-specific source string (a path for csv, a connection URL for a DB)
	RateLimitRPS    float64       // global rate limit in requests/sec; must be > 0
	ShutdownTimeout time.Duration // grace period for draining in-flight requests
}

// Load reads configuration from the environment, applies defaults, and
// validates the result. It aggregates every problem it finds into a single
// error so the operator sees all misconfiguration at once instead of fixing
// them one boot at a time.
//
// Datastore-specific validation (e.g. that the path exists) is deliberately
// left to the datastore implementation, which knows its own requirements.
func Load() (Config, error) {
	cfg := Config{
		ServerAddr:      getEnv("SERVER_ADDR", defaultServerAddr),
		DatastoreType:   getEnv("DATASTORE_TYPE", defaultDatastoreType),
		DatastoreDSN:    getEnv("DATASTORE_DSN", ""),
		ShutdownTimeout: defaultShutdownTimeout,
	}

	var errs []error

	// RATE_LIMIT_RPS is required and must be positive. We model it as a float so
	// the limiter stays fully general (e.g. 0.5 == one request every two seconds).
	rps, err := requireFloat("RATE_LIMIT_RPS")
	switch {
	case err != nil:
		errs = append(errs, err)
	case rps <= 0:
		errs = append(errs, fmt.Errorf("RATE_LIMIT_RPS must be > 0, got %v", rps))
	default:
		cfg.RateLimitRPS = rps
	}

	// SHUTDOWN_TIMEOUT is optional; when set it must be a positive duration.
	if v, ok := os.LookupEnv("SHUTDOWN_TIMEOUT"); ok && v != "" {
		switch d, err := time.ParseDuration(v); {
		case err != nil:
			errs = append(errs, fmt.Errorf("SHUTDOWN_TIMEOUT %q is not a valid duration: %w", v, err))
		case d <= 0:
			errs = append(errs, fmt.Errorf("SHUTDOWN_TIMEOUT must be > 0, got %v", d))
		default:
			cfg.ShutdownTimeout = d
		}
	}

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

// getEnv returns the value of key, or def when the variable is unset or empty.
func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

// requireFloat reads a required float64 environment variable.
func requireFloat(key string) (float64, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return 0, fmt.Errorf("%s is required", key)
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("%s %q is not a number: %w", key, v, err)
	}
	return f, nil
}
