// Package geo resolves an IP address to a Location. It defines the Locator
// interface (the seam that makes the service "extendable to different ip2country
// databases") and ships a range-based CSV implementation. New databases plug in
// by implementing Locator and adding a case to New.
package geo

import (
	"context"
	"errors"
	"net/netip"
)

// Location is the result of a lookup. City may be empty for sources that only
// resolve to country granularity.
type Location struct {
	Country string
	City    string
}

// Locator resolves an IP to a Location. It is the single contract every
// datastore implements; callers depend on this, never on a concrete store.
//
// ctx is honored by network-backed stores (Postgres, Redis, an HTTP API) for
// cancellation and deadlines. In-memory stores such as the CSV one ignore it.
type Locator interface {
	Find(ctx context.Context, ip netip.Addr) (Location, error)
}

// ErrNotFound is returned when no range contains the requested IP. Callers
// match it with errors.Is to map the result to an HTTP 404.
var ErrNotFound = errors.New("ip not found")
