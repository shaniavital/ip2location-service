// Package config loads the service configuration from environment variables,
// applying defaults and failing fast when a required value is missing or invalid.
package config

import (
	"fmt"
	"os"
	"strconv"
)

const (
	defaultServerAddr    = ":8080"
	defaultDatastoreType = "csv"
)

// Config holds the resolved service configuration.
type Config struct {
	ServerAddr    string  // listen address, e.g. ":8080"
	DatastoreType string  // selects the datastore implementation (the "driver")
	DatastoreDSN  string  // driver-specific source string (a file path for csv)
	RateLimitRPS  float64 // rate limit in requests/sec; must be > 0
}

// Load reads configuration from the environment and validates it. It returns an
// error (and the service refuses to start) if a required value is missing or
// invalid, so misconfiguration is caught at startup rather than at request time.
func Load() (Config, error) {
	cfg := Config{
		ServerAddr:    getEnv("SERVER_ADDR", defaultServerAddr),
		DatastoreType: getEnv("DATASTORE_TYPE", defaultDatastoreType),
		DatastoreDSN:  getEnv("DATASTORE_DSN", ""),
	}

	// RATE_LIMIT_RPS is required and must be positive. It is a float so the
	// limiter can express fractional rates (e.g. 0.5 == one request every 2s).
	rps, err := requireFloat("RATE_LIMIT_RPS")
	if err != nil {
		return Config{}, err
	}
	if rps <= 0 {
		return Config{}, fmt.Errorf("RATE_LIMIT_RPS must be > 0, got %v", rps)
	}
	cfg.RateLimitRPS = rps

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
