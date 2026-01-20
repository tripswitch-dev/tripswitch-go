package admin

import (
	"context"
	"os"
	"testing"
	"time"
)

// Integration tests are gated by environment variables.
// Run with:
//   TRIPSWITCH_API_KEY=sk_... TRIPSWITCH_PROJECT_ID=proj_... go test -v -run Integration
//
// Optional:
//   TRIPSWITCH_BASE_URL=https://api.tripswitch.dev (defaults to production)

func skipIfNoEnv(t *testing.T) (apiKey, projectID, baseURL string) {
	apiKey = os.Getenv("TRIPSWITCH_API_KEY")
	projectID = os.Getenv("TRIPSWITCH_PROJECT_ID")
	baseURL = os.Getenv("TRIPSWITCH_BASE_URL")

	if apiKey == "" || projectID == "" {
		t.Skip("Skipping integration test: TRIPSWITCH_API_KEY and TRIPSWITCH_PROJECT_ID must be set")
	}

	if baseURL == "" {
		baseURL = "https://api.tripswitch.dev"
	}

	return apiKey, projectID, baseURL
}

func TestIntegration_GetProject(t *testing.T) {
	apiKey, projectID, baseURL := skipIfNoEnv(t)

	client := NewClient(
		WithAPIKey(apiKey),
		WithBaseURL(baseURL),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	project, err := client.GetProject(ctx, projectID)
	if err != nil {
		t.Fatalf("GetProject failed: %v", err)
	}

	if project.ID != projectID {
		t.Errorf("expected project ID %q, got %q", projectID, project.ID)
	}

	t.Logf("Project: %s (%s)", project.Name, project.ID)
}

func TestIntegration_ListBreakers(t *testing.T) {
	apiKey, projectID, baseURL := skipIfNoEnv(t)

	client := NewClient(
		WithAPIKey(apiKey),
		WithBaseURL(baseURL),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := client.ListBreakers(ctx, projectID, ListParams{Limit: 10})
	if err != nil {
		t.Fatalf("ListBreakers failed: %v", err)
	}

	t.Logf("Found %d breakers", len(result.Items))
	for _, b := range result.Items {
		t.Logf("  - %s (%s)", b.Name, b.ID)
	}
}

func TestIntegration_GetStatus(t *testing.T) {
	apiKey, projectID, baseURL := skipIfNoEnv(t)

	client := NewClient(
		WithAPIKey(apiKey),
		WithBaseURL(baseURL),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	status, err := client.GetStatus(ctx, projectID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}

	t.Logf("Status: %d total breakers (%d open, %d closed, %d half-open)",
		status.TotalBreakers,
		status.OpenBreakers,
		status.ClosedBreakers,
		status.HalfOpenBreakers,
	)
}

func TestIntegration_BreakerCRUD(t *testing.T) {
	apiKey, projectID, baseURL := skipIfNoEnv(t)

	client := NewClient(
		WithAPIKey(apiKey),
		WithBaseURL(baseURL),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create
	breaker, err := client.CreateBreaker(ctx, projectID, CreateBreakerInput{
		Name:      "integration-test-breaker",
		Metric:    "test_metric",
		Kind:      BreakerKindErrorRate,
		Op:        BreakerOpGt,
		Threshold: 0.5,
		WindowMs:  60000,
		MinCount:  10,
	})
	if err != nil {
		t.Fatalf("CreateBreaker failed: %v", err)
	}
	t.Logf("Created breaker: %s", breaker.ID)

	// Read
	fetched, err := client.GetBreaker(ctx, projectID, breaker.ID)
	if err != nil {
		t.Fatalf("GetBreaker failed: %v", err)
	}
	if fetched.Name != "integration-test-breaker" {
		t.Errorf("expected name 'integration-test-breaker', got %q", fetched.Name)
	}

	// Update
	updated, err := client.UpdateBreaker(ctx, projectID, breaker.ID, UpdateBreakerInput{
		Threshold: Ptr(0.75),
	})
	if err != nil {
		t.Fatalf("UpdateBreaker failed: %v", err)
	}
	if updated.Threshold != 0.75 {
		t.Errorf("expected threshold 0.75, got %f", updated.Threshold)
	}

	// Get state
	state, err := client.GetBreakerState(ctx, projectID, breaker.ID)
	if err != nil {
		t.Fatalf("GetBreakerState failed: %v", err)
	}
	t.Logf("Breaker state: %s (allow_rate: %.2f)", state.State, state.AllowRate)

	// Delete
	err = client.DeleteBreaker(ctx, projectID, breaker.ID)
	if err != nil {
		t.Fatalf("DeleteBreaker failed: %v", err)
	}
	t.Log("Deleted breaker")

	// Verify deletion
	_, err = client.GetBreaker(ctx, projectID, breaker.ID)
	if !IsNotFound(err) {
		t.Errorf("expected NotFound error after deletion, got: %v", err)
	}
}

func TestIntegration_ListRouters(t *testing.T) {
	apiKey, projectID, baseURL := skipIfNoEnv(t)

	client := NewClient(
		WithAPIKey(apiKey),
		WithBaseURL(baseURL),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := client.ListRouters(ctx, projectID, ListParams{Limit: 10})
	if err != nil {
		t.Fatalf("ListRouters failed: %v", err)
	}

	t.Logf("Found %d routers", len(result.Items))
	for _, r := range result.Items {
		t.Logf("  - %s (%s) mode=%s", r.Name, r.ID, r.Mode)
	}
}

func TestIntegration_ListNotificationChannels(t *testing.T) {
	apiKey, projectID, baseURL := skipIfNoEnv(t)

	client := NewClient(
		WithAPIKey(apiKey),
		WithBaseURL(baseURL),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := client.ListNotificationChannels(ctx, projectID, ListParams{Limit: 10})
	if err != nil {
		t.Fatalf("ListNotificationChannels failed: %v", err)
	}

	t.Logf("Found %d notification channels", len(result.Items))
	for _, c := range result.Items {
		t.Logf("  - %s (%s) type=%s", c.Name, c.ID, c.Channel)
	}
}

func TestIntegration_ListEvents(t *testing.T) {
	apiKey, projectID, baseURL := skipIfNoEnv(t)

	client := NewClient(
		WithAPIKey(apiKey),
		WithBaseURL(baseURL),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := client.ListEvents(ctx, projectID, ListEventsParams{Limit: 10})
	if err != nil {
		t.Fatalf("ListEvents failed: %v", err)
	}

	t.Logf("Found %d events", len(result.Items))
	for _, e := range result.Items {
		t.Logf("  - %s: %s -> %s", e.BreakerID, e.FromState, e.ToState)
	}
}
