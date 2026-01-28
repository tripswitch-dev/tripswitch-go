package tripswitch

import (
	"context"
	"os"
	"testing"
	"time"
)

// Integration tests are gated by environment variables.
// Run with:
//
//	TRIPSWITCH_API_KEY=eb_pk_...
//	TRIPSWITCH_INGEST_SECRET=<64-char-hex>
//	TRIPSWITCH_PROJECT_ID=proj_...
//	TRIPSWITCH_BREAKER_NAME=my-breaker
//	TRIPSWITCH_BREAKER_ROUTER_ID=router-id
//	TRIPSWITCH_BREAKER_METRIC=metric-name
//	go test -v -run Integration
//
// Optional:
//
//	TRIPSWITCH_BASE_URL=https://api.tripswitch.dev (defaults to production)

type testConfig struct {
	apiKey       string
	ingestSecret string
	projectID    string
	baseURL      string
	breakerName  string
	routerID     string
	metricName   string
}

func skipIfNoEnv(t *testing.T) testConfig {
	cfg := testConfig{
		apiKey:       os.Getenv("TRIPSWITCH_API_KEY"),
		ingestSecret: os.Getenv("TRIPSWITCH_INGEST_SECRET"),
		projectID:    os.Getenv("TRIPSWITCH_PROJECT_ID"),
		baseURL:      os.Getenv("TRIPSWITCH_BASE_URL"),
		breakerName:  os.Getenv("TRIPSWITCH_BREAKER_NAME"),
		routerID:     os.Getenv("TRIPSWITCH_BREAKER_ROUTER_ID"),
		metricName:   os.Getenv("TRIPSWITCH_BREAKER_METRIC"),
	}

	if cfg.apiKey == "" || cfg.projectID == "" {
		t.Skip("Skipping integration test: TRIPSWITCH_API_KEY and TRIPSWITCH_PROJECT_ID must be set")
	}

	if cfg.breakerName == "" || cfg.routerID == "" || cfg.metricName == "" {
		t.Skip("Skipping integration test: TRIPSWITCH_BREAKER_NAME, TRIPSWITCH_BREAKER_ROUTER_ID, and TRIPSWITCH_BREAKER_METRIC must be set")
	}

	if cfg.baseURL == "" {
		cfg.baseURL = "https://api.tripswitch.dev"
	}

	return cfg
}

func TestIntegration_Ready(t *testing.T) {
	cfg := skipIfNoEnv(t)

	client := NewClient(cfg.projectID,
		WithAPIKey(cfg.apiKey),
		WithIngestSecret(cfg.ingestSecret),
		WithBaseURL(cfg.baseURL),
	)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := client.Ready(ctx)
	if err != nil {
		t.Fatalf("Ready failed: %v", err)
	}

	t.Log("SSE connection established and ready")
}

func TestIntegration_Execute(t *testing.T) {
	cfg := skipIfNoEnv(t)

	client := NewClient(cfg.projectID,
		WithAPIKey(cfg.apiKey),
		WithIngestSecret(cfg.ingestSecret),
		WithBaseURL(cfg.baseURL),
	)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Wait for SSE to be ready
	if err := client.Ready(ctx); err != nil {
		t.Fatalf("Ready failed: %v", err)
	}

	// Execute a simple task using the configured breaker
	result, err := Execute(client, ctx, func() (string, error) {
		return "success", nil
	}, WithBreakers(cfg.breakerName), WithRouter(cfg.routerID), WithMetric(cfg.metricName, Latency))

	// The breaker might be open, closed, or not exist (fail-open)
	if err != nil && !IsBreakerError(err) {
		t.Fatalf("Execute failed with unexpected error: %v", err)
	}

	if err == nil {
		if result != "success" {
			t.Errorf("expected result 'success', got %q", result)
		}
		t.Log("Execute completed successfully")
	} else {
		t.Log("Execute blocked by open breaker (expected if breaker is tripped)")
	}
}

func TestIntegration_Stats(t *testing.T) {
	cfg := skipIfNoEnv(t)

	client := NewClient(cfg.projectID,
		WithAPIKey(cfg.apiKey),
		WithIngestSecret(cfg.ingestSecret),
		WithBaseURL(cfg.baseURL),
	)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Wait for SSE to be ready
	if err := client.Ready(ctx); err != nil {
		t.Fatalf("Ready failed: %v", err)
	}

	stats := client.Stats()
	if !stats.SSEConnected {
		t.Error("expected SSEConnected to be true after Ready")
	}

	t.Logf("Stats: SSEConnected=%v, SSEReconnects=%d, DroppedSamples=%d",
		stats.SSEConnected, stats.SSEReconnects, stats.DroppedSamples)
}

func TestIntegration_GracefulShutdown(t *testing.T) {
	cfg := skipIfNoEnv(t)

	client := NewClient(cfg.projectID,
		WithAPIKey(cfg.apiKey),
		WithIngestSecret(cfg.ingestSecret),
		WithBaseURL(cfg.baseURL),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Wait for SSE to be ready
	if err := client.Ready(ctx); err != nil {
		t.Fatalf("Ready failed: %v", err)
	}

	// Execute a few tasks to generate samples
	for i := 0; i < 5; i++ {
		Execute(client, ctx, func() (int, error) {
			return i, nil
		}, WithBreakers(cfg.breakerName), WithRouter(cfg.routerID), WithMetric(cfg.metricName, Latency))
	}

	// Graceful shutdown should flush samples
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	err := client.Close(shutdownCtx)
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	t.Log("Graceful shutdown completed")
}

func TestIntegration_GetStatus(t *testing.T) {
	cfg := skipIfNoEnv(t)

	client := NewClient(cfg.projectID,
		WithAPIKey(cfg.apiKey),
		WithBaseURL(cfg.baseURL),
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
		t.Fatalf("GetStatus failed: %v", err)
	}

	t.Logf("Status: %d open, %d closed", status.OpenCount, status.ClosedCount)
}
