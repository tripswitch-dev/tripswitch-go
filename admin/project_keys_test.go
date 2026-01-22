package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListProjectKeys(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/projects/proj_123/keys" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer eb_admin_test" {
			t.Errorf("unexpected auth header: %s", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ListProjectKeysResponse{
			Keys: []ProjectKey{
				{ID: "key_1", Name: "production", KeyPrefix: "eb_pk_abc123"},
				{ID: "key_2", Name: "staging", KeyPrefix: "eb_pk_def456"},
			},
			Count: 2,
		})
	}))
	defer server.Close()

	client := NewClient(
		WithAPIKey("eb_admin_test"),
		WithBaseURL(server.URL),
	)

	result, err := client.ListProjectKeys(context.Background(), "proj_123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(result.Keys))
	}
	if result.Count != 2 {
		t.Errorf("expected count 2, got %d", result.Count)
	}
	if result.Keys[0].Name != "production" {
		t.Errorf("expected name 'production', got %q", result.Keys[0].Name)
	}
}

func TestCreateProjectKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/projects/proj_123/keys" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var input CreateProjectKeyInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if input.Name != "my-service-key" {
			t.Errorf("unexpected name: %s", input.Name)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(CreateProjectKeyResponse{
			ID:        "key_new",
			Name:      input.Name,
			Key:       "eb_pk_full_secret_key_here",
			KeyPrefix: "eb_pk_full_se",
			Message:   "Store this key securely - it cannot be retrieved later.",
		})
	}))
	defer server.Close()

	client := NewClient(
		WithAPIKey("eb_admin_test"),
		WithBaseURL(server.URL),
	)

	result, err := client.CreateProjectKey(context.Background(), "proj_123", CreateProjectKeyInput{
		Name: "my-service-key",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != "key_new" {
		t.Errorf("expected ID 'key_new', got %q", result.ID)
	}
	if result.Key != "eb_pk_full_secret_key_here" {
		t.Errorf("expected full key, got %q", result.Key)
	}
	if result.KeyPrefix != "eb_pk_full_se" {
		t.Errorf("expected key prefix, got %q", result.KeyPrefix)
	}
}

func TestDeleteProjectKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/v1/projects/proj_123/keys/key_456" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(
		WithAPIKey("eb_admin_test"),
		WithBaseURL(server.URL),
	)

	err := client.DeleteProjectKey(context.Background(), "proj_123", "key_456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateProjectKey_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Missing or invalid admin API key",
		})
	}))
	defer server.Close()

	client := NewClient(
		WithAPIKey("eb_pk_wrong_key_type"),
		WithBaseURL(server.URL),
	)

	_, err := client.CreateProjectKey(context.Background(), "proj_123", CreateProjectKeyInput{
		Name: "test",
	})
	if !IsUnauthorized(err) {
		t.Errorf("expected unauthorized error, got %v", err)
	}
}

func TestCreateProjectKey_Forbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Project belongs to a different org",
		})
	}))
	defer server.Close()

	client := NewClient(
		WithAPIKey("eb_admin_other_org"),
		WithBaseURL(server.URL),
	)

	_, err := client.CreateProjectKey(context.Background(), "proj_123", CreateProjectKeyInput{
		Name: "test",
	})
	if !IsForbidden(err) {
		t.Errorf("expected forbidden error, got %v", err)
	}
}
