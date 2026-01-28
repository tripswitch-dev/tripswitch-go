// Package tripswitch provides the official Go client SDK for Tripswitch,
// a circuit breaker management service.
//
// The client maintains real-time circuit breaker state via Server-Sent Events (SSE)
// and automatically reports execution samples to the Tripswitch API. It is goroutine-safe
// and designed for high-throughput applications.
//
// # Authentication
//
// The runtime client uses two credentials:
//   - Project API key (eb_pk_...): For SSE subscriptions and state reads
//   - Ingest secret: For HMAC-signed sample ingestion
//
// Create project keys via the admin API or Tripswitch dashboard.
//
// # Quick Start
//
//	ts := tripswitch.NewClient("proj_abc123",
//	    tripswitch.WithAPIKey("eb_pk_..."),   // project key for SSE
//	    tripswitch.WithIngestSecret("..."),   // 64-char hex string for HMAC
//	)
//	defer ts.Close(context.Background())
//
//	// Wait for state sync before taking traffic
//	if err := ts.Ready(ctx); err != nil {
//	    log.Fatal(err)
//	}
//
//	// Wrap operations with circuit breaker
//	resp, err := tripswitch.Execute(ts, ctx, "router-id", func() (*http.Response, error) {
//	    return client.Do(req)
//	},
//	    tripswitch.WithBreakers("external-api"),          // Gating: block if breaker is open
//	    tripswitch.WithMetric("latency", tripswitch.Latency),  // Report latency in ms
//	)
//
// # Circuit Breaker States
//
// The SDK handles three breaker states:
//   - closed: All requests allowed, results reported
//   - open: All requests rejected with [ErrOpen]
//   - half_open: Requests throttled based on allow_rate (probabilistic)
//
// # Error Handling
//
// Use [IsBreakerError] to check if an error is circuit breaker related:
//
//	if tripswitch.IsBreakerError(err) {
//	    // Return cached/fallback response
//	}
//
// # Graceful Shutdown
//
// Always call [Client.Close] to flush buffered samples:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	ts.Close(ctx)
package tripswitch

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/r3labs/sse/v2"
)

// breakerState holds the current state of a circuit breaker.
type breakerState struct {
	State     string  // "open", "closed", "half_open"
	AllowRate float64 // 0.0 to 1.0
}

// latencyMarker is the unexported type for the Latency sentinel.
// When used with WithMetric, the SDK automatically computes task duration in milliseconds.
type latencyMarker struct{}

// Latency is a sentinel value for WithMetric that instructs the SDK to
// automatically compute and report task duration in milliseconds.
//
// Example:
//
//	tripswitch.Execute(c, ctx, "router-id", task,
//	    tripswitch.WithMetric("latency", tripswitch.Latency),
//	)
var Latency = &latencyMarker{}

// reportEntry represents a single sample to be reported.
type reportEntry struct {
	RouterID string            `json:"router_id"`
	Metric   string            `json:"metric"`
	TsMs     int64             `json:"ts_ms"`
	Value    float64           `json:"value"`
	OK       bool              `json:"ok"`
	Tags     map[string]string `json:"tags,omitempty"`
	TraceID  string            `json:"trace_id,omitempty"`
}

// batchPayload is the wire format for sending samples to the ingest endpoint.
type batchPayload struct {
	Samples []reportEntry `json:"samples"`
}

// Client is the main tripswitch client.
type Client struct {
	projectID    string
	apiKey       string
	ingestSecret string // 64-char hex string for HMAC signing
	failOpen     bool
	baseURL        string
	logger         Logger
	onStateChange  func(name, from, to string)
	traceExtractor func(ctx context.Context) string
	globalTags     map[string]string

	// for graceful shutdown
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once

	// for stats
	stats struct {
		// a lock for the stats since they are updated concurrently
		mu                  sync.RWMutex
		droppedSamples      uint64
		bufferSize          int
		sseConnected        bool
		sseReconnects       uint64
		lastSuccessfulFlush time.Time
	}

	// for SSE state sync
	breakerStates   map[string]breakerState
	breakerStatesMu sync.RWMutex
	sseReady        chan struct{} // Closed when initial SSE sync is complete
	sseClient       *sse.Client
	sseEventURL     string

	// for report buffering
	reportChan     chan reportEntry
	droppedSamples uint64 // atomic counter for dropped samples
	httpClient     *http.Client
}

// Option is a functional option for configuring the client.
type Option func(*Client)

// NewClient creates a new tripswitch client.
func NewClient(projectID string, opts ...Option) *Client {
	// Default client
	c := &Client{
		projectID:     projectID,
		failOpen:      true,
		baseURL:       "https://api.tripswitch.dev",
		logger:        slog.Default(),
		breakerStates: make(map[string]breakerState),
		sseReady:      make(chan struct{}),
		reportChan:    make(chan reportEntry, 10000),
		httpClient:    &http.Client{Timeout: 30 * time.Second},
	}

	for _, opt := range opts {
		opt(c)
	}

	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.sseEventURL = c.baseURL + "/v1/projects/" + c.projectID + "/breakers/state:stream"

	go c.startSSEListener()
	go c.startFlusher()

	return c
}

// Logger interface - compatible with slog.Logger
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// SDKStats holds health metrics about the SDK.
type SDKStats struct {
	DroppedSamples      uint64
	BufferSize          int
	SSEConnected        bool
	SSEReconnects       uint64
	LastSuccessfulFlush time.Time
}

// Stats returns a snapshot of the SDK's health metrics.
func (c *Client) Stats() SDKStats {
	c.stats.mu.RLock()
	defer c.stats.mu.RUnlock()

	return SDKStats{
		DroppedSamples:      c.stats.droppedSamples,
		BufferSize:          c.stats.bufferSize,
		SSEConnected:        c.stats.sseConnected,
		SSEReconnects:       c.stats.sseReconnects,
		LastSuccessfulFlush: c.stats.lastSuccessfulFlush,
	}
}

// WithAPIKey sets the project API key for SSE subscriptions and state reads.
// Project keys have the prefix "eb_pk_" and are project-scoped.
func WithAPIKey(key string) Option {
	return func(c *Client) {
		c.apiKey = key
	}
}

// WithIngestSecret sets the ingest secret for HMAC-signed sample ingestion.
// The secret is a 64-character hex string used to sign requests to the metrics endpoint.
func WithIngestSecret(secret string) Option {
	return func(c *Client) {
		c.ingestSecret = secret
	}
}

// WithIngestKey is deprecated: use [WithIngestSecret] instead.
// This function exists for backwards compatibility.
func WithIngestKey(key string) Option {
	return WithIngestSecret(key)
}

// WithFailOpen sets the fail-open behavior of the client.
// If true, the client will allow traffic when tripswitch is unreachable.
// Defaults to true.
func WithFailOpen(failOpen bool) Option {
	return func(c *Client) {
		c.failOpen = failOpen
	}
}

// WithBaseURL overrides the default base URL for the Tripswitch API.
func WithBaseURL(url string) Option {
	return func(c *Client) {
		c.baseURL = url
	}
}

// WithLogger sets a custom logger for the client.
func WithLogger(logger Logger) Option {
	return func(c *Client) {
		c.logger = logger
	}
}

// WithOnStateChange sets a callback that is invoked whenever a breaker's
// state changes.
func WithOnStateChange(f func(name, from, to string)) Option {
	return func(c *Client) {
		c.onStateChange = f
	}
}

// WithTraceIDExtractor sets a function to extract a trace ID from a context.
// This is used to associate samples with a specific trace.
func WithTraceIDExtractor(f func(ctx context.Context) string) Option {
	return func(c *Client) {
		c.traceExtractor = f
	}
}

// WithGlobalTags sets tags that will be applied to all samples reported
// by the client.
func WithGlobalTags(tags map[string]string) Option {
	return func(c *Client) {
		if tags != nil {
			c.globalTags = make(map[string]string, len(tags))
			for k, v := range tags {
				c.globalTags[k] = v
			}
		}
	}
}

// executeOptions holds per-call configuration for Execute.
type executeOptions struct {
	breakers       []string              // Gating: breaker names to check
	metrics        map[string]any        // Keys are metric names, values are markers/closures/primitives
	tags           map[string]string     // Diagnostic breadcrumbs
	ignoreErrors   []error               // Errors that don't count as failures
	errorEvaluator func(error) bool      // Custom OK logic
	traceID        string                // Correlation ID
}

// defaultOptions returns an executeOptions with initialized maps.
func defaultOptions() *executeOptions {
	return &executeOptions{
		metrics: make(map[string]any),
		tags:    make(map[string]string),
	}
}

// ExecuteOption configures a single Execute call.
type ExecuteOption func(*executeOptions)

// WithBreakers specifies breaker names to check before executing the task.
// If ANY breaker is open, Execute returns ErrOpen without running the task.
// This makes gating opt-in - if not specified, no breaker check is performed.
func WithBreakers(names ...string) ExecuteOption {
	return func(opts *executeOptions) {
		opts.breakers = append(opts.breakers, names...)
	}
}

// WithMetric adds a metric to be reported with this Execute call.
// The value can be:
//   - Latency: SDK computes task duration in milliseconds
//   - func() float64: User closure called after task completes
//   - int or float64: Static numeric value
//
// Multiple metrics can be added by calling WithMetric multiple times or using WithMetrics.
// Empty keys are ignored.
func WithMetric(key string, value any) ExecuteOption {
	return func(opts *executeOptions) {
		if key == "" {
			return // Ignore empty keys
		}
		opts.metrics[key] = value
	}
}

// WithMetrics adds multiple metrics to be reported with this Execute call.
// See WithMetric for supported value types.
func WithMetrics(metrics map[string]any) ExecuteOption {
	return func(opts *executeOptions) {
		for k, v := range metrics {
			opts.metrics[k] = v
		}
	}
}

// WithTag adds a single diagnostic tag for this Execute call.
// Tags are merged with global tags, with call-site tags taking precedence.
func WithTag(key, value string) ExecuteOption {
	return func(opts *executeOptions) {
		opts.tags[key] = value
	}
}

// WithTags sets diagnostic tags for this specific Execute call.
// These are merged with global tags, with call-site tags taking precedence.
func WithTags(tags map[string]string) ExecuteOption {
	return func(opts *executeOptions) {
		for k, v := range tags {
			opts.tags[k] = v
		}
	}
}

// WithIgnoreErrors specifies errors that should not count as failures.
func WithIgnoreErrors(errs ...error) ExecuteOption {
	return func(opts *executeOptions) {
		opts.ignoreErrors = errs
	}
}

// WithErrorEvaluator sets a custom function to determine if an error is a failure.
// If set, this takes precedence over WithIgnoreErrors.
// Return true if the error should count as a failure.
func WithErrorEvaluator(f func(error) bool) ExecuteOption {
	return func(opts *executeOptions) {
		opts.errorEvaluator = f
	}
}

// WithTraceID sets a specific trace ID for this Execute call.
// This takes precedence over the client's TraceIDExtractor.
func WithTraceID(traceID string) ExecuteOption {
	return func(opts *executeOptions) {
		opts.traceID = traceID
	}
}

// resolveMetrics converts the metrics map to a slice of samples.
// It handles latency markers, closures, and numeric primitive values.
// Closures that panic are recovered and logged, and that metric is skipped.
//
// Note: Map iteration order is non-deterministic, so samples may be returned
// in any order. This is fine since samples are independent.
func (opts *executeOptions) resolveMetrics(c *Client, duration time.Duration) []reportEntry {
	if len(opts.metrics) == 0 {
		return nil
	}

	samples := make([]reportEntry, 0, len(opts.metrics))
	for key, val := range opts.metrics {
		var resolvedValue float64
		var skip bool

		switch v := val.(type) {
		case *latencyMarker:
			resolvedValue = float64(duration.Milliseconds())
		case func() float64:
			// Recover from panics - don't let one bad closure kill all telemetry
			func() {
				defer func() {
					if r := recover(); r != nil {
						c.logger.Warn("metric closure panicked", "metric", key, "panic", r)
						skip = true
					}
				}()
				resolvedValue = v()
			}()
		// Signed integers
		case int:
			resolvedValue = float64(v)
		case int8:
			resolvedValue = float64(v)
		case int16:
			resolvedValue = float64(v)
		case int32:
			resolvedValue = float64(v)
		case int64:
			resolvedValue = float64(v)
		// Unsigned integers
		case uint:
			resolvedValue = float64(v)
		case uint8:
			resolvedValue = float64(v)
		case uint16:
			resolvedValue = float64(v)
		case uint32:
			resolvedValue = float64(v)
		case uint64:
			resolvedValue = float64(v)
		// Floats
		case float32:
			resolvedValue = float64(v)
		case float64:
			resolvedValue = v
		default:
			c.logger.Warn("unsupported metric value type", "metric", key, "type", fmt.Sprintf("%T", val))
			skip = true
		}

		if !skip {
			samples = append(samples, reportEntry{
				Metric: key,
				Value:  resolvedValue,
			})
		}
	}
	return samples
}

// ContractVersion declares the SDK Contract version this implementation conforms to.
// See https://app.tripswitch.dev/docs/sdk-contract.md
const ContractVersion = "0.2"

var (
	// ErrOpen is returned by Execute when the circuit breaker is open.
	ErrOpen = errors.New("tripswitch: breaker is open")
)

// IsBreakerError returns true if the error is a circuit breaker error.
func IsBreakerError(err error) bool {
	return errors.Is(err, ErrOpen)
}

// Close gracefully shuts down the client, flushing any buffered samples.
// The provided context controls how long to wait for the flush to complete.
func (c *Client) Close(ctx context.Context) error {
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
			// finished gracefully
		case <-ctx.Done():
			err = fmt.Errorf("tripswitch: close interrupted waiting for flush: %w", ctx.Err())
		}
	})
	return err
}

// sseBreakerEvent represents the JSON payload of an SSE event for a breaker state change.
type sseBreakerEvent struct {
	Breaker   string  `json:"breaker"`
	State     string  `json:"state"`
	AllowRate float64 `json:"allow_rate"`
}

// startSSEListener connects to the SSE endpoint, listens for breaker state changes,
// and updates the local cache. It handles reconnections and ensures graceful shutdown.
func (c *Client) startSSEListener() {
	c.wg.Add(1)
	defer c.wg.Done()

	c.sseClient = sse.NewClient(c.sseEventURL)

	// Force HTTP/1.1 for SSE - HTTP/2 has issues with streaming in some configurations
	c.sseClient.Connection = &http.Client{
		Transport: &http.Transport{
			ForceAttemptHTTP2: false,
			TLSNextProto:      make(map[string]func(authority string, c *tls.Conn) http.RoundTripper),
		},
	}

	// Set up Authorization header
	if c.apiKey != "" {
		c.sseClient.Headers["Authorization"] = "Bearer " + c.apiKey
	}

	// The sse client library handles reconnects automatically when SubscribeWithContext is called.

	// Handler for SSE events
	handler := func(msg *sse.Event) {
		var event sseBreakerEvent
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			c.logger.Error("failed to unmarshal SSE event", "error", err, "data", string(msg.Data))
			return
		}
		c.updateBreakerState(event.Breaker, event.State, event.AllowRate)

		// Mark SSE as connected
		c.stats.mu.Lock()
		c.stats.sseConnected = true
		c.stats.mu.Unlock()

		// Signal that initial sync is complete
		// This needs to be done carefully to avoid closing a closed channel
		select {
		case <-c.sseReady:
			// Already closed
		default:
			close(c.sseReady)
		}
	}

	// This blocks until c.ctx is cancelled or connection fails permanently after retries
	// Note: empty string avoids query param and receives all events without filtering
	c.logger.Debug("Attempting to subscribe to SSE stream", "url", c.sseEventURL)
	err := c.sseClient.SubscribeWithContext(c.ctx, "", handler)
	if err != nil && err != context.Canceled { // Ignore context cancellation error
		c.logger.Error("SSE client subscription ended with error", "error", err)

		// Increment reconnects if the subscription failed unexpectedly
		c.stats.mu.Lock()
		c.stats.sseReconnects++
		c.stats.sseConnected = false // Indicate connection lost
		c.stats.mu.Unlock()
	}

	c.logger.Debug("SSE listener shutting down.")
}

func (c *Client) updateBreakerState(name, newState string, allowRate float64) {
	c.breakerStatesMu.Lock()
	defer c.breakerStatesMu.Unlock()

	oldState := ""
	if existing, ok := c.breakerStates[name]; ok {
		oldState = existing.State
	}

	c.breakerStates[name] = breakerState{
		State:     newState,
		AllowRate: allowRate,
	}

	c.logger.Info("Breaker state updated", "name", name, "oldState", oldState, "newState", newState, "allowRate", allowRate)

	// Invoke callback if state changed and it's set
	if oldState != "" && oldState != newState && c.onStateChange != nil {
		c.onStateChange(name, oldState, newState)
	}
}

// Ready blocks until the initial SSE handshake completes and state is synced.
func (c *Client) Ready(ctx context.Context) error {
	select {
	case <-c.sseReady:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Execute wraps a task with circuit breaker logic.
// It runs the task and reports samples to the specified router.
// This is a package-level generic function because Go does not support generic methods.
//
// The routerID is the routing key for samples - it determines where data goes.
// Use WithBreakers to optionally gate execution on breaker state.
// Use WithMetric/WithMetrics to specify what values to report.
//
// Example:
//
//	result, err := tripswitch.Execute(c, ctx, "checkout-router", task,
//	    tripswitch.WithBreakers("checkout-error-rate"),
//	    tripswitch.WithMetric("latency", tripswitch.Latency),
//	    tripswitch.WithTag("endpoint", "/checkout"),
//	)
func Execute[T any](c *Client, ctx context.Context, routerID string, task func() (T, error), opts ...ExecuteOption) (T, error) {
	var zero T

	// 1. Check context cancelled
	if err := ctx.Err(); err != nil {
		return zero, err
	}

	// 2. Apply options to executeOptions (with defaultOptions())
	execOpts := defaultOptions()
	for _, opt := range opts {
		opt(execOpts)
	}

	// 3. If breakers specified, check using min allow_rate policy
	minAllowRate := 1.0
	c.breakerStatesMu.RLock()
	for _, name := range execOpts.breakers {
		state, exists := c.breakerStates[name]
		if !exists {
			continue
		}
		switch state.State {
		case "open":
			c.breakerStatesMu.RUnlock()
			return zero, ErrOpen
		case "half_open":
			if state.AllowRate < minAllowRate {
				minAllowRate = state.AllowRate
			}
		}
	}
	c.breakerStatesMu.RUnlock()
	if minAllowRate < 1.0 && rand.Float64() >= minAllowRate {
		c.logger.Debug("request throttled in half-open", "minAllowRate", minAllowRate)
		return zero, ErrOpen
	}

	// 4. Capture start time
	startTime := time.Now()

	// 5. Run task
	result, err := task()

	// 6. Compute duration
	duration := time.Since(startTime)

	// 7. Determine OK via isFailure
	ok := !c.isFailure(err, execOpts)

	// 8. Resolve TraceID: option > extractor > empty
	traceID := execOpts.traceID
	if traceID == "" && c.traceExtractor != nil {
		traceID = c.traceExtractor(ctx)
	}

	// 9. Resolve metrics → []sample
	samples := execOpts.resolveMetrics(c, duration)

	// 10. Build and enqueue reportEntry per sample
	mergedTags := c.mergeTags(execOpts.tags)
	tsMs := startTime.UnixMilli()

	for i := range samples {
		samples[i].RouterID = routerID
		samples[i].OK = ok
		samples[i].TsMs = tsMs
		samples[i].Tags = mergedTags
		samples[i].TraceID = traceID

		// 11. Enqueue samples (fire-and-forget)
		c.report(samples[i])
	}

	// 12. Return result, err
	return result, err
}

// isFailure determines if an error should count as a failure.
func (c *Client) isFailure(err error, opts *executeOptions) bool {
	// No error = not a failure
	if err == nil {
		return false
	}

	// Custom evaluator takes precedence
	if opts.errorEvaluator != nil {
		return opts.errorEvaluator(err)
	}

	// Check ignore list
	for _, ignored := range opts.ignoreErrors {
		if errors.Is(err, ignored) {
			return false
		}
	}

	// Default: any error is a failure
	return true
}

// mergeTags merges global tags with dynamic tags.
// Dynamic tags override global tags on conflict.
func (c *Client) mergeTags(dynamic map[string]string) map[string]string {
	// Optimization: avoid allocation when possible
	if len(dynamic) == 0 {
		return c.globalTags
	}
	if len(c.globalTags) == 0 {
		return dynamic
	}

	// Merge with pre-sized map
	merged := make(map[string]string, len(c.globalTags)+len(dynamic))
	for k, v := range c.globalTags {
		merged[k] = v
	}
	for k, v := range dynamic {
		merged[k] = v // dynamic wins on conflict
	}
	return merged
}

// report sends a sample to the report buffer (fire-and-forget).
func (c *Client) report(entry reportEntry) {
	select {
	case c.reportChan <- entry:
		// sent successfully
	default:
		// channel full, drop and count
		atomic.AddUint64(&c.droppedSamples, 1)
	}
}

// startFlusher runs the background batch flusher.
func (c *Client) startFlusher() {
	c.wg.Add(1)
	defer c.wg.Done()

	batch := make([]reportEntry, 0, 500)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		// Add to WaitGroup before spawning goroutine
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
		case <-c.ctx.Done():
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

// sendBatch sends a batch of samples to the metrics ingest endpoint.
func (c *Client) sendBatch(batch []reportEntry) {
	if len(batch) == 0 {
		return
	}

	// Wrap in payload format (project_id is in header, not body)
	payload := batchPayload{
		Samples: batch,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		c.logger.Error("failed to marshal batch", "error", err)
		atomic.AddUint64(&c.droppedSamples, uint64(len(batch)))
		return
	}

	// GZIP compress
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(jsonData); err != nil {
		c.logger.Error("failed to gzip batch", "error", err)
		atomic.AddUint64(&c.droppedSamples, uint64(len(batch)))
		return
	}
	if err := gz.Close(); err != nil {
		c.logger.Error("failed to close gzip writer", "error", err)
		atomic.AddUint64(&c.droppedSamples, uint64(len(batch)))
		return
	}
	wireBytes := compressed.Bytes()

	// Generate timestamp for signing
	timestampMs := time.Now().UnixMilli()
	timestampStr := strconv.FormatInt(timestampMs, 10)

	// Compute HMAC signature on compressed bytes: sign "{timestamp_ms}.{compressed_body}"
	// This is the "sign what you send" approach - signature covers the wire bytes.
	var signature string
	if c.ingestSecret != "" {
		secretBytes, err := hex.DecodeString(c.ingestSecret)
		if err != nil {
			c.logger.Error("failed to decode ingest secret", "error", err)
			atomic.AddUint64(&c.droppedSamples, uint64(len(batch)))
			return
		}
		message := timestampStr + "." + string(wireBytes)
		mac := hmac.New(sha256.New, secretBytes)
		mac.Write([]byte(message))
		signature = "v1=" + hex.EncodeToString(mac.Sum(nil))
	}

	// Exponential backoff: 100ms, 400ms, 1s (max 3 retries)
	backoffs := []time.Duration{100 * time.Millisecond, 400 * time.Millisecond, 1 * time.Second}
	url := c.baseURL + "/v1/projects/" + c.projectID + "/ingest"

	for attempt := 0; attempt <= len(backoffs); attempt++ {
		// Don't retry if context is cancelled (shutdown in progress)
		if c.ctx.Err() != nil {
			c.logger.Debug("batch send cancelled due to shutdown", "remaining", len(batch))
			atomic.AddUint64(&c.droppedSamples, uint64(len(batch)))
			return
		}

		if attempt > 0 {
			select {
			case <-time.After(backoffs[attempt-1]):
			case <-c.ctx.Done():
				c.logger.Debug("batch send cancelled during backoff", "remaining", len(batch))
				atomic.AddUint64(&c.droppedSamples, uint64(len(batch)))
				return
			}
		}

		req, err := http.NewRequestWithContext(c.ctx, http.MethodPost, url, bytes.NewReader(wireBytes))
		if err != nil {
			c.logger.Error("failed to create request", "error", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Encoding", "gzip")

		// HMAC signature authentication (project is in URL path)
		req.Header.Set("X-EB-Timestamp", timestampStr)
		if signature != "" {
			req.Header.Set("X-EB-Signature", signature)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			// Don't log error if context was cancelled
			if c.ctx.Err() == nil {
				c.logger.Error("failed to send batch", "error", err, "attempt", attempt+1)
			}
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			c.stats.mu.Lock()
			c.stats.lastSuccessfulFlush = time.Now()
			c.stats.mu.Unlock()
			return // Success
		}

		c.logger.Error("batch request failed", "status", resp.StatusCode, "attempt", attempt+1)
	}

	// All retries exhausted
	c.logger.Error("dropping batch after retries exhausted", "count", len(batch))
	atomic.AddUint64(&c.droppedSamples, uint64(len(batch)))
}

// Status represents the project health status.
type Status struct {
	OpenCount   int   `json:"open_count"`
	ClosedCount int   `json:"closed_count"`
	LastEvalMs  int64 `json:"last_eval_ms,omitempty"`
}

// GetStatus retrieves the project health status from the API.
// This returns the count of open and closed breakers for the project.
// Requires a project API key (eb_pk_).
func (c *Client) GetStatus(ctx context.Context) (*Status, error) {
	url := c.baseURL + "/v1/projects/" + c.projectID + "/status"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("tripswitch: failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tripswitch: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tripswitch: unexpected status code: %d", resp.StatusCode)
	}

	var status Status
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("tripswitch: failed to decode response: %w", err)
	}

	return &status, nil
}
