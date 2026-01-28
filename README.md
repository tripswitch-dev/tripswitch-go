# tripswitch-go

[![Go Reference](https://pkg.go.dev/badge/github.com/tripswitch-dev/tripswitch-go.svg)](https://pkg.go.dev/github.com/tripswitch-dev/tripswitch-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/tripswitch-dev/tripswitch-go)](https://goreportcard.com/report/github.com/tripswitch-dev/tripswitch-go)
[![Version](https://img.shields.io/badge/version-v0.6.0-blue)](https://github.com/tripswitch-dev/tripswitch-go/releases/tag/v0.6.0)

Official Go client SDK for [Tripswitch](https://tripswitch.dev) - a circuit breaker management service.

> **v0.6.0 Breaking Changes:** The `Execute` function signature has changed. Both `routerID` and breakers are now optional via `WithRouter()` and `WithBreakers()`. See [Migration](#migration-to-v060) for details.

This SDK conforms to the [Tripswitch SDK Contract v0.2](https://tripswitch.dev/docs/sdk-contract).

## Features

- **Real-time state sync** via Server-Sent Events (SSE)
- **Automatic sample reporting** with buffered, batched uploads
- **Fail-open by default** - your app stays available even if Tripswitch is unreachable
- **Goroutine-safe** - one client per project, safe for concurrent use
- **Graceful shutdown** with context-aware close and sample flushing

## Installation

```bash
go get github.com/tripswitch-dev/tripswitch-go
```

**Requires Go 1.22+** (uses `math/rand/v2` for thread-safe random number generation)

## Authentication

Tripswitch uses a two-tier authentication model introduced in v0.3.0:

### Runtime Credentials (SDK)

For SDK initialization, you need two credentials from **Project Settings → SDK Keys**:

| Credential | Prefix | Purpose |
|------------|--------|---------|
| **Project Key** | `eb_pk_` | SSE connection and state reads |
| **Ingest Secret** | `ik_` | HMAC-signed sample ingestion |

```go
ts := tripswitch.NewClient("proj_abc123",
    tripswitch.WithAPIKey("eb_pk_..."),    // Project key
    tripswitch.WithIngestKey("ik_..."),    // Ingest secret
)
```

### Admin Credentials (Management API)

For management and automation tasks, use an **Admin Key** from **Organization Settings → Admin Keys**:

| Credential | Prefix | Purpose |
|------------|--------|---------|
| **Admin Key** | `eb_admin_` | Organization-scoped management operations |

Admin keys are used with the [Admin Client](#admin-client) for creating projects, managing breakers, and other administrative tasks—not for runtime SDK usage.

## Quick Start

```go
package main

import (
    "context"
    "log"
    "net/http"
    "time"

    "github.com/tripswitch-dev/tripswitch-go"
)

func main() {
    // Create client
    ts := tripswitch.NewClient("proj_abc123",
        tripswitch.WithAPIKey("eb_pk_..."),
        tripswitch.WithIngestKey("ik_..."),
    )
    defer ts.Close(context.Background())

    // Wait for initial state sync before taking traffic
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if err := ts.Ready(ctx); err != nil {
        log.Fatal("tripswitch failed to initialize:", err)
    }

    // Wrap operations with circuit breaker
    resp, err := tripswitch.Execute(ts, ctx, func() (*http.Response, error) {
        return http.Get("https://api.example.com/data")
    },
        tripswitch.WithBreakers("external-api"),              // Gate on breaker state
        tripswitch.WithRouter("my-router-id"),                // Route samples to this router
        tripswitch.WithMetric("latency", tripswitch.Latency), // Report latency
    )
    if err != nil {
        if tripswitch.IsBreakerError(err) {
            // Circuit is open - return cached/fallback response
            log.Println("circuit open, using fallback")
            return
        }
        log.Println("request failed:", err)
        return
    }
    defer resp.Body.Close()
    // Process response...
}
```

## Configuration Options

### Client Options

| Option | Description | Default |
|--------|-------------|---------|
| `WithAPIKey(key)` | Project key (`eb_pk_`) for SSE authentication | Required |
| `WithIngestKey(key)` | Ingest secret (`ik_`) for HMAC-signed sample reporting | Required |
| `WithFailOpen(bool)` | Allow traffic when Tripswitch is unreachable | `true` |
| `WithBaseURL(url)` | Override API endpoint | `https://api.tripswitch.dev` |
| `WithLogger(logger)` | Custom logger (compatible with `slog.Logger`) | `slog.Default()` |
| `WithOnStateChange(fn)` | Callback on breaker state transitions | `nil` |
| `WithTraceIDExtractor(fn)` | Extract trace ID from context for each sample | `nil` |
| `WithGlobalTags(tags)` | Tags applied to all samples | `nil` |

### Execute Options

| Option | Description |
|--------|-------------|
| `WithBreakers(names...)` | Breaker names to check before executing (any open → `ErrOpen`). If omitted, no gating is performed. |
| `WithRouter(routerID)` | Router ID for sample routing. If omitted, no samples are emitted. |
| `WithMetric(key, value)` | Add a metric to report (`Latency` sentinel, `func() float64`, or numeric) |
| `WithMetrics(map)` | Add multiple metrics at once |
| `WithTag(key, value)` | Add a single diagnostic tag |
| `WithTags(tags)` | Diagnostic tags for this specific call (merged with global tags) |
| `WithIgnoreErrors(errs...)` | Errors that should not count as failures |
| `WithErrorEvaluator(fn)` | Custom function to determine if error is a failure (takes precedence over `WithIgnoreErrors`) |
| `WithTraceID(id)` | Explicit trace ID (takes precedence over `WithTraceIDExtractor`) |

### Error Classification

Every sample includes an `ok` field indicating whether the task succeeded or failed. This is determined by the following evaluation order:

1. **`WithErrorEvaluator(fn)`** — if set, takes precedence over everything else. The function signature is `func(error) bool`. Return `true` if the error **is a failure**; return `false` if it should be treated as success.

   ```go
   // Only count 5xx as failures; 4xx are "expected" errors
   tripswitch.WithErrorEvaluator(func(err error) bool {
       var httpErr *HTTPError
       if errors.As(err, &httpErr) {
           return httpErr.StatusCode >= 500
       }
       return true // non-HTTP errors are failures
   })
   ```

2. **`WithIgnoreErrors(errs...)`** — if the task error matches any of these (via `errors.Is`, so wrapped errors work), it is **not** counted as a failure.

   ```go
   // sql.ErrNoRows is expected, don't count it
   tripswitch.WithIgnoreErrors(sql.ErrNoRows)
   ```

3. **Default** — any non-nil error is a failure; nil error is success.

### Trace IDs

Trace IDs associate samples with distributed traces. Two ways to set them:

- **`WithTraceID(id)`** — explicit per-call trace ID. Takes precedence over the extractor.

- **`WithTraceIDExtractor(fn)`** (client option) — automatically extracts a trace ID from the context for every `Execute` call. Useful for OpenTelemetry integration:

  ```go
  tripswitch.WithTraceIDExtractor(func(ctx context.Context) string {
      span := trace.SpanFromContext(ctx)
      if span.SpanContext().IsValid() {
          return span.SpanContext().TraceID().String()
      }
      return ""
  })
  ```

If both are set, `WithTraceID` wins.

## API Reference

### NewClient

```go
func NewClient(projectID string, opts ...Option) *Client
```

Creates a new Tripswitch client. Automatically starts background goroutines for SSE state sync and sample flushing.

### Execute

```go
func Execute[T any](c *Client, ctx context.Context, task func() (T, error), opts ...ExecuteOption) (T, error)
```

Runs a task end-to-end: checks breaker state, executes the task, and reports samples — all in one call. There is no need to call a separate report method.

- Use `WithBreakers()` to gate execution on breaker state (omit for pass-through)
- Use `WithRouter()` to specify where samples go (omit for no sample emission)
- Use `WithMetric()` to specify what values to report

Returns `ErrOpen` if any specified breaker is open.

**Note:** This is a package-level generic function (not a method) because Go does not support generic methods.

### Latency

```go
var Latency = &latencyMarker{}
```

Sentinel value for `WithMetric` that instructs the SDK to automatically compute and report task duration in milliseconds.

### Ready

```go
func (c *Client) Ready(ctx context.Context) error
```

Blocks until the initial SSE handshake completes and breaker state is synced. Use this to ensure your app doesn't take traffic before state is known.

### Close

```go
func (c *Client) Close(ctx context.Context) error
```

Gracefully shuts down the client. The context controls how long to wait for buffered samples to flush.

### Stats

```go
func (c *Client) Stats() SDKStats
```

Returns a snapshot of SDK health metrics:

```go
type SDKStats struct {
    DroppedSamples      uint64    // Samples dropped due to buffer overflow
    BufferSize          int       // Current buffer occupancy
    SSEConnected        bool      // SSE connection status
    SSEReconnects       uint64    // Count of SSE reconnections
    LastSuccessfulFlush time.Time // Timestamp of last successful flush
}
```

### Error Handling

```go
var ErrOpen = errors.New("tripswitch: breaker is open")

func IsBreakerError(err error) bool
```

Use `IsBreakerError` to check if an error is circuit breaker related:

```go
result, err := tripswitch.Execute(ts, ctx, task,
    tripswitch.WithBreakers("my-breaker"),
)
if tripswitch.IsBreakerError(err) {
    // Breaker is open or request was throttled
    return fallbackValue, nil
}
```

## Custom Metric Values

`Latency` is a convenience sentinel that auto-computes task duration in milliseconds. You can report **any metric with any value**:

```go
// Auto-computed latency (convenience)
tripswitch.WithMetric("latency", tripswitch.Latency)

// Static numeric values
tripswitch.WithMetric("response_bytes", 4096)
tripswitch.WithMetric("queue_depth", 42.5)

// Dynamic values via closure (called after task completes)
tripswitch.WithMetric("memory_mb", func() float64 {
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    return float64(m.Alloc / 1024 / 1024)
})
```

### Reporting Without Wrapping a Task

For fire-and-forget metrics (e.g., values from a background process), use a no-op task:

```go
tripswitch.Execute(ts, ctx, func() (struct{}, error) {
    return struct{}{}, nil
},
    tripswitch.WithRouter("worker-metrics"),
    tripswitch.WithMetric("queue_depth", currentDepth),
    tripswitch.WithMetric("processing_time_ms", elapsed),
)
```

## Examples

See the [examples](./examples) directory for complete, runnable examples:

- **[HTTP Client Wrap](./examples/http)** - Wrap HTTP calls with circuit breaker
- **[Database Query](./examples/database)** - Ignore specific errors like `sql.ErrNoRows`
- **[Graceful Shutdown](./examples/shutdown)** - Handle OS signals and flush samples
- **[Custom Error Evaluator](./examples/evaluator)** - Ignore 4xx errors, count 5xx as failures
- **[OpenTelemetry Integration](./examples/otel)** - Extract trace IDs from OTel context
- **[Per-Request Tags](./examples/tags)** - Add diagnostic metadata to samples

## Circuit Breaker States

| State | Behavior |
|-------|----------|
| `closed` | All requests allowed, results reported |
| `open` | All requests rejected with `ErrOpen` |
| `half_open` | Requests throttled based on `allow_rate` (e.g., 20% allowed) |

## How It Works

1. **State Sync**: The client maintains a local cache of breaker states, updated in real-time via SSE
2. **Execute Check**: Each `Execute` call checks the local cache (no network call)
3. **Sample Reporting**: Results are buffered and batched (500 samples or 15s, whichever comes first)
4. **Graceful Degradation**: If Tripswitch is unreachable, the client fails open by default

## Admin Client

The `admin` package provides a client for management and automation tasks. This is separate from the runtime SDK and uses organization-scoped admin keys.

```go
import "github.com/tripswitch-dev/tripswitch-go/admin"

client := admin.NewClient(
    admin.WithAPIKey("eb_admin_..."), // From Organization Settings → Admin Keys
)

// Get project details
project, err := client.GetProject(ctx, "proj_abc123")

// List breakers
page, err := client.ListBreakers(ctx, "proj_abc123", admin.ListParams{Limit: 100})

// Create a breaker
breaker, err := client.CreateBreaker(ctx, "proj_abc123", admin.CreateBreakerInput{
    Name:      "api-latency",
    Metric:    "latency_ms",
    Kind:      admin.BreakerKindP95,
    Op:        admin.BreakerOpGt,
    Threshold: 500,
})
```

**Note:** Admin keys (`eb_admin_`) are for management operations only. For runtime SDK usage, use project keys (`eb_pk_`) as shown in [Quick Start](#quick-start).

## Migration to v0.6.0

v0.6.0 makes both breakers and router optional in the `Execute` signature:

```go
// Before (v0.5.0)
result, err := tripswitch.Execute(ts, ctx, "router-id", task,
    tripswitch.WithBreakers("my-breaker"),
    tripswitch.WithMetric("latency", tripswitch.Latency),
)

// After (v0.6.0)
result, err := tripswitch.Execute(ts, ctx, task,
    tripswitch.WithBreakers("my-breaker"),
    tripswitch.WithRouter("router-id"),
    tripswitch.WithMetric("latency", tripswitch.Latency),
)
```

**Key changes:**
- `routerID` parameter removed from signature, now optional via `WithRouter()`
- No `WithBreakers()` = no gating (pass-through, task always runs)
- No `WithRouter()` = no samples emitted (metrics silently ignored)
- If `WithMetric()` specified but no `WithRouter()`, a warning is logged
- Maximum flexibility: use gating only, metrics only, or both

**Usage patterns:**

```go
// Full usage - gating + metrics
tripswitch.Execute(c, ctx, task,
    tripswitch.WithBreakers("payment-gateway"),
    tripswitch.WithRouter("router-id"),
    tripswitch.WithMetric("latency", tripswitch.Latency),
)

// Gating only, no metrics
tripswitch.Execute(c, ctx, task,
    tripswitch.WithBreakers("payment-gateway"),
)

// Metrics only, no gating (observability without circuit breaking)
tripswitch.Execute(c, ctx, task,
    tripswitch.WithRouter("router-id"),
    tripswitch.WithMetric("latency", tripswitch.Latency),
)
```

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.

## License

[Apache License 2.0](LICENSE)
