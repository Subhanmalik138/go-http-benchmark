# httpload — Concurrent HTTP Benchmarking CLI

A small, zero-dependency HTTP benchmarking tool written in Go. Point it at an
endpoint and it fires concurrent requests, then reports latency percentiles,
throughput, and the status-code breakdown — the numbers you actually need
before deciding whether an endpoint is fast enough.

```text
$ httpload -url https://example.com -n 1000 -c 50

Benchmarking GET https://example.com with 50 workers, 1000 requests...

Requests:      1000 (1000 ok, 0 failed)
Duration:      2.41s
Requests/sec:  415.20
Data received: 1.24 MiB

Latency (successful requests):
  min    8.1ms
  mean   118.4ms
  stddev 24.7ms
  p50    112.3ms
  p90    152.8ms
  p95    171.2ms
  p99    204.6ms
  max    231.9ms

Status codes:
  200  1000
```

## Why this exists

Most load-testing tools are either huge (JMeter, k6 with a JS runtime) or
single-threaded. `httpload` is a single static binary with no dependencies
that answers one question well: *how does this endpoint behave under N
concurrent clients?* It is useful for smoke-testing a new service, comparing
two implementations, or catching a latency regression in CI.

## Design decisions

- **Worker pool, not one goroutine per request.** A fixed pool of workers
  pulls jobs from a channel, so concurrency is bounded and memory stays flat
  even for `-n 100000`.
- **Connection reuse.** The HTTP transport caps idle keep-alive connections
  at the worker count, so the measurement reflects server latency rather
  than TCP/TLS handshake cost on every request.
- **Latency is measured over successful requests only.** Timeouts and
  connection errors are counted separately and grouped by kind
  (`connection refused`, `timeout`, `dns error`, ...), so a few failures
  don't skew the percentiles.
- **Two stop conditions.** Stop after N requests (`-n`), after a wall-clock
  duration (`-d`), or whichever comes first when both are set. A duration
  run is how you benchmark an endpoint you can't count requests against.
- **Percentiles use the nearest-rank method** on sorted latencies, and the
  report includes mean and standard deviation so you can tell a stable
  endpoint from a jittery one at a glance.

## Install

Requires Go 1.23 or newer.

```bash
go install github.com/Subhanmalik138/go-http-benchmark/cmd/httpload@latest
```

Or build from source:

```bash
git clone https://github.com/Subhanmalik138/go-http-benchmark.git
cd go-http-benchmark
go build -o httpload ./cmd/httpload
```

## Usage

```
httpload -url <target> [-n requests | -d duration] [-c concurrency]
         [-method METHOD] [-H "Name: value"]... [-body ...]
         [-timeout 30s] [-json]
```

| Flag | Default | Description |
| ---- | ------- | ----------- |
| `-url` | — | Target URL (**required**, `http` or `https`) |
| `-n` | 0 | Total requests to send |
| `-d` | 0 | Max run duration, e.g. `30s` |
| `-c` | 10 | Concurrent workers |
| `-method` | `GET` | HTTP method |
| `-H` | — | Extra header, `"Name: value"` (repeatable) |
| `-body` | — | Request body sent with every request |
| `-timeout` | `30s` | Per-request timeout |
| `-json` | false | Print the report as JSON instead of a table |

At least one of `-n` or `-d` is required. Press Ctrl-C to stop early — the
report is still printed for the requests that completed.

### Examples

```bash
# 500 requests, 20 at a time
httpload -url https://api.example.com/health -n 500 -c 20

# Hammer for 30 seconds, machine-readable output for CI
httpload -url https://api.example.com/users -d 30s -c 50 -json

# POST with a JSON body and an auth header
httpload -url https://api.example.com/items -method POST \
  -H "Authorization: Bearer TOKEN" -H "Content-Type: application/json" \
  -body '{"name":"widget"}' -n 100 -c 10
```

## Tests

```bash
go test ./...
```

The suite covers config validation, percentile math, error classification,
and the runner itself against a local `httptest` server — including a
duration-capped run and a refused-connection target.

## License

MIT — see [LICENSE](LICENSE).
