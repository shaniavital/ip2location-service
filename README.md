# ip2location-service

An HTTP service that resolves an IP address to a location (country, city). It is
built around two design goals from the brief: a datastore that is **easy to
extend to different ip2country databases**, and a **hand-rolled rate limiter**
(no third-party rate-limiting libraries).

## Highlights

- `GET /v1/find-country?ip=...` → `{"country":"..","city":".."}`, JSON errors throughout.
- Pluggable datastore behind a `Locator` interface, selected by an environment variable.
- Token-bucket rate limiter written from scratch, configured by an environment variable.
- Production touches: structured logging (`log/slog`), HTTP server timeouts, panic recovery.
- **Zero external dependencies** — standard library only, including the tests.

## Requirements

- Go 1.26+

## Quick start

```bash
# Build
go build -o bin/server ./cmd/server

# Run (a sample datastore is provided under data/)
SERVER_ADDR=:8080 \
DATASTORE_TYPE=csv \
DATASTORE_DSN=data/ip-ranges.csv \
RATE_LIMIT_RPS=50 \
./bin/server
```

Then:

```bash
curl 'localhost:8080/v1/find-country?ip=8.8.8.8'
# {"country":"US","city":"Mountain View"}
```

## Configuration

All configuration is via environment variables (12-factor). Invalid or missing
required values cause the service to fail at startup rather than at request time.

| Variable | Required | Default | Description |
|---|---|---|---|
| `RATE_LIMIT_RPS` | **yes** | — | Rate limit in requests/second. Must be `> 0` (fractions allowed, e.g. `0.5`). |
| `DATASTORE_DSN` | yes (for `csv`) | — | Driver-specific source string. For `csv`, a file path. |
| `DATASTORE_TYPE` | no | `csv` | Selects the datastore implementation ("driver"). |
| `SERVER_ADDR` | no | `:8080` | Listen address. |

## API

### `GET /v1/find-country?ip=<address>`

| Status | When | Body |
|---|---|---|
| `200` | IP resolved | `{"country":"IL","city":"Tel Aviv"}` |
| `400` | `ip` missing or unparseable | `{"error":"invalid ip address"}` |
| `404` | IP valid but not in any range | `{"error":"no location found for the given ip"}` |
| `404` | Unknown path | `{"error":"not found"}` |
| `405` | Unsupported method on a known path | `{"error":"method not allowed"}` |
| `429` | Rate limit exceeded | `{"error":"rate limit exceeded"}` (with `Retry-After: 1`) |
| `500` | Unexpected datastore error | `{"error":"internal server error"}` |

```bash
curl -i 'localhost:8080/v1/find-country?ip=2.22.233.255'   # 200
curl -i 'localhost:8080/v1/find-country?ip=1.1.1.1'        # 404
curl -i 'localhost:8080/v1/find-country?ip=nope'           # 400
```

### `GET /healthz`

Liveness probe returning `200 ok`. **Not** rate-limited (see design notes), so a
load balancer's health check is never rejected under load.

## Datastore format & extensibility

The active datastore is chosen by `DATASTORE_TYPE` and constructed by a factory
(`internal/geo/factory.go`). Every datastore satisfies one interface:

```go
type Locator interface {
    Find(ctx context.Context, ip netip.Addr) (Location, error)
}
```

Extensibility lives in this interface, **not** in any particular file format.
Adding a new ip2country database (e.g. MaxMind, IP2Location, a Postgres table) is:

1. a new type implementing `Locator`,
2. one `case` in the factory,
3. a new `DATASTORE_TYPE` / `DATASTORE_DSN` value.

No handler, config-struct, or other store changes. `ctx` is already in the
signature so a network-backed store can honor cancellation/deadlines.

### The bundled `csv` driver

`DATASTORE_DSN` is a file path. Each line is an **inclusive IP range**:

```
start_ip,end_ip,country,city
```

Example (`data/ip-ranges.csv`):

```
2.22.233.0,2.22.233.255,IL,Tel Aviv
8.8.8.0,8.8.8.255,US,Mountain View
```

Lines starting with `#` are comments; `country` is required, `city` may be empty.
The file is parsed, validated, sorted, and checked for overlaps **once at
startup**; lookups are an `O(log n)` binary search.

> The brief offered a single-IP `ip,city,country` format "for the sake of the
> exercise". I chose range rows instead because every real ip2country database is
> range-based, which makes the extensibility requirement concrete (a real
> provider drops in without an impedance mismatch). The `Locator` interface is
> unchanged either way.

## Architecture

```
cmd/server/main.go         Wiring: config → datastore → limiter → router → server
internal/config            Load + validate environment configuration
internal/geo               Locator interface, ErrNotFound, range-based CSV store, factory
internal/ratelimit         Token-bucket limiter (pure algorithm, no HTTP dependency)
internal/httpapi           Handlers, JSON error helper, middleware (rate limit, panic recovery), router
```

## Design notes

- **Rate limiting — token bucket.** Tokens refill lazily at `RATE_LIMIT_RPS`
  tokens/second up to a capacity of one second's worth of traffic (floored at 1
  so fractional rates work). "Lazy" means no background goroutine: `Allow()`
  computes accrued tokens from elapsed time, making it `O(1)`. The clock is
  injectable, so time-based tests are deterministic (no `time.Sleep`). This is
  the approach the standard `golang.org/x/time/rate` uses — which the brief
  disallows, so it is reimplemented here.
- **Rate-limit scope — global.** The brief specifies a single limit via a single
  variable, so the limiter is global ("the service accepts N req/s total"). The
  middleware depends on a tiny `Limiter` interface, so a per-client limiter could
  replace it without touching the middleware. See "what I'd add next".
- **Health checks bypass the limiter.** A rate-limited `/healthz` would let a
  traffic spike trip the limiter and cause an orchestrator to mark the instance
  unhealthy — an outage from load. So the limiter is applied per-route to the API
  endpoint only.
- **JSON errors throughout.** Handler errors, rate-limit errors, unknown paths,
  and unsupported methods all use the same `{"error":"..."}` shape.
- **Errors don't leak internals.** A datastore failure is logged server-side; the
  client gets a generic `500` message.
- **Fail fast.** Bad config or a bad datastore stops the service at startup, not
  on the first request.

## Testing

```bash
go test ./...
go test -race ./...        # the limiter has a concurrency test
```

Coverage is table-driven and uses only the standard library (`testing`,
`net/http/httptest`):

- **config** — defaults, valid load, and rate-limit validation failures.
- **geo** — range lookups (boundaries, gaps, below/above all ranges) and load-time
  rejection of malformed/overlapping data.
- **ratelimit** — burst, refill-over-time, capacity cap, fractional rate, and a
  concurrent test (run under `-race`) asserting exactly `capacity` requests pass.
- **httpapi** — every status code, JSON error bodies (including 404/405), and
  panic recovery.

## Linting

```bash
golangci-lint run ./...
```

## Known limitations / what I'd add next

- **Graceful shutdown** — currently the process exits immediately on a signal; a
  `signal.NotifyContext` + `http.Server.Shutdown` would drain in-flight requests.
- **Request/access logging** — a logging middleware (with a status-capturing
  response writer) would record one line per request.
- **Per-client rate limiting** (keyed by client IP, with eviction) — the seam is
  in place via the `Limiter` interface.
- **IPv6 data** — the lookup uses `net/netip`, which supports IPv6; the sample
  data and validation assume a single address family per file.
- **A real GeoIP datastore** (MaxMind / IP2Location / SQL) behind the same
  `Locator` interface, with connection cleanup on shutdown.
- A readiness probe distinct from liveness, request-ID/tracing, and a container image.
