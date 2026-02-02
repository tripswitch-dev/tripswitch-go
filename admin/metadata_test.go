package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpdateBreakerMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		if want := "/v1/projects/proj_123/breakers/brk_456/metadata"; r.URL.Path != want {
			t.Errorf("unexpected path: got %s, want %s", r.URL.Path, want)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer eb_admin_test" {
			t.Errorf("unexpected auth header: %s", auth)
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if body["env"] != "prod" {
			t.Errorf("expected env=prod, got %q", body["env"])
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(
		WithAPIKey("eb_admin_test"),
		WithBaseURL(server.URL),
	)

	err := client.UpdateBreakerMetadata(context.Background(), "proj_123", "brk_456", map[string]string{"env": "prod"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateRouterMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		if want := "/v1/projects/proj_123/routers/rtr_789/metadata"; r.URL.Path != want {
			t.Errorf("unexpected path: got %s, want %s", r.URL.Path, want)
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if body["team"] != "platform" {
			t.Errorf("expected team=platform, got %q", body["team"])
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(
		WithAPIKey("eb_admin_test"),
		WithBaseURL(server.URL),
	)

	err := client.UpdateRouterMetadata(context.Background(), "proj_123", "rtr_789", map[string]string{"team": "platform"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
