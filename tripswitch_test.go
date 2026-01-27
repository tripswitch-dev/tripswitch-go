package tripswitch

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockLogger is a simple logger for testing.
type mockLogger struct {
	lastMsg  atomic.Value // Use atomic.Value for concurrent-safe updates
	warnMsgs []string     // Track all warn messages
	mu       sync.Mutex
}

func (m *mockLogger) Debug(msg string, args ...any) { m.lastMsg.Store(msg) }
func (m *mockLogger) Info(msg string, args ...any)  { m.lastMsg.Store(msg) }
func (m *mockLogger) Warn(msg string, args ...any) {
	m.lastMsg.Store(msg)
	m.mu.Lock()
	m.warnMsgs = append(m.warnMsgs, msg)
	m.mu.Unlock()
}
func (m *mockLogger) Error(msg string, args ...any) { m.lastMsg.Store(msg) }
func (m *mockLogger) LastMsg() string {
	if v := m.lastMsg.Load(); v != nil {
		return v.(string)
	}
	return ""
}
func (m *mockLogger) HasWarn(msg string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, w := range m.warnMsgs {
		if w == msg {
			return true
		}
	}
	return false
}

// testRouterID is a constant router ID used for testing.
const testRouterID = "test-router-id"

func TestNewClient(t *testing.T) {
	// Start a mock SSE server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"breaker\": \"test\", \"state\": \"closed\", \"allow_rate\": 1.0}\n\n")
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	projectID := "proj_123"
	apiKey := "eb_pk_test"
	ingestKey := "ik_def"
	logger := &mockLogger{}
	tags := map[string]string{"env": "testing"}
	extractor := func(ctx context.Context) string { return "trace-id" }

	ts := NewClient(projectID,
		WithAPIKey(apiKey),
		WithIngestKey(ingestKey),
		WithFailOpen(false),
		WithBaseURL(server.URL),
		WithLogger(logger),
		WithGlobalTags(tags),
		WithTraceIDExtractor(extractor),
		WithOnStateChange(func(name, from, to string) {}),
	)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ts.Close(ctx)
	}()

	if ts.projectID != projectID {
		t.Errorf("expected projectID %q, got %q", projectID, ts.projectID)
	}
	if ts.apiKey != apiKey {
		t.Errorf("expected apiKey %q, got %q", apiKey, ts.apiKey)
	}
	if ts.ingestSecret != ingestKey {
		t.Errorf("expected ingestSecret %q, got %q", ingestKey, ts.ingestSecret)
	}
	if ts.failOpen != false {
		t.Errorf("expected failOpen to be false")
	}
	if ts.baseURL != server.URL {
		t.Errorf("expected baseURL %q, got %q", server.URL, ts.baseURL)
	}
	if ts.logger != logger {
		t.Errorf("expected logger to be set")
	}
	if ts.globalTags["env"] != "testing" {
		t.Errorf("expected globalTags to be set")
	}
	if ts.traceExtractor == nil {
		t.Errorf("expected traceExtractor to be set")
	}
	if ts.onStateChange == nil {
		t.Errorf("expected onStateChange to be set")
	}

	// Test default values with mock server
	tsDefault := NewClient("proj_456", WithBaseURL(server.URL))
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tsDefault.Close(ctx)
	}()

	if tsDefault.failOpen != true {
		t.Errorf("expected failOpen to be true by default")
	}
	if tsDefault.logger == nil {
		t.Errorf("expected default logger to be non-nil")
	}
}

func TestClose(t *testing.T) {
	// Start a mock SSE server that keeps connection open
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Send initial event to signal ready
		fmt.Fprintf(w, "data: {\"breaker\": \"test\", \"state\": \"closed\", \"allow_rate\": 1.0}\n\n")
		flusher.Flush()

		// Wait until client disconnects
		<-r.Context().Done()
	}))
	defer server.Close()

	ts := NewClient("proj_abc", WithBaseURL(server.URL))

	// Wait for SSE connection to be established before closing
	// This avoids race condition where Close() is called while SSE is still connecting
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = ts.Ready(ctx) // Ignore error - just ensuring connection attempt completes

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer closeCancel()

	err := ts.Close(closeCtx)
	if err != nil {
		t.Fatalf("Close() returned an error: %v", err)
	}

	// Calling it again should be a no-op and not block.
	err = ts.Close(closeCtx)
	if err != nil {
		t.Fatalf("second call to Close() returned an error: %v", err)
	}
}

func TestStats(t *testing.T) {
	// Start a mock SSE server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"breaker\": \"test\", \"state\": \"closed\", \"allow_rate\": 1.0}\n\n")
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	ts := NewClient("proj_abc", WithBaseURL(server.URL))
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ts.Close(ctx)
	}()

	ts.stats.mu.Lock()
	ts.stats.droppedSamples = 5
	ts.stats.sseConnected = true
	ts.stats.mu.Unlock()

	stats := ts.Stats()
	if stats.DroppedSamples != 5 {
		t.Errorf("expected droppedSamples to be 5, got %d", stats.DroppedSamples)
	}
	if !stats.SSEConnected {
		t.Errorf("expected sseConnected to be true")
	}
}

func TestIsBreakerError(t *testing.T) {
	if !IsBreakerError(ErrOpen) {
		t.Errorf("expected IsBreakerError(ErrOpen) to be true")
	}
	if IsBreakerError(errors.New("some other error")) {
		t.Errorf("expected IsBreakerError(another_error) to be false")
	}

	wrappedErr := fmt.Errorf("outer error: %w", ErrOpen) // Correct way to wrap
	if !IsBreakerError(wrappedErr) {
		t.Errorf("expected IsBreakerError to detect wrapped ErrOpen")
	}
}

func TestReady(t *testing.T) {
	// Start a mock SSE server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Send initial event
		fmt.Fprintf(w, "data: {\"breaker\": \"test-breaker\", \"state\": \"closed\", \"allow_rate\": 1.0}\n\n")
		flusher.Flush()

		// Keep connection alive for a short period, then close
		select {
		case <-r.Context().Done():
			return
		case <-time.After(100 * time.Millisecond):
			// Simulate server closing connection
			return
		}
	}))
	defer ts.Close()

	client := NewClient("proj_test", WithBaseURL(ts.URL))
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		client.Close(closeCtx)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Ready should block until the initial event is received
	err := client.Ready(ctx)
	if err != nil {
		t.Fatalf("Ready() failed: %v", err)
	}

	// Verify the state was updated
	client.breakerStatesMu.RLock()
	state, ok := client.breakerStates["test-breaker"]
	client.breakerStatesMu.RUnlock()

	if !ok || state.State != "closed" || state.AllowRate != 1.0 {
		t.Errorf("expected test-breaker state to be closed with allow_rate 1.0, got %+v", state)
	}
}

// newTestClient creates a client with a mock SSE server for testing.
// The returned cleanup function must be called to close both the client and server.
func newTestClient(t *testing.T, opts ...Option) (*Client, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"breaker\": \"test\", \"state\": \"closed\", \"allow_rate\": 1.0}\n\n")
		flusher.Flush()
		<-r.Context().Done()
	}))

	allOpts := append([]Option{WithBaseURL(server.URL)}, opts...)
	client := NewClient("proj_test", allOpts...)

	cleanup := func() {
		// Close client first (cancels SSE subscription), then close server
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		client.Close(ctx)
		server.Close()
	}
	return client, cleanup
}

func TestExecute_NoBreakers(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()

	// No breakers specified = pass-through (always allowed)
	result, err := Execute(client, context.Background(), testRouterID, func() (string, error) {
		return "success", nil
	}, WithMetric("latency", Latency))

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if result != "success" {
		t.Errorf("expected result 'success', got %q", result)
	}
}

func TestExecute_ClosedBreaker(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()

	// Set breaker to closed state
	client.breakerStatesMu.Lock()
	client.breakerStates["test-breaker"] = breakerState{State: "closed", AllowRate: 1.0}
	client.breakerStatesMu.Unlock()

	result, err := Execute(client, context.Background(), testRouterID, func() (string, error) {
		return "success", nil
	}, WithBreakers("test-breaker"), WithMetric("latency", Latency))

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if result != "success" {
		t.Errorf("expected result 'success', got %q", result)
	}
}

func TestExecute_OpenBreaker(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()

	// Set breaker to open state
	client.breakerStatesMu.Lock()
	client.breakerStates["test-breaker"] = breakerState{State: "open", AllowRate: 0}
	client.breakerStatesMu.Unlock()

	result, err := Execute(client, context.Background(), testRouterID, func() (string, error) {
		t.Error("task should not be executed when breaker is open")
		return "should-not-run", nil
	}, WithBreakers("test-breaker"))

	if !errors.Is(err, ErrOpen) {
		t.Errorf("expected ErrOpen, got %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
}

func TestExecute_HalfOpenBreaker(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()

	// Set breaker to half-open with 0% allow rate (always throttled)
	client.breakerStatesMu.Lock()
	client.breakerStates["throttled-breaker"] = breakerState{State: "half_open", AllowRate: 0}
	client.breakerStatesMu.Unlock()

	_, err := Execute(client, context.Background(), testRouterID, func() (string, error) {
		t.Error("task should not be executed when throttled")
		return "should-not-run", nil
	}, WithBreakers("throttled-breaker"))

	if !errors.Is(err, ErrOpen) {
		t.Errorf("expected ErrOpen for throttled request, got %v", err)
	}

	// Set breaker to half-open with 100% allow rate (always allowed)
	client.breakerStatesMu.Lock()
	client.breakerStates["allowed-breaker"] = breakerState{State: "half_open", AllowRate: 1.0}
	client.breakerStatesMu.Unlock()

	result, err := Execute(client, context.Background(), testRouterID, func() (string, error) {
		return "allowed", nil
	}, WithBreakers("allowed-breaker"))

	if err != nil {
		t.Errorf("expected no error for allowed request, got %v", err)
	}
	if result != "allowed" {
		t.Errorf("expected result 'allowed', got %q", result)
	}
}

func TestExecute_ContextCanceled(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := Execute(client, ctx, testRouterID, func() (string, error) {
		t.Error("task should not be executed when context is canceled")
		return "should-not-run", nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestExecute_WithIgnoreErrors(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()

	errNotFound := errors.New("not found")

	// Execute with ignored error - should report as OK
	_, err := Execute(client, context.Background(), testRouterID, func() (string, error) {
		return "", errNotFound
	}, WithMetric("count", 1), WithIgnoreErrors(errNotFound))

	if !errors.Is(err, errNotFound) {
		t.Errorf("expected errNotFound to be returned, got %v", err)
	}

	// Check that a sample was reported (drain the channel)
	select {
	case entry := <-client.reportChan:
		if !entry.OK {
			t.Error("expected ignored error to be reported as OK")
		}
	default:
		t.Error("expected a report entry")
	}
}

func TestExecute_WithErrorEvaluator(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()

	// Custom evaluator that considers even numbered errors as non-failures
	evaluator := func(err error) bool {
		return err.Error() != "ok-error"
	}

	// Error that evaluator says is NOT a failure
	_, _ = Execute(client, context.Background(), testRouterID, func() (string, error) {
		return "", errors.New("ok-error")
	}, WithMetric("count", 1), WithErrorEvaluator(evaluator))

	select {
	case entry := <-client.reportChan:
		if !entry.OK {
			t.Error("expected evaluator to mark 'ok-error' as OK")
		}
	default:
		t.Error("expected a report entry")
	}

	// Error that evaluator says IS a failure
	_, _ = Execute(client, context.Background(), testRouterID, func() (string, error) {
		return "", errors.New("bad-error")
	}, WithMetric("count", 1), WithErrorEvaluator(evaluator))

	select {
	case entry := <-client.reportChan:
		if entry.OK {
			t.Error("expected evaluator to mark 'bad-error' as failure")
		}
	default:
		t.Error("expected a report entry")
	}
}

func TestExecute_WithTags(t *testing.T) {
	client, cleanup := newTestClient(t, WithGlobalTags(map[string]string{"env": "test", "service": "api"}))
	defer cleanup()

	_, _ = Execute(client, context.Background(), testRouterID, func() (string, error) {
		return "ok", nil
	}, WithMetric("count", 1), WithTags(map[string]string{"endpoint": "/users", "env": "override"}))

	select {
	case entry := <-client.reportChan:
		if entry.Tags["endpoint"] != "/users" {
			t.Errorf("expected dynamic tag 'endpoint' to be '/users', got %q", entry.Tags["endpoint"])
		}
		if entry.Tags["service"] != "api" {
			t.Errorf("expected global tag 'service' to be 'api', got %q", entry.Tags["service"])
		}
		if entry.Tags["env"] != "override" {
			t.Errorf("expected dynamic tag to override global, got %q", entry.Tags["env"])
		}
	default:
		t.Error("expected a report entry")
	}
}

func TestExecute_WithTraceID(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()

	_, _ = Execute(client, context.Background(), testRouterID, func() (string, error) {
		return "ok", nil
	}, WithMetric("count", 1), WithTraceID("explicit-trace-123"))

	select {
	case entry := <-client.reportChan:
		if entry.TraceID != "explicit-trace-123" {
			t.Errorf("expected traceID 'explicit-trace-123', got %q", entry.TraceID)
		}
	default:
		t.Error("expected a report entry")
	}
}

func TestExecute_TraceIDFromExtractor(t *testing.T) {
	extractor := func(ctx context.Context) string {
		return "extracted-trace-456"
	}
	client, cleanup := newTestClient(t, WithTraceIDExtractor(extractor))
	defer cleanup()

	_, _ = Execute(client, context.Background(), testRouterID, func() (string, error) {
		return "ok", nil
	}, WithMetric("count", 1))

	select {
	case entry := <-client.reportChan:
		if entry.TraceID != "extracted-trace-456" {
			t.Errorf("expected traceID 'extracted-trace-456', got %q", entry.TraceID)
		}
	default:
		t.Error("expected a report entry")
	}
}

func TestExecute_TraceIDOptionOverridesExtractor(t *testing.T) {
	extractor := func(ctx context.Context) string {
		return "extractor-trace"
	}
	client, cleanup := newTestClient(t, WithTraceIDExtractor(extractor))
	defer cleanup()

	_, _ = Execute(client, context.Background(), testRouterID, func() (string, error) {
		return "ok", nil
	}, WithMetric("count", 1), WithTraceID("option-trace"))

	select {
	case entry := <-client.reportChan:
		if entry.TraceID != "option-trace" {
			t.Errorf("expected option traceID to override extractor, got %q", entry.TraceID)
		}
	default:
		t.Error("expected a report entry")
	}
}

func TestMergeTags(t *testing.T) {
	client, cleanup := newTestClient(t, WithGlobalTags(map[string]string{"a": "1", "b": "2"}))
	defer cleanup()

	// No dynamic tags - should return globalTags directly
	result := client.mergeTags(nil)
	if result["a"] != "1" || result["b"] != "2" {
		t.Error("expected globalTags to be returned when no dynamic tags")
	}

	// Only dynamic tags (client without global tags)
	client2, cleanup2 := newTestClient(t)
	defer cleanup2()

	dynamic := map[string]string{"x": "10"}
	result2 := client2.mergeTags(dynamic)
	if result2["x"] != "10" {
		t.Error("expected dynamic tags to be returned when no global tags")
	}

	// Both - dynamic overrides
	result3 := client.mergeTags(map[string]string{"a": "override", "c": "3"})
	if result3["a"] != "override" {
		t.Errorf("expected dynamic to override global, got %q", result3["a"])
	}
	if result3["b"] != "2" {
		t.Errorf("expected global tag b to remain, got %q", result3["b"])
	}
	if result3["c"] != "3" {
		t.Errorf("expected new dynamic tag c, got %q", result3["c"])
	}
}

func TestIsFailure(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()

	errTest := errors.New("test error")
	errIgnored := errors.New("ignored error")

	tests := []struct {
		name     string
		err      error
		opts     *executeOptions
		expected bool
	}{
		{"nil error", nil, &executeOptions{}, false},
		{"any error", errTest, &executeOptions{}, true},
		{"ignored error", errIgnored, &executeOptions{ignoreErrors: []error{errIgnored}}, false},
		{"non-ignored error", errTest, &executeOptions{ignoreErrors: []error{errIgnored}}, true},
		{"evaluator returns true", errTest, &executeOptions{errorEvaluator: func(e error) bool { return true }}, true},
		{"evaluator returns false", errTest, &executeOptions{errorEvaluator: func(e error) bool { return false }}, false},
		{"evaluator overrides ignore list", errIgnored, &executeOptions{
			ignoreErrors:   []error{errIgnored},
			errorEvaluator: func(e error) bool { return true }, // evaluator says it's a failure
		}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.isFailure(tt.err, tt.opts)
			if result != tt.expected {
				t.Errorf("isFailure() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestIsAllowed(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()

	// Unknown breaker - fail-open
	if !client.isAllowed("unknown") {
		t.Error("expected fail-open for unknown breaker")
	}

	// Closed breaker
	client.breakerStatesMu.Lock()
	client.breakerStates["closed-breaker"] = breakerState{State: "closed", AllowRate: 0}
	client.breakerStatesMu.Unlock()

	if !client.isAllowed("closed-breaker") {
		t.Error("expected closed breaker to allow")
	}

	// Open breaker
	client.breakerStatesMu.Lock()
	client.breakerStates["open-breaker"] = breakerState{State: "open", AllowRate: 0}
	client.breakerStatesMu.Unlock()

	if client.isAllowed("open-breaker") {
		t.Error("expected open breaker to deny")
	}

	// Unknown state - fail-open
	client.breakerStatesMu.Lock()
	client.breakerStates["unknown-state"] = breakerState{State: "weird", AllowRate: 0}
	client.breakerStatesMu.Unlock()

	if !client.isAllowed("unknown-state") {
		t.Error("expected fail-open for unknown state")
	}
}

func TestSendBatch_PayloadFormat(t *testing.T) {
	var receivedPayload batchPayload
	var receivedEncoding string
	var receivedTimestamp string
	var receivedSignature string
	var receivedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for SSE endpoint vs ingest endpoint
		if r.URL.Path != "/v1/projects/proj_test/ingest" {
			// SSE endpoint - keep alive
			flusher, ok := w.(http.Flusher)
			if !ok {
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"breaker\": \"test\", \"state\": \"closed\", \"allow_rate\": 1.0}\n\n")
			flusher.Flush()
			<-r.Context().Done()
			return
		}

		// Ingest endpoint
		receivedPath = r.URL.Path
		receivedEncoding = r.Header.Get("Content-Encoding")
		receivedTimestamp = r.Header.Get("X-EB-Timestamp")
		receivedSignature = r.Header.Get("X-EB-Signature")

		// Decompress GZIP and decode JSON
		gr, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Errorf("failed to create gzip reader: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer gr.Close()

		if err := json.NewDecoder(gr).Decode(&receivedPayload); err != nil {
			t.Errorf("failed to decode payload: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	// Use a valid 64-char hex string for the ingest secret
	ingestSecret := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	client := NewClient("proj_test", WithBaseURL(server.URL), WithIngestSecret(ingestSecret))
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client.Close(ctx)
	}()

	// Send a batch directly
	testTsMs := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC).UnixMilli()
	batch := []reportEntry{
		{
			RouterID: "router-123",
			Metric:   "error_rate",
			TsMs:     testTsMs,
			OK:       true,
			Value:    1.0,
			TraceID:  "abc123",
			Tags:     map[string]string{"tier": "premium"},
		},
		{
			RouterID: "router-123",
			Metric:   "error_rate",
			TsMs:     testTsMs + 1000,
			OK:       false,
			Value:    1.0,
			TraceID:  "def456",
			Tags:     map[string]string{"tier": "free"},
		},
	}

	client.sendBatch(batch)

	// Verify path and headers
	if receivedPath != "/v1/projects/proj_test/ingest" {
		t.Errorf("expected path '/v1/projects/proj_test/ingest', got %q", receivedPath)
	}

	if receivedEncoding != "gzip" {
		t.Errorf("expected Content-Encoding: gzip, got %q", receivedEncoding)
	}

	if receivedTimestamp == "" {
		t.Error("expected X-EB-Timestamp to be set")
	}

	if receivedSignature == "" {
		t.Error("expected X-EB-Signature to be set")
	}

	if len(receivedPayload.Samples) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(receivedPayload.Samples))
	}

	sample := receivedPayload.Samples[0]
	if sample.RouterID != "router-123" {
		t.Errorf("expected router_id 'router-123', got %q", sample.RouterID)
	}
	if sample.Metric != "error_rate" {
		t.Errorf("expected metric 'error_rate', got %q", sample.Metric)
	}
	if !sample.OK {
		t.Error("expected first sample OK to be true")
	}
	if sample.TraceID != "abc123" {
		t.Errorf("expected trace_id 'abc123', got %q", sample.TraceID)
	}
	if sample.Tags["tier"] != "premium" {
		t.Errorf("expected tier 'premium', got %q", sample.Tags["tier"])
	}
}

func TestGetStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// SSE endpoint
		if r.URL.Path != "/v1/projects/proj_123/status" {
			flusher, ok := w.(http.Flusher)
			if !ok {
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"breaker\": \"test\", \"state\": \"closed\", \"allow_rate\": 1.0}\n\n")
			flusher.Flush()
			<-r.Context().Done()
			return
		}

		// Status endpoint
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer eb_pk_test" {
			t.Errorf("unexpected auth header: %s", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Status{
			OpenCount:   2,
			ClosedCount: 8,
			LastEvalMs:  1234567890,
		})
	}))
	defer server.Close()

	client := NewClient("proj_123",
		WithAPIKey("eb_pk_test"),
		WithBaseURL(server.URL),
	)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client.Close(ctx)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	status, err := client.GetStatus(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.OpenCount != 2 {
		t.Errorf("expected 2 open breakers, got %d", status.OpenCount)
	}
	if status.ClosedCount != 8 {
		t.Errorf("expected 8 closed breakers, got %d", status.ClosedCount)
	}
}

// New tests for the redesigned Execute API

func TestExecute_WithLatencySentinel(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()

	_, _ = Execute(client, context.Background(), testRouterID, func() (string, error) {
		// Task execution - latency will be measured
		return "ok", nil
	}, WithMetric("latency", Latency))

	select {
	case entry := <-client.reportChan:
		if entry.Metric != "latency" {
			t.Errorf("expected metric 'latency', got %q", entry.Metric)
		}
		// Latency should be >= 0
		if entry.Value < 0 {
			t.Errorf("expected latency >= 0, got %f", entry.Value)
		}
	default:
		t.Error("expected a report entry")
	}
}

func TestExecute_WithMultipleMetrics(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()

	_, _ = Execute(client, context.Background(), testRouterID, func() (string, error) {
		return "ok", nil
	},
		WithMetric("latency", Latency),
		WithMetric("count", 1),
		WithMetric("amount", 99.99),
	)

	// Collect all samples
	samples := make(map[string]float64)
	for i := 0; i < 3; i++ {
		select {
		case entry := <-client.reportChan:
			samples[entry.Metric] = entry.Value
		default:
			t.Error("expected 3 report entries")
		}
	}

	if _, ok := samples["latency"]; !ok {
		t.Error("expected latency metric")
	}
	if samples["count"] != 1 {
		t.Errorf("expected count=1, got %f", samples["count"])
	}
	if samples["amount"] != 99.99 {
		t.Errorf("expected amount=99.99, got %f", samples["amount"])
	}
}

func TestExecute_WithMetricClosure(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()

	queueDepth := 42.0
	_, _ = Execute(client, context.Background(), testRouterID, func() (string, error) {
		return "ok", nil
	}, WithMetric("queue_depth", func() float64 { return queueDepth }))

	select {
	case entry := <-client.reportChan:
		if entry.Metric != "queue_depth" {
			t.Errorf("expected metric 'queue_depth', got %q", entry.Metric)
		}
		if entry.Value != 42.0 {
			t.Errorf("expected value 42.0, got %f", entry.Value)
		}
	default:
		t.Error("expected a report entry")
	}
}

func TestExecute_WithMetricClosurePanic(t *testing.T) {
	logger := &mockLogger{}
	client, cleanup := newTestClient(t, WithLogger(logger))
	defer cleanup()

	// Use two metrics: one that panics, one that doesn't
	_, _ = Execute(client, context.Background(), testRouterID, func() (string, error) {
		return "ok", nil
	},
		WithMetric("panicking", func() float64 { panic("boom") }),
		WithMetric("safe", 1.0),
	)

	// Should still get the safe metric (panic should be recovered)
	// Due to map iteration order randomness, we might get either metric first
	// (the panicking one will be skipped, so we should only get "safe")
	select {
	case entry := <-client.reportChan:
		if entry.Metric != "safe" {
			t.Errorf("expected only 'safe' metric (panicking should be skipped), got %q", entry.Metric)
		}
	default:
		t.Error("expected a report entry for 'safe' metric")
	}

	// Logger should have warned about the panic
	if !logger.HasWarn("metric closure panicked") {
		t.Errorf("expected panic warning in log, got warns: %v", logger.warnMsgs)
	}
}

func TestExecute_UnknownBreaker_FailOpen(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()

	// Unknown breaker not in cache should fail-open (allow execution)
	result, err := Execute(client, context.Background(), testRouterID, func() (string, error) {
		return "success", nil
	}, WithBreakers("unknown-breaker-not-in-cache"), WithMetric("count", 1))

	if err != nil {
		t.Errorf("expected fail-open for unknown breaker, got error: %v", err)
	}
	if result != "success" {
		t.Errorf("expected result 'success', got %q", result)
	}
}

func TestExecute_MultipleBreakers_AnyOpen(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()

	// Set one breaker closed, one open
	client.breakerStatesMu.Lock()
	client.breakerStates["breaker-a"] = breakerState{State: "closed", AllowRate: 1.0}
	client.breakerStates["breaker-b"] = breakerState{State: "open", AllowRate: 0}
	client.breakerStatesMu.Unlock()

	// If ANY breaker is open, should return ErrOpen
	_, err := Execute(client, context.Background(), testRouterID, func() (string, error) {
		t.Error("task should not be executed when any breaker is open")
		return "should-not-run", nil
	}, WithBreakers("breaker-a", "breaker-b"))

	if !errors.Is(err, ErrOpen) {
		t.Errorf("expected ErrOpen when any breaker is open, got %v", err)
	}
}

func TestExecute_MultipleBreakers_AllClosed(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()

	// Set both breakers closed
	client.breakerStatesMu.Lock()
	client.breakerStates["breaker-a"] = breakerState{State: "closed", AllowRate: 1.0}
	client.breakerStates["breaker-b"] = breakerState{State: "closed", AllowRate: 1.0}
	client.breakerStatesMu.Unlock()

	result, err := Execute(client, context.Background(), testRouterID, func() (string, error) {
		return "success", nil
	}, WithBreakers("breaker-a", "breaker-b"), WithMetric("count", 1))

	if err != nil {
		t.Errorf("expected no error when all breakers closed, got %v", err)
	}
	if result != "success" {
		t.Errorf("expected result 'success', got %q", result)
	}
}

func TestExecute_NoMetrics_NoSamples(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()

	// Execute without any metrics - should not emit any samples
	_, _ = Execute(client, context.Background(), testRouterID, func() (string, error) {
		return "ok", nil
	})

	// Should be no samples in the channel
	select {
	case entry := <-client.reportChan:
		t.Errorf("expected no samples when no metrics specified, got %+v", entry)
	default:
		// Good - no samples
	}
}

func TestExecute_WithTag(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()

	_, _ = Execute(client, context.Background(), testRouterID, func() (string, error) {
		return "ok", nil
	},
		WithMetric("count", 1),
		WithTag("endpoint", "/checkout"),
		WithTag("method", "POST"),
	)

	select {
	case entry := <-client.reportChan:
		if entry.Tags["endpoint"] != "/checkout" {
			t.Errorf("expected tag endpoint='/checkout', got %q", entry.Tags["endpoint"])
		}
		if entry.Tags["method"] != "POST" {
			t.Errorf("expected tag method='POST', got %q", entry.Tags["method"])
		}
	default:
		t.Error("expected a report entry")
	}
}

func TestExecute_WithMetrics_Bulk(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()

	_, _ = Execute(client, context.Background(), testRouterID, func() (string, error) {
		return "ok", nil
	}, WithMetrics(map[string]any{
		"latency": Latency,
		"count":   1,
		"amount":  50.0,
	}))

	// Collect all samples
	samples := make(map[string]float64)
	for i := 0; i < 3; i++ {
		select {
		case entry := <-client.reportChan:
			samples[entry.Metric] = entry.Value
		default:
			break
		}
	}

	if len(samples) != 3 {
		t.Errorf("expected 3 samples, got %d", len(samples))
	}
}

func TestExecute_RouterIDInSamples(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()

	customRouterID := "custom-router-123"
	_, _ = Execute(client, context.Background(), customRouterID, func() (string, error) {
		return "ok", nil
	}, WithMetric("count", 1))

	select {
	case entry := <-client.reportChan:
		if entry.RouterID != customRouterID {
			t.Errorf("expected RouterID %q, got %q", customRouterID, entry.RouterID)
		}
	default:
		t.Error("expected a report entry")
	}
}
