package tripswitch

import (
	"context"
	"errors"
	"testing"
)

// mockLogger is a simple logger for testing.
type mockLogger struct {
	lastMsg string
}

func (m *mockLogger) Debug(msg string, args ...any) { m.lastMsg = msg }
func (m *mockLogger) Info(msg string, args ...any)  { m.lastMsg = msg }
func (m *mockLogger) Warn(msg string, args ...any)  { m.lastMsg = msg }
func (m *mockLogger) Error(msg string, args ...any) { m.lastMsg = msg }

func TestNewClient(t *testing.T) {
	projectID := "proj_123"
	apiKey := "sk_abc"
	ingestKey := "ik_def"
	baseURL := "https://example.com"
	logger := &mockLogger{}
	tags := map[string]string{"env": "testing"}
	extractor := func(ctx context.Context) string { return "trace-id" }

	ts := NewClient(projectID,
		WithAPIKey(apiKey),
		WithIngestKey(ingestKey),
		WithFailOpen(false),
		WithBaseURL(baseURL),
		WithLogger(logger),
		WithGlobalTags(tags),
		WithTraceIDExtractor(extractor),
		WithOnStateChange(func(name, from, to string) {}),
	)

	if ts.projectID != projectID {
		t.Errorf("expected projectID %q, got %q", projectID, ts.projectID)
	}
	if ts.apiKey != apiKey {
		t.Errorf("expected apiKey %q, got %q", apiKey, ts.apiKey)
	}
	if ts.ingestKey != ingestKey {
		t.Errorf("expected ingestKey %q, got %q", ingestKey, ts.ingestKey)
	}
	if ts.failOpen != false {
		t.Errorf("expected failOpen to be false")
	}
	if ts.baseURL != baseURL {
		t.Errorf("expected baseURL %q, got %q", baseURL, ts.baseURL)
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

	// Test default values
	tsDefault := NewClient("proj_456")
	if tsDefault.failOpen != true {
		t.Errorf("expected failOpen to be true by default")
	}
	if tsDefault.baseURL != "https://api.tripswitch.dev" {
		t.Errorf("expected default baseURL, got %q", tsDefault.baseURL)
	}
	if tsDefault.logger == nil {
		t.Errorf("expected default logger to be non-nil")
	}
}

func TestClose(t *testing.T) {
	ts := NewClient("proj_abc")
	err := ts.Close()
	if err != nil {
		t.Fatalf("Close() returned an error: %v", err)
	}

	// Calling it again should be a no-op and not block.
	err = ts.Close()
	if err != nil {
		t.Fatalf("second call to Close() returned an error: %v", err)
	}
}

func TestStats(t *testing.T) {
	ts := NewClient("proj_abc")
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
}
