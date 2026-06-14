package config_test

import (
	"testing"

	"github.com/shaniavital/ip2location-service/internal/config"
)

func TestLoad(t *testing.T) {
	// Every variable Load reads. Each subtest sets all of them explicitly
	// (absent keys become ""), so the result never depends on the host's real
	// environment. An empty value is treated as "unset" by Load.
	keys := []string{"SERVER_ADDR", "DATASTORE_TYPE", "DATASTORE_DSN", "RATE_LIMIT_RPS"}

	tests := []struct {
		name    string
		env     map[string]string
		want    config.Config
		wantErr bool
	}{
		{
			name: "valid full config",
			env: map[string]string{
				"SERVER_ADDR":    ":9090",
				"DATASTORE_TYPE": "csv",
				"DATASTORE_DSN":  "/data/ip.csv",
				"RATE_LIMIT_RPS": "100",
			},
			want: config.Config{
				ServerAddr:    ":9090",
				DatastoreType: "csv",
				DatastoreDSN:  "/data/ip.csv",
				RateLimitRPS:  100,
			},
		},
		{
			name: "defaults applied when only required var is set",
			env:  map[string]string{"RATE_LIMIT_RPS": "50"},
			want: config.Config{
				ServerAddr:    ":8080",
				DatastoreType: "csv",
				RateLimitRPS:  50,
			},
		},
		{
			name: "fractional rate is allowed",
			env:  map[string]string{"RATE_LIMIT_RPS": "0.5"},
			want: config.Config{
				ServerAddr:    ":8080",
				DatastoreType: "csv",
				RateLimitRPS:  0.5,
			},
		},
		{name: "missing required rate limit", env: map[string]string{}, wantErr: true},
		{name: "rate limit not a number", env: map[string]string{"RATE_LIMIT_RPS": "abc"}, wantErr: true},
		{name: "rate limit zero", env: map[string]string{"RATE_LIMIT_RPS": "0"}, wantErr: true},
		{name: "rate limit negative", env: map[string]string{"RATE_LIMIT_RPS": "-5"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, k := range keys {
				t.Setenv(k, tt.env[k]) // missing → "", which Load treats as unset
			}

			got, err := config.Load()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Load() expected an error, got nil (config=%+v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Load() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
