// Package tripswitch provides the official Go client SDK for Tripswitch,
// a circuit breaker management service.
//
// The client maintains real-time circuit breaker state via Server-Sent Events (SSE)
// and automatically reports execution samples to the Tripswitch API. It is goroutine-safe
// and designed for high-throughput applications.
//
// # Quick Start
//
//	ts := tripswitch.NewClient("proj_abc123",
//	    tripswitch.WithAPIKey("sk_..."),
//	    tripswitch.WithIngestSecret("..."), // 64-char hex string
//	)
//	defer ts.Close(context.Background())
//
//	// Wait for state sync before taking traffic
//	if err := ts.Ready(ctx); err != nil {
//	    log.Fatal(err)
//	}
//
//	// Wrap operations with circuit breaker
//	resp, err := tripswitch.Execute(ts, ctx, "external-api", func() (*http.Response, error) {
//	    return client.Do(req)
//	})
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

// Breaker identifies a circuit breaker for use with [Execute].
// The RouterID and Metric fields are required for sample reporting.
// The Name field is used for state lookup (must match the breaker name from SSE).
type Breaker struct {
	RouterID string // Routing key for samples (from breaker config)
	Metric   string // Metric name for samples (from breaker config)
	Name     string // Breaker name for state lookup (matches SSE events)
}

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

// WithAPIKey sets the API key for the client.
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

// executeConfig holds per-call configuration for Execute.
type executeConfig struct {
	ignoreErrors   []error
	errorEvaluator func(error) bool
	traceID        string
	tags           map[string]string
}

// ExecuteOption configures a single Execute call.
type ExecuteOption func(*executeConfig)

// WithIgnoreErrors specifies errors that should not count as failures.
func WithIgnoreErrors(errs ...error) ExecuteOption {
	return func(cfg *executeConfig) {
		cfg.ignoreErrors = errs
	}
}

// WithErrorEvaluator sets a custom function to determine if an error is a failure.
// If set, this takes precedence over WithIgnoreErrors.
// Return true if the error should count as a failure.
func WithErrorEvaluator(f func(error) bool) ExecuteOption {
	return func(cfg *executeConfig) {
		cfg.errorEvaluator = f
	}
}

// WithTags sets diagnostic tags for this specific Execute call.
// These are merged with global tags, with call-site tags taking precedence.
func WithTags(tags map[string]string) ExecuteOption {
	return func(cfg *executeConfig) {
		cfg.tags = tags
	}
}

// WithTraceID sets a specific trace ID for this Execute call.
// This takes precedence over the client's TraceIDExtractor.
func WithTraceID(traceID string) ExecuteOption {
	return func(cfg *executeConfig) {
		cfg.traceID = traceID
	}
}

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
// It checks the breaker state, runs the task if allowed, and reports the result.
// This is a package-level generic function because Go does not support generic methods.
//
// The Breaker parameter must include RouterID, Metric, and Name fields:
//   - Name is used for state lookup (must match breaker name from SSE)
//   - RouterID and Metric are used for sample reporting
func Execute[T any](c *Client, ctx context.Context, b Breaker, task func() (T, error), opts ...ExecuteOption) (T, error) {
	var zero T

	// Check context first
	if err := ctx.Err(); err != nil {
		return zero, err
	}

	// Check breaker state using the breaker name
	if !c.isAllowed(b.Name) {
		return zero, ErrOpen
	}

	// Apply options
	cfg := executeConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	// Run task
	startTime := time.Now()
	result, err := task()

	// Determine if this is a failure
	ok := !c.isFailure(err, cfg)

	// Resolve TraceID: option > extractor > empty
	traceID := cfg.traceID
	if traceID == "" && c.traceExtractor != nil {
		traceID = c.traceExtractor(ctx)
	}

	// Fire-and-forget report using router_id and metric
	c.report(reportEntry{
		RouterID: b.RouterID,
		Metric:   b.Metric,
		TsMs:     startTime.UnixMilli(),
		Value:    1.0,
		OK:       ok,
		TraceID:  traceID,
		Tags:     c.mergeTags(cfg.tags),
	})

	return result, err
}

// isAllowed checks if a request to the named breaker should be allowed.
func (c *Client) isAllowed(name string) bool {
	c.breakerStatesMu.RLock()
	state, exists := c.breakerStates[name]
	c.breakerStatesMu.RUnlock()

	// Fail-open: if breaker not in cache, allow
	if !exists {
		return true
	}

	switch state.State {
	case "closed":
		return true
	case "open":
		return false
	case "half_open":
		// Throttle via dice roll
		allowed := rand.Float64() < state.AllowRate
		if !allowed {
			c.logger.Debug("request throttled in half-open", "breaker", name, "allowRate", state.AllowRate)
		}
		return allowed
	default:
		// Unknown state, fail-open
		return true
	}
}

// isFailure determines if an error should count as a failure.
func (c *Client) isFailure(err error, cfg executeConfig) bool {
	// No error = not a failure
	if err == nil {
		return false
	}

	// Custom evaluator takes precedence
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
	url := c.baseURL + "/v1/metrics"

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

		// HMAC signature authentication
		req.Header.Set("x-eb-project-id", c.projectID)
		req.Header.Set("x-eb-timestamp", timestampStr)
		if signature != "" {
			req.Header.Set("x-eb-signature", signature)
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
