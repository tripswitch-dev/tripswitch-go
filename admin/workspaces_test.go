package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestListWorkspaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/workspaces" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ListWorkspacesResponse{
			Workspaces: []Workspace{
				{ID: "ws_1", Name: "workspace-one", Slug: "workspace-one", OrgID: "org_1"},
				{ID: "ws_2", Name: "workspace-two", Slug: "workspace-two", OrgID: "org_1"},
			},
		})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	result, err := client.ListWorkspaces(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Workspaces) != 2 {
		t.Errorf("expected 2 workspaces, got %d", len(result.Workspaces))
	}
	if result.Workspaces[0].ID != "ws_1" {
		t.Errorf("expected ID 'ws_1', got %q", result.Workspaces[0].ID)
	}
}

func TestCreateWorkspace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/workspaces" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var input CreateWorkspaceInput
		json.NewDecoder(r.Body).Decode(&input)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(Workspace{
			ID:         "ws_new",
			Name:       input.Name,
			Slug:       input.Slug,
			OrgID:      "org_1",
			InsertedAt: time.Now(),
		})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	workspace, err := client.CreateWorkspace(context.Background(), CreateWorkspaceInput{
		Name: "my-workspace",
		Slug: "my-workspace",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if workspace.Name != "my-workspace" {
		t.Errorf("expected name 'my-workspace', got %q", workspace.Name)
	}
	if workspace.Slug != "my-workspace" {
		t.Errorf("expected slug 'my-workspace', got %q", workspace.Slug)
	}
}

func TestGetWorkspace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/workspaces/ws_123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Workspace{
			ID:    "ws_123",
			Name:  "Test Workspace",
			Slug:  "test-workspace",
			OrgID: "org_1",
		})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	workspace, err := client.GetWorkspace(context.Background(), "ws_123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if workspace.ID != "ws_123" {
		t.Errorf("expected ID 'ws_123', got %q", workspace.ID)
	}
	if workspace.Name != "Test Workspace" {
		t.Errorf("expected Name 'Test Workspace', got %q", workspace.Name)
	}
}

func TestUpdateWorkspace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		if r.URL.Path != "/v1/workspaces/ws_123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var input UpdateWorkspaceInput
		json.NewDecoder(r.Body).Decode(&input)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Workspace{
			ID:    "ws_123",
			Name:  *input.Name,
			Slug:  "test-workspace",
			OrgID: "org_1",
		})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	workspace, err := client.UpdateWorkspace(context.Background(), "ws_123", UpdateWorkspaceInput{
		Name: Ptr("Updated Workspace"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if workspace.Name != "Updated Workspace" {
		t.Errorf("expected Name 'Updated Workspace', got %q", workspace.Name)
	}
}

func TestDeleteWorkspace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/v1/workspaces/ws_123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	err := client.DeleteWorkspace(context.Background(), "ws_123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
