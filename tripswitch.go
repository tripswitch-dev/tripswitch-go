package tripswitch

import (
	"context"
	"encoding/json" // Added for JSON unmarshalling
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/r3labs/sse/v2"
)

// breakerState holds the current state of a circuit breaker.
type breakerState struct {
	State     string  // "open", "closed", "half_open"
	AllowRate float64 // 0.0 to 1.0
}

// Client is the main tripswitch client.
type Client struct {
	projectID      string
	apiKey         string
	ingestKey      string
	failOpen       bool
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
	breakerStates     map[string]breakerState
	breakerStatesMu   sync.RWMutex
	sseReady          chan struct{} // Closed when initial SSE sync is complete
	sseClient         *sse.Client
	sseEventURL       string


}

// Option is a functional option for configuring the client.
type Option func(*Client)

// NewClient creates a new tripswitch client.
func NewClient(projectID string, opts ...Option) *Client {
	// Default client
	c := &Client{
		projectID: projectID,
		failOpen:  true,
		baseURL:   "https://api.tripswitch.dev",
		logger:    slog.Default(),
		breakerStates: make(map[string]breakerState),
		sseReady:      make(chan struct{}),
	}

	for _, opt := range opts {
		opt(c)
	}

	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.sseEventURL = c.baseURL + "/v1/projects/" + c.projectID + "/breakers/state:stream"

	go c.startSSEListener()

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

// WithIngestKey sets the ingest key for the client.
func WithIngestKey(key string) Option {
	return func(c *Client) {
		c.ingestKey = key
	}
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

var (
	// ErrOpen is returned by Execute when the circuit breaker is open.
	ErrOpen = errors.New("tripswitch: breaker is open")
)

// IsBreakerError returns true if the error is a circuit breaker error.
func IsBreakerError(err error) bool {
	return errors.Is(err, ErrOpen)
}

// Close gracefully shuts down the client, flushing any buffered samples.
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
			// finished gracefully
		case <-time.After(5 * time.Second):
			err = errors.New("tripswitch: close timed out waiting for flush")
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
	c.logger.Debug("Attempting to subscribe to SSE stream", "url", c.sseEventURL)
	err := c.sseClient.SubscribeWithContext(c.ctx, "messages", handler)
	if err != nil && err != context.Canceled { // Ignore context cancellation error
		c.logger.Error("SSE client subscription ended with error", "error", err)

		// If subscription fails, and failOpen is false, we should indicate connection is down
		if !c.failOpen {
			c.stats.mu.Lock()
			c.stats.sseConnected = false // Indicate connection lost if not fail-open
			c.stats.mu.Unlock()
		}
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

	// Signal that initial sync is complete after the first update (or on connect)
	select {
	case <-c.sseReady:
		// Already closed
	default:
		close(c.sseReady)
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
