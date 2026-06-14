# ip2location-service — Design Notes

A REST service that resolves an IP address to a location (country, city), with a
hand-rolled rate limiter and a swappable datastore. This document records the
design decisions and the reasoning behind them.

## Decisions (and why)

| Area | Decision | One-line rationale |
|---|---|---|
| **Data model** | Range-based CSV + binary search over `netip.Addr` | Matches how real GeoIP data is structured; `O(log n)` lookup; gaps → clean 404. |
| **Rate limiter** | Token bucket, lazy refill, injectable clock | Standard approach (what `x/time/rate` does, which is banned); smooth; `O(1)`; testable. |
| **Limiter scope** | Global (one bucket), behind an `Allow() bool` interface | Literal reading of the spec (one limit, one env var); per-client is a documented extension. |
| **Config** | Env vars → validated `Config` struct, fail-fast at boot | 12-factor; bad config never reaches runtime. |
| **Extensibility** | `Locator` interface + factory `switch` on `DATASTORE_TYPE` | Add a DB = implement interface + one case. |
| **HTTP** | stdlib `net/http`, Go 1.22+ method-aware `ServeMux` | No router dependency. |
| **Dependencies** | Zero external (stdlib only, incl. `testing`, `log/slog`) | Easiest to defend; no supply chain. |

## Package layout

```
cmd/server/main.go            # wiring: config → datastore → limiter → router → server
internal/config/config.go     # read + validate env into Config
internal/geo/location.go      # Location type, Locator interface, ErrNotFound
internal/geo/csv.go           # range-based CSV store (sort + binary search)
internal/geo/factory.go       # New(type, dsn) → Locator, switch on type
internal/ratelimit/bucket.go  # token bucket + injectable clock
internal/httpapi/errors.go    # JSON error writer (single source of truth)
internal/httpapi/handler.go   # find-country handler, /healthz
internal/httpapi/middleware.go# rate-limit (429) + panic-recovery middleware
internal/httpapi/router.go    # builds the mux + middleware, JSON 404/405 catch-all
```

`cmd/ + internal/` is the standard Go layout; one job per package.

## Key contracts

```go
type Location struct { Country, City string }

type Locator interface {
    Find(ctx context.Context, ip netip.Addr) (Location, error)
}
var ErrNotFound = errors.New("ip not found")   // handler maps via errors.Is → 404

type Limiter interface { Allow() bool }         // declared in httpapi (consumer side)
```

## Datastore

- **Factory:** `geo.New(type, dsn)` — `switch` on type; `csv` today, a new `case`
  per future store.
- **CSV store:** rows are `start_ip,end_ip,country,city`. At load: parse → validate
  (valid IPs, same family, `start <= end`, country non-empty) → sort by start →
  reject overlaps. Lookup: binary search for the last `start <= ip`, then confirm
  `ip <= end`; miss → `ErrNotFound`.
- Scope: IPv4 data file. `ctx` is accepted but unused by the in-memory CSV store;
  it exists so a future network-backed store can honor cancellation/deadlines.

## Rate limiter

- Capacity (burst) = one second of tokens, floored at 1 so fractional rates work.
- Lazy refill: `Allow()` adds `elapsed * rate` tokens (capped at capacity) on each
  call — no background goroutine. Mutex-guarded for concurrent requests.
- Clock injected via an internal constructor so tests advance time deterministically.

## HTTP API

- `GET /v1/find-country?ip=<addr>` → `200 {"country","city"}`.
- Errors `{"error":"..."}`: `400` bad/missing ip · `404` not found / unknown path ·
  `405` wrong method · `429` rate limited · `500` unexpected.
- `GET /healthz` → `200 ok`, **not** rate-limited (a throttled health check would
  cause a load balancer to drop a busy-but-healthy instance).
- Middleware: `recoverPanic` wraps the mux; `rateLimit` is applied per-route to the
  API endpoint only.

## Deliberate simplifications

The brief's first requirement is "clear and easy to read code", so a few
production niceties were intentionally left out to keep the code small and fully
explainable. Each is listed under "what I'd add next" in the README:

- **No graceful shutdown** — plain `ListenAndServe`; a signal exits immediately.
  (Avoids goroutine + channel + `select` + signal handling.)
- **No request/access logging** — keeps the response-writer wrapping out of scope.
  (Startup, server-side errors, and recovered panics are still logged.)
- **Single rate-limit knob** — `RATE_LIMIT_RPS` only; burst defaults to the rate.
- **No connection-cleanup hook** — the CSV store has nothing to close; a DB store
  would add one.

## Testing

Table-driven, stdlib `testing` + `net/http/httptest`:

- **config:** valid load, defaults, missing/invalid rate limit.
- **geo:** in-range hits, boundaries, gaps, below/above all ranges; load-time
  rejection of malformed/overlapping data.
- **ratelimit:** burst, refill over time, capacity cap, fractional rate, and a
  concurrent test (fake clock, `-race`) asserting exactly `capacity` requests pass.
- **httpapi:** every status code, JSON error bodies (incl. 404/405), panic recovery.

## Gate

`gofmt` · `go vet ./...` · `golangci-lint run ./...` · `go test -race ./...` — all clean.
