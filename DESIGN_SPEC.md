# Tripswitch Go SDK Spec

## Overview

First SDK for Tripswitch. Go first, then TypeScript and Python once Go is solid.

## SDK Instance

Project-scoped. One client per project, goroutine-safe.

### Functional Options Pattern

```go
ts := tripswitch.NewClient("proj_abc123",
    tripswitch.WithAPIKey("sk_..."),
    tripswitch.WithIngestKey("ik_..."),
    tripswitch.WithFailOpen(true),                    // default: true
    tripswitch.WithBaseURL("https://..."),            // optional override
    tripswitch.WithLogger(slog.Default()),            // optional custom logger
    tripswitch.WithOnStateChange(func(name, from, to string) {
        // callback on breaker state changes
    }),
    tripswitch.WithTraceIDExtractor(func(ctx context.Context) string {
        // Pull TraceID from context (e.g., OpenTelemetry)
        return trace.SpanFromContext(ctx).SpanContext().TraceID().String()
    }),
    tripswitch.WithGlobalTags(map[string]string{
        "service": "checkout-svc",
        "env":     "production",
    }),
)
defer ts.Close()
```

### Options

| Option                 | Description                                      | Default                          |
|------------------------|--------------------------------------------------|----------------------------------|
| `WithAPIKey`           | Authenticates SSE connection for reading state   | Required                         |
| `WithIngestKey`        | Authenticates sample ingestion (Report flush)    | Required                         |
| `WithFailOpen`         | Allow traffic when Tripswitch unreachable        | `true`                           |
| `WithBaseURL`          | Override API endpoint                            | `https://api.tripswitch.dev`     |
| `WithLogger`           | Custom logger (interface-based)                  | `slog.Default()`                 |
| `WithOnStateChange`    | Callback on breaker state transitions            | `nil`                            |
| `WithTraceIDExtractor` | Extracts TraceID from context for each sample    | `nil`                            |
| `WithGlobalTags`       | Tags applied to all samples (e.g., service name) | `nil`                            |

## Interface

```go
type Client struct {
    // unexported fields
}

func NewClient(projectID string, opts ...Option) *Client

func (c *Client) Ready(ctx context.Context) error

func (c *Client) Execute[T any](ctx context.Context, name string, task func() (T, error), opts ...ExecuteOption) (T, error)

func (c *Client) Close() error

func (c *Client) Stats() SDKStats
```

- `Ready` blocks until SSE handshake completes (ensures state is synced before taking traffic)
- `Execute` is the primary public method
- `Close` is required for graceful shutdown
- `Stats` returns SDK health metrics
- `Report` is internal, called automatically by `Execute`
- `IsAllowed` is internal, called by `Execute`
- Add public `Report` later if users need escape hatch for async/batch workflows

### Errors

```go
var (
    ErrOpen = errors.New("tripswitch: breaker is open")
)

// Helper to check for any breaker-related error
func IsBreakerError(err error) bool {
    return errors.Is(err, ErrOpen)
}
```

`ErrOpen` is returned for both fully-open breakers and throttled requests in half-open state. Throttle events are logged separately via the configured logger.

Users handle breaker-open via standard Go error checking:

```go
result, err := ts.Execute(ctx, "checkout", task)
if errors.Is(err, tripswitch.ErrOpen) {
    // handle breaker open - return cached value, degrade gracefully, etc.
}

// Or use the helper (useful if we add more error types later)
if tripswitch.IsBreakerError(err) {
    // handle any breaker-related error
}
```

### SDK Stats

```go
type SDKStats struct {
    DroppedSamples      uint64    // samples dropped due to buffer overflow
    BufferSize          int       // current buffer occupancy
    SSEConnected        bool      // SSE connection status
    SSEReconnects       uint64    // count of SSE reconnections (detects network instability)
    LastSuccessfulFlush time.Time // detects if ingest is failing silently
}

func (c *Client) Stats() SDKStats
```

---

## Ready

Blocks until SSE handshake completes and initial state is synced.

### Why This Matters

On `NewClient()`, the client has an empty cache. If `Execute()` is called before SSE syncs, it will fail-open (allow traffic) because no state exists yet. `Ready()` ensures the app doesn't take traffic until breaker state is known.

### Signature

```go
func (c *Client) Ready(ctx context.Context) error
```

### Implementation

```go
func (c *Client) Ready(ctx context.Context) error {
    select {
    case <-c.sseReady: // closed when initial SSE handshake completes
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

### Usage

```go
ts := tripswitch.NewClient("proj_abc123", ...)
defer ts.Close()

// Wait for state sync before taking traffic
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
if err := ts.Ready(ctx); err != nil {
    log.Fatal("tripswitch failed to initialize:", err)
}

// Now safe to start HTTP server
http.ListenAndServe(":8080", handler)
```

---

## Execute

Primary API. Wraps task execution with circuit breaker logic.

### Signature

```go
func (c *Client) Execute[T any](ctx context.Context, name string, task func() (T, error), opts ...ExecuteOption) (T, error)
```

### Parameters

| Param      | Type                    | Description                              |
|------------|-------------------------|------------------------------------------|
| `ctx`      | `context.Context`       | For cancellation/timeouts                |
| `name`     | `string`                | Breaker name (unique within project)     |
| `task`     | `func() (T, error)`     | The operation to execute                 |
| `opts`     | `...ExecuteOption`      | Optional: error evaluator, etc.          |

### Execute Options

```go
// Errors to ignore (don't count as failures)
tripswitch.WithIgnoreErrors(sql.ErrNoRows, redis.Nil)

// Custom error evaluator
tripswitch.WithErrorEvaluator(func(err error) bool {
    // return true if this error should count as a failure
    return !errors.Is(err, sql.ErrNoRows)
})

// Diagnostic tags (for debugging trips)
tripswitch.WithTags(map[string]string{
    "tier":     "premium",
    "endpoint": "/api/checkout",
})

// Override TraceID (takes precedence over WithTraceIDExtractor)
tripswitch.WithTraceID("abc123")
```

### TraceID Resolution

TraceID is resolved in the following order:
1. `WithTraceID` option on `Execute` call (if provided)
2. `WithTraceIDExtractor` callback on client (if configured)
3. Empty string (if neither is set)

### Behavior

| Breaker State | Action                                      |
|---------------|---------------------------------------------|
| `closed`      | Run task, auto-report result                |
| `open`        | Return `ErrOpen` immediately                |
| `half_open`   | Throttle (allow X%), return `ErrOpen` if throttled, report if task runs |

### Context Handling

- If `ctx` is canceled before task runs, return `ctx.Err()` immediately
- Do not check breaker or run task if context is already done

### Auto-Reporting

- SDK infers `ok` from error (nil = success), respecting `WithIgnoreErrors`
- SDK sends `value: 1.0` as default (sufficient for ok/fail breakers)
- User gets their actual return type `T` back

### Use Case Fit

Execute is designed for **ok/fail breakers** (error_rate, consecutive_failures):
- Wrap operation, track success/failure, open breaker on threshold

For **metric breakers** (avg, p95, latency, etc.), samples come from:
- Middleware (HTTP, gRPC) that measures latency
- Observability pipelines (OpenTelemetry, Prometheus)
- Direct Report calls (escape hatch, add to public API if needed)

### Half-Open Throttling

- Server pushes `allow_rate` with state update (e.g., 0.2 = 20%)
- SDK rolls the dice locally to decide allow/deny
- If throttled, return `ErrOpen` (logged as throttle event)
- Allows gradual traffic recovery

---

## Close

Required for graceful shutdown.

```go
func (c *Client) Close() error
```

### Behavior

1. Cancel internal context (signals SSE and flusher goroutines to stop)
2. Wait for in-flight `sendBatch` calls to complete (via WaitGroup)
3. Clean up resources

### Implementation

```go
type Client struct {
    ctx        context.Context
    cancel     context.CancelFunc
    wg         sync.WaitGroup
    closeOnce  sync.Once
    // ...
}

func (c *Client) Close() error {
    var err error
    c.closeOnce.Do(func() {
        // Signal all goroutines to stop
        c.cancel()

        // Wait for flusher AND all sendBatch workers
        done := make(chan struct{})
        go func() {
            c.wg.Wait()
            close(done)
        }()

        select {
        case <-done:
            err = nil
        case <-time.After(5 * time.Second):
            err = errors.New("tripswitch: close timed out waiting for flush")
        }
    })
    return err
}
```

### Usage

```go
ts := tripswitch.NewClient("proj_abc123", ...)
defer ts.Close()
```

---

## Report (Internal)

Internal sample reporting, called by `Execute`. Fire-and-forget.

### Signature

```go
func (c *Client) report(entry reportEntry)
```

The `reportEntry` struct (see Buffer Implementation) includes all fields: `Name`, `OK`, `Value`, `TraceID`, `Tags`, `Timestamp`.

### Behavior

- Fire-and-forget: no return value, no blocking
- Errors surface at flush time (logged via configured logger)

### Buffer Spec

| Property        | Value                                               |
|-----------------|-----------------------------------------------------|
| Strategy        | Buffered channel (10,000 capacity)                  |
| Flush triggers  | 15s interval OR 500 samples (whichever first)       |
| Overflow        | Non-blocking send; drop + increment `DroppedSamples` counter |
| Flush failure   | Exponential backoff (100ms start), max 3 retries, then drop + log |
| Transport       | JSON payload, GZIP compressed                       |
| Header          | `Content-Encoding: gzip`                            |

### Buffer Implementation

```go
type reportEntry struct {
    Name      string
    OK        bool
    Value     float64
    TraceID   string            // from context extractor or WithTraceID option
    Tags      map[string]string // diagnostic metadata from WithTags option
    Timestamp time.Time
}

type Client struct {
    reportChan     chan reportEntry
    traceExtractor func(context.Context) string // set via WithTraceIDExtractor
    globalTags     map[string]string            // set via WithGlobalTags (read-only after init)
    stats          struct {
        DroppedSamples uint64
    }
}

func (c *Client) report(entry reportEntry) {
    select {
    case c.reportChan <- entry:
        // sent successfully
    default:
        // channel full, drop and count
        atomic.AddUint64(&c.stats.DroppedSamples, 1)
    }
}
```

### Batch Flusher

Background worker goroutine reads from channel and batches for flush. Owns the batch slice entirely - no mutexes needed.

**Critical**: Flusher itself is tracked in WaitGroup to prevent race condition where `wg.Wait()` resolves before final flush starts.

```go
func (c *Client) startFlusher(ctx context.Context) {
    c.wg.Add(1)        // Flusher itself is a tracked task
    defer c.wg.Done()

    // Local buffer for batching - owned by this goroutine
    batch := make([]reportEntry, 0, 500)

    // Timer for the 15s interval
    ticker := time.NewTicker(15 * time.Second)
    defer ticker.Stop()

    flush := func() {
        if len(batch) == 0 {
            return
        }
        // IMPORTANT: Add to WaitGroup BEFORE spawning goroutine
        c.wg.Add(1)
        go func(b []reportEntry) {
            defer c.wg.Done()
            c.sendBatch(b)
        }(batch)
        batch = make([]reportEntry, 0, 500)
        ticker.Reset(15 * time.Second)
    }

    for {
        select {
        case <-ctx.Done():
            flush() // Final drain on shutdown
            return
        case entry := <-c.reportChan:
            batch = append(batch, entry)
            if len(batch) >= 500 {
                flush()
            }
        case <-ticker.C:
            flush()
        }
    }
}

func (c *Client) sendBatch(batch []reportEntry) {
    // 1. Marshal to JSON
    // 2. GZIP compress
    // 3. POST to ingest endpoint
    // 4. On failure: exponential backoff (100ms, 400ms, 1s), max 3 retries
    // 5. If still failing: log error, increment DroppedSamples by len(batch)
}
```

### Why This Works (Go Idioms)

1. **Ownership of Data**: `batch` slice owned entirely by flusher goroutine - no mutexes needed
2. **Select Pattern**: Gracefully handles race between "buffer full" and "time up"
3. **Graceful Shutdown**: Context cancellation triggers final `flush()` before exit
4. **Non-blocking I/O**: `sendBatch` runs in separate goroutine to not block channel consumption
5. **Backpressure**: Buffered channel + default case protects app memory if network is slow

### Name Resolution

- SDK sends breaker name in payload
- Server resolves name → ID using `(project_id, name)` index
- Requires: add index on breaker name within project scope

---

## State Sync (SSE)

SDK maintains local cache of breaker states via Server-Sent Events.

### Local Cache Structure

```go
type breakerState struct {
    State     string  // "open", "closed", "half_open"
    AllowRate float64 // 0.0 to 1.0 (for half_open throttling)
}
```

### Connection

- SDK opens SSE connection to server on init
- Dedicated goroutine for SSE listener
- Use `select` to handle `Close()` signal and incoming stream
- Server pushes state changes as they occur
- SDK updates local cache

### Payload

```json
{
  "breaker": "checkout-latency",
  "state": "open",
  "allow_rate": 0.2
}
```

### Benefits

- Real-time state updates (no polling delay)
- Fast `Execute` checks (local cache lookup, no network call)
- Works behind firewalls (client-initiated connection)

### Failure Handling

- SSE disconnect: auto-reconnect with exponential backoff
- During disconnect: fail-open (allow traffic) by default, configurable

---

## IsAllowed (Internal)

Not public. Used internally by `Execute`.

### Implementation

```go
func (c *Client) isAllowed(name string) bool {
    c.mu.RLock()
    state, exists := c.cache[name]
    c.mu.RUnlock()

    if !exists || state.State == "closed" {
        return true
    }
    if state.State == "open" {
        return false
    }
    if state.State == "half_open" {
        // Local dice roll using math/rand/v2 (Go 1.22+)
        allowed := rand.Float64() < state.AllowRate
        if !allowed {
            c.logger.Debug("request throttled in half-open", "breaker", name)
        }
        return allowed
    }
    return true // Fail-open default
}
```

---

## Implementation Notes (Go-specific)

### Goroutine Safety

- All public methods must be safe for concurrent use
- Use `sync.RWMutex` for state cache access
- Use buffered channels for report buffer

### Execute Implementation

```go
func (c *Client) Execute[T any](ctx context.Context, name string, task func() (T, error), opts ...ExecuteOption) (T, error) {
    // 1. Context check (standard Go behavior)
    if err := ctx.Err(); err != nil {
        return *new(T), err
    }

    // 2. Local State Check (from SSE cache)
    if !c.isAllowed(name) {
        return *new(T), ErrOpen
    }

    // 3. Collect Options (tags, overrides)
    cfg := executeConfig{}
    for _, opt := range opts {
        opt(&cfg)
    }

    // 4. Run Task
    startTime := time.Now()
    result, err := task()

    // 5. Determine Failure (respecting custom evaluators)
    ok := !c.isFailure(err, cfg)

    // 6. Resolve First-Class TraceID
    traceID := cfg.traceID
    if traceID == "" && c.traceExtractor != nil {
        traceID = c.traceExtractor(ctx)
    }

    // 7. Fire-and-Forget Report
    c.report(reportEntry{
        Name:      name,
        OK:        ok,
        Value:     1.0,
        TraceID:   traceID,
        Tags:      c.mergeTags(cfg.tags),
        Timestamp: startTime,
    })

    return result, err
}
```

**Why this is correct:**

- **TraceID Resolution:** Checks call-specific option first, then falls back to Client's context extractor
- **Performance:** Captures `startTime` immediately for accurate timestamps
- **Separation of Concerns:** `isFailure` handles error logic, `isAllowed` handles SSE/throttling, `report` handles async batching
- **Generics:** Uses `*new(T)` to return zero-value of generic type on error

### Tag Merging (High-Performance)

```go
func (c *Client) mergeTags(dynamic map[string]string) map[string]string {
    // Optimization: If no dynamic tags, use global tags directly
    if len(dynamic) == 0 {
        return c.globalTags
    }

    // Optimization: If no global tags, use dynamic tags directly
    if len(c.globalTags) == 0 {
        return dynamic
    }

    // Only allocate a new map if we truly have to merge two sources
    merged := make(map[string]string, len(c.globalTags)+len(dynamic))
    for k, v := range c.globalTags {
        merged[k] = v
    }
    for k, v := range dynamic {
        merged[k] = v // dynamic wins on conflict
    }
    return merged
}
```

**Why this matters:**

- **Allocation Avoidance:** Only allocates if both global and dynamic tags exist (critical for high-throughput)
- **Pre-sized Map:** Uses `make(map, size)` to prevent re-growth/re-hashing
- **Thread Safety:** `c.globalTags` is read-only after init, no mutex needed

### Error Evaluation (isFailure)

Determines whether an error should count as a failure for breaker purposes.

**Important**: `WithErrorEvaluator` takes precedence over `WithIgnoreErrors`. If both are provided, only the evaluator is used. This allows users to handle complex cases like ignoring entire classes of errors (e.g., all 4xx HTTP codes).

```go
type executeConfig struct {
    ignoreErrors   []error
    errorEvaluator func(error) bool
    traceID        string
    tags           map[string]string
}

func (c *Client) isFailure(err error, cfg executeConfig) bool {
    // No error = not a failure
    if err == nil {
        return false
    }

    // Custom evaluator takes precedence (overrides ignore list)
    if cfg.errorEvaluator != nil {
        return cfg.errorEvaluator(err)
    }

    // Check ignore list
    for _, ignored := range cfg.ignoreErrors {
        if errors.Is(err, ignored) {
            return false
        }
    }

    // Default: any error is a failure
    return true
}
```

### SSE Goroutine

```go
func (c *Client) runSSE(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case event := <-c.sseEvents:
            c.updateState(event)
            if c.onStateChange != nil {
                c.onStateChange(event.Name, event.OldState, event.NewState)
            }
        }
    }
}
```

### Logging Interface

```go
// Logger interface - compatible with slog.Logger
type Logger interface {
    Debug(msg string, args ...any)
    Info(msg string, args ...any)
    Warn(msg string, args ...any)
    Error(msg string, args ...any)
}
```

---

## Target Go Version

**Go 1.22+** required. This allows use of `math/rand/v2` which is thread-safe and faster (no global locked source like old `math/rand`).

---

## Next Steps to Ship

1. Initialize `reportChan` with 10,000 buffer capacity
2. Write "gold-standard" examples for README:
   - Standard HTTP client wrap
   - Database query wrap with `sql.ErrNoRows` ignored
   - Graceful shutdown in `main.go` using `os.Signal`
3. Implement SSE endpoint on server side
4. Add breaker name index to database (`project_id`, `name`)

---

## Implementation Invariants

These invariants MUST hold in the implementation:

### TraceID Resolution
- `Execute` MUST resolve `TraceID` from `Context` using the configured `traceExtractor` if not explicitly provided via `WithTraceID`
- Priority: `WithTraceID` option > `WithTraceIDExtractor` callback > empty string

### Tag Merging
- `Tags` from `WithTags` (call-site) MUST be merged with `globalTags` (client-level) before the sample is queued
- On key conflict, **dynamic tags win** (call-site overrides global)
- `globalTags` is read-only after client initialization

### Performance Properties
- `globalTags` is stored on the `Client` struct as `map[string]string`
- Dynamic tags are stored in the `executeConfig` struct
- `mergeTags` MUST avoid allocation when only one tag source exists

---

## Open Questions

*None remaining - spec is implementation-ready.*
