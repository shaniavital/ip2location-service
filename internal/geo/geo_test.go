package geo_test

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/shaniavital/ip2location-service/internal/geo"
)

const testDataFile = "testdata/ip-ranges.csv"

func TestCSVStore_Find(t *testing.T) {
	store, err := geo.New("csv", testDataFile)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	tests := []struct {
		name string
		ip   string
		want geo.Location
		err  error
	}{
		{name: "mid range", ip: "2.22.233.128", want: geo.Location{Country: "IL", City: "Tel Aviv"}},
		{name: "start boundary inclusive", ip: "2.22.233.0", want: geo.Location{Country: "IL", City: "Tel Aviv"}},
		{name: "end boundary inclusive", ip: "2.22.233.255", want: geo.Location{Country: "IL", City: "Tel Aviv"}},
		{name: "example from spec", ip: "8.8.8.8", want: geo.Location{Country: "US", City: "Mountain View"}},
		{name: "first range", ip: "1.0.0.1", want: geo.Location{Country: "AU", City: "Brisbane"}},
		{name: "last range", ip: "9.9.9.9", want: geo.Location{Country: "US", City: "Berkeley"}},
		{name: "below all ranges", ip: "0.0.0.1", err: geo.ErrNotFound},
		{name: "above all ranges", ip: "255.255.255.255", err: geo.ErrNotFound},
		{name: "gap between ranges", ip: "5.5.5.5", err: geo.ErrNotFound},
		{name: "just past a range end", ip: "1.0.1.0", err: geo.ErrNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := netip.MustParseAddr(tt.ip)
			got, err := store.Find(context.Background(), ip)

			if tt.err != nil {
				if !errors.Is(err, tt.err) {
					t.Fatalf("Find(%s) error = %v, want %v", tt.ip, err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Find(%s) unexpected error: %v", tt.ip, err)
			}
			if got != tt.want {
				t.Errorf("Find(%s) = %+v, want %+v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestNew_LoadErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "malformed start ip", content: "not-an-ip,1.0.0.255,AU,Brisbane\n"},
		{name: "malformed end ip", content: "1.0.0.0,nope,AU,Brisbane\n"},
		{name: "start greater than end", content: "1.0.0.255,1.0.0.0,AU,Brisbane\n"},
		{name: "too few fields", content: "1.0.0.0,1.0.0.255,AU\n"},
		{name: "too many fields", content: "1.0.0.0,1.0.0.255,AU,Brisbane,extra\n"},
		{name: "empty country", content: "1.0.0.0,1.0.0.255, ,Brisbane\n"},
		{name: "mixed address families", content: "1.0.0.0,::ffff,AU,Brisbane\n"},
		{name: "overlapping ranges", content: "1.0.0.0,1.0.0.255,AU,Brisbane\n1.0.0.100,1.0.0.200,US,Reston\n"},
		{name: "no records", content: "# only a comment\n"},
		{name: "empty file", content: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempCSV(t, tt.content)
			if _, err := geo.New("csv", path); err == nil {
				t.Errorf("New() expected an error for %q, got nil", tt.name)
			}
		})
	}
}

func TestNew_Errors(t *testing.T) {
	t.Run("unknown datastore type", func(t *testing.T) {
		if _, err := geo.New("redis", "whatever"); err == nil {
			t.Error("New() expected an error for unknown type, got nil")
		}
	})
	t.Run("empty dsn for csv", func(t *testing.T) {
		if _, err := geo.New("csv", ""); err == nil {
			t.Error("New() expected an error for empty dsn, got nil")
		}
	})
	t.Run("missing file", func(t *testing.T) {
		if _, err := geo.New("csv", "testdata/does-not-exist.csv"); err == nil {
			t.Error("New() expected an error for missing file, got nil")
		}
	})
}

// writeTempCSV writes content to a temp file and returns its path. The file is
// cleaned up automatically when the test ends.
func writeTempCSV(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ranges.csv")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp csv: %v", err)
	}
	return path
}
