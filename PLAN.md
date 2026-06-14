# ip2location-service — Implementation Plan

A REST service that resolves an IP address to a location (country, city), with a
hand-rolled rate limiter and a swappable datastore.

## Locked decisions (and why)

| Area | Decision | One-line rationale |
|---|---|---|
| **Data model** | Range-based CSV + binary search over `netip.Addr` | Matches how real GeoIP data is structured; `O(log n)` lookup; gaps → clean 404. |
| **Rate limiter** | Token bucket, lazy refill, injected clock, configurable burst | Industry standard (what `x/time/rate` does, which is banned); smooth; `O(1)`; testable. |
| **Limiter scope** | Global (one bucket), behind an `Allow() bool` interface | Literal reading of the spec (one limit, one env var); per-client is a documented extension. |
| **Config** | Env vars → validated `Config` struct, fail-fast at boot | 12-factor; bad config never reaches runtime. |
| **Extensibility** | `Locator` interface + factory `switch` on `DATASTORE_TYPE` | Add a DB = implement interface + one case. |
| **HTTP** | stdlib `net/http`, Go 1.22+ method-aware `ServeMux` plus JSON catch-all | No router dependency; router-level errors keep the JSON error contract. |
| **Dependencies** | Zero external (stdlib only, incl. `testing`, `log/slog`) | Easiest to defend; no supply chain. |
| **Polish** | Server timeouts, graceful shutdown, panic recovery, slog + request logging, `ctx` in interface | Each maps to a concrete prod failure mode; all stdlib. |

## Package layout

```
cmd/server/main.go            # wiring only: config → deps → server → graceful shutdown
internal/config/config.go     # read + validate env into Config
internal/geo/location.go      # Location type, Locator interface, ErrNotFound
internal/geo/csv.go           # range-based CSV store (sort + binary search)
internal/geo/factory.go       # New(cfg) → Locator, switch on type
internal/ratelimit/bucket.go  # token bucket + injectable clock
internal/httpapi/router.go    # build the mux + middleware chain
internal/httpapi/handler.go   # find-country handler, /healthz
internal/httpapi/errors.go    # JSON error helper, status mapping
internal/httpapi/middleware.go    # rate limiting, panic recovery, request logging
testdata/ip-ranges.csv        # sample data
README.md                     # run instructions + design write-up
```

`cmd/ + internal/` is the standard Go layout; `internal/` blocks external imports;
one job per package.

## Configuration (env vars)

| Var | Required | Default | Notes |
|---|---|---|---|
| `SERVER_ADDR` | no | `:8080` | listen address |
| `DATASTORE_TYPE` | no | `csv` | selects the `Locator` implementation |
| `DATASTORE_DSN` | yes (for csv) | — | path to the data file |
| `RATE_LIMIT_RPS` | yes | — | must be `> 0`; token refill rate |
| `RATE_LIMIT_BURST` | no | `ceil(RATE_LIMIT_RPS)`, minimum `1` | maximum burst size |
| `SHUTDOWN_TIMEOUT` | no | `10s` | grace period for in-flight requests |

Invalid/missing required values → log and exit non-zero before the server starts.

## Datastore

```go
type Location struct {
    Country string
    City    string
}

type Locator interface {
    Find(ctx context.Context, ip netip.Addr) (Location, error)
}

var ErrNotFound = errors.New("ip not found")
```

- **Factory:** `geo.New(cfg) (Locator, error)` — `switch cfg.DatastoreType { case "csv": ... }`.
- **CSV store:** rows are `start_ip,end_ip,country,city`. At load: parse → `[]rangeRow`
  (`start`, `end` as `netip.Addr`) → sort by `start`. Validate: parseable IPs, `start <= end`,
  4 fields/row. Lookup: `sort.Search` for the rightmost `start <= ip`, then confirm `ip <= end`;
  miss → `ErrNotFound`.
- Scope: **IPv4** data file. `ctx` is accepted but unused by the in-memory CSV store;
  it exists so a future network-backed store can honor cancellation/deadlines.

## Rate limiter

```go
type TokenBucket struct {
    mu         sync.Mutex
    tokens     float64
    capacity   float64
    refillRate float64        // tokens/sec == RPS
    lastRefill time.Time
    now        func() time.Time   // injectable clock; defaults to time.Now
}

func (b *TokenBucket) Allow() bool   // lazy refill, then try to spend one token
```

- `RATE_LIMIT_RPS` controls refill speed; `RATE_LIMIT_BURST` controls capacity.
- Default burst is one second of headroom: `ceil(RATE_LIMIT_RPS)`, with a minimum of 1.
- **Middleware** wraps any `interface{ Allow() bool }`: on `false` → `429` with the JSON error
  body + `Retry-After: 1`. Keeping it behind the interface makes per-client a clean later swap.

## HTTP API

**Endpoint:** `GET /v1/find-country?ip=<addr>` → `200 {"country":"XX","city":"YY"}`

**Errors:** `{"error":"<message>"}` with status:

| Condition | Status |
|---|---|
| missing/empty `ip`, or fails `netip.ParseAddr` | `400` |
| IP not in any range (`ErrNotFound`) | `404` |
| rate limit exceeded | `429` |
| panic / unexpected | `500` |

**Health:** `GET /healthz` → `200 ok` (liveness).

**Middleware chain (outermost → innermost):**
`request-logging → recover → mux`
(logging records 429s and recovered 500s; rate-limit is applied per-route to the API handler.)

Handler maps errors with `errors.Is(err, geo.ErrNotFound)`; input is validated before the
lookup so 400 vs 404 is unambiguous.

## Production polish (Tier 1 + 2)

- **`http.Server` timeouts:** `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout`
  (defends against Slowloris).
- **Graceful shutdown:** `signal.NotifyContext` on SIGINT/SIGTERM → `server.Shutdown(ctx)`
  bounded by `SHUTDOWN_TIMEOUT`.
- **Panic recovery middleware:** recover → JSON `500`.
- **Structured logging:** `log/slog` with levels; request-logging middleware logs
  method, path, status, duration (via a `responseWriter` wrapper that captures status).
- **`ctx` in `Locator.Find`:** future-proofs network-backed stores.

## Testing

Table-driven, stdlib `testing` + `net/http/httptest`:

- **config:** valid load, missing required, invalid RPS.
- **csv store:** in-range hit, boundary (start/end inclusive), gap → `ErrNotFound`,
  malformed row rejected at load.
- **token bucket:** uses a **fake clock** (advance time manually, no `time.Sleep`):
  burst up to capacity, refill over time, depletion → deny.
- **rate-limit middleware:** returns 429 when denied.
- **handler:** 200 shape, 400 (missing/invalid ip), 404 (not found) via `httptest`.

## Implementation order (each step compiles + has tests)

1. `internal/config` + tests.
2. `internal/geo`: types, interface, CSV store, factory + tests + `testdata/ip-ranges.csv`.
3. `internal/ratelimit`: token bucket (+ fake clock) + tests, then middleware + tests.
4. `internal/httpapi`: errors, handler, router + tests.
5. `cmd/server/main.go`: slog, server timeouts, middleware chain, graceful shutdown; replace the current placeholder `main.go`.
6. `README.md` + sample data.
7. Gate: `go vet ./...`, `golangci-lint run ./...`, `go test ./...` all clean.

## Out of scope (documented "what I'd add next")

Per-client rate limiting (identity/XFF/eviction), IPv6 data, request-ID/tracing,
readiness probe, Dockerfile, a real GeoIP datastore (MaxMind/IP2Location) behind the
same interface.
