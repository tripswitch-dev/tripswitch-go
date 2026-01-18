package tripswitch

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

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
	}

	for _, opt := range opts {
		opt(c)
	}

	c.ctx, c.cancel = context.WithCancel(context.Background())

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
		c.globalTags = tags
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
