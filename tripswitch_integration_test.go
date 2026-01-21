package tripswitch

import (
	"context"
	"os"
	"testing"
	"time"
)

// Integration tests are gated by environment variables.
// Run with:
//   TRIPSWITCH_API_KEY=sk_... TRIPSWITCH_INGEST_KEY=ik_... TRIPSWITCH_PROJECT_ID=proj_... go test -v -run Integration
//
// Optional:
//   TRIPSWITCH_BASE_URL=https://api.tripswitch.dev (defaults to production)

func skipIfNoEnv(t *testing.T) (apiKey, ingestKey, projectID, baseURL string) {
	apiKey = os.Getenv("TRIPSWITCH_API_KEY")
	ingestKey = os.Getenv("TRIPSWITCH_INGEST_KEY")
	projectID = os.Getenv("TRIPSWITCH_PROJECT_ID")
	baseURL = os.Getenv("TRIPSWITCH_BASE_URL")

	if apiKey == "" || projectID == "" {
		t.Skip("Skipping integration test: TRIPSWITCH_API_KEY and TRIPSWITCH_PROJECT_ID must be set")
	}

	if baseURL == "" {
		baseURL = "https://api.tripswitch.dev"
	}

	return apiKey, ingestKey, projectID, baseURL
}

func TestIntegration_Ready(t *testing.T) {
	apiKey, ingestKey, projectID, baseURL := skipIfNoEnv(t)

	client := NewClient(projectID,
		WithAPIKey(apiKey),
		WithIngestKey(ingestKey),
		WithBaseURL(baseURL),
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
	apiKey, ingestKey, projectID, baseURL := skipIfNoEnv(t)

	client := NewClient(projectID,
		WithAPIKey(apiKey),
		WithIngestKey(ingestKey),
		WithBaseURL(baseURL),
	)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Wait for SSE to be ready
	if err := client.Ready(ctx); err != nil {
		t.Fatalf("Ready failed: %v", err)
	}

	// Execute a simple task - use a breaker name that likely exists
	result, err := Execute(client, ctx, "integration-test-breaker", func() (string, error) {
		return "success", nil
	})

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
	apiKey, ingestKey, projectID, baseURL := skipIfNoEnv(t)

	client := NewClient(projectID,
		WithAPIKey(apiKey),
		WithIngestKey(ingestKey),
		WithBaseURL(baseURL),
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
	apiKey, ingestKey, projectID, baseURL := skipIfNoEnv(t)

	client := NewClient(projectID,
		WithAPIKey(apiKey),
		WithIngestKey(ingestKey),
		WithBaseURL(baseURL),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Wait for SSE to be ready
	if err := client.Ready(ctx); err != nil {
		t.Fatalf("Ready failed: %v", err)
	}

	// Execute a few tasks to generate samples
	for i := 0; i < 5; i++ {
		Execute(client, ctx, "integration-test-breaker", func() (int, error) {
			return i, nil
		})
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
