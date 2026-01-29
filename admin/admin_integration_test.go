package admin

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// Integration tests are gated by environment variables.
// Run with:
//   TRIPSWITCH_API_KEY=eb_admin_... TRIPSWITCH_PROJECT_ID=proj_... go test -v -run Integration
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

func TestIntegration_ProjectCRUD(t *testing.T) {
	apiKey, _, baseURL := skipIfNoEnv(t)

	client := NewClient(
		WithAPIKey(apiKey),
		WithBaseURL(baseURL),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	projectName := fmt.Sprintf("integration-test-project-%d", time.Now().UnixNano())

	// Create
	project, err := client.CreateProject(ctx, CreateProjectInput{
		Name: projectName,
	})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	t.Logf("Created project: %s (%s)", project.Name, project.ID)

	// Best-effort cleanup in case the test fails before reaching Delete.
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = client.DeleteProject(cleanupCtx, project.ID,
			WithConfirmDeleteProjectName(projectName),
		)
	})

	// List
	result, err := client.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}
	found := false
	for _, p := range result.Projects {
		if p.ID == project.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("created project %s not found in list", project.ID)
	}
	t.Logf("Found %d projects", len(result.Projects))

	// Delete
	err = client.DeleteProject(ctx, project.ID,
		WithConfirmDeleteProjectName(projectName),
	)
	if err != nil {
		t.Fatalf("DeleteProject failed: %v", err)
	}
	t.Log("Deleted project")

	// Verify deletion
	_, err = client.GetProject(ctx, project.ID)
	if !IsNotFound(err) {
		t.Errorf("expected NotFound error after deletion, got: %v", err)
	}
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

	t.Logf("Found %d breakers", len(result.Breakers))
	for _, b := range result.Breakers {
		t.Logf("  - %s (%s)", b.Name, b.ID)
	}
}

// TestIntegration_GetStatus removed - GetStatus moved to runtime client (tripswitch.Client)
// because it requires a project API key (eb_pk_), not an admin key.

func TestIntegration_BreakerCRUD(t *testing.T) {
	apiKey, projectID, baseURL := skipIfNoEnv(t)

	client := NewClient(
		WithAPIKey(apiKey),
		WithBaseURL(baseURL),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use unique name to avoid conflicts with previous test runs
	breakerName := fmt.Sprintf("integration-test-breaker-%d", time.Now().UnixNano())

	// Create
	breaker, err := client.CreateBreaker(ctx, projectID, CreateBreakerInput{
		Name:      breakerName,
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
	if fetched.Name != breakerName {
		t.Errorf("expected name %q, got %q", breakerName, fetched.Name)
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

	// Note: GetBreakerState requires a project key (eb_pk_), not an admin key.
	// State reads are runtime operations, not governance operations.

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

	t.Logf("Found %d routers", len(result.Routers))
	for _, r := range result.Routers {
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

	t.Logf("Found %d events", len(result.Events))
	for _, e := range result.Events {
		t.Logf("  - %s: %s -> %s", e.BreakerID, e.FromState, e.ToState)
	}
}
