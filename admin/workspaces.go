package admin

import (
	"context"
	"net/http"
)

// ListWorkspaces retrieves all workspaces for the authenticated org.
func (c *Client) ListWorkspaces(ctx context.Context, opts ...RequestOption) ([]Workspace, error) {
	var result []Workspace
	err := c.do(ctx, request{
		method:  http.MethodGet,
		path:    "/v1/workspaces",
		options: opts,
	}, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// CreateWorkspace creates a new workspace.
func (c *Client) CreateWorkspace(ctx context.Context, input CreateWorkspaceInput, opts ...RequestOption) (*Workspace, error) {
	var workspace Workspace
	err := c.do(ctx, request{
		method:  http.MethodPost,
		path:    "/v1/workspaces",
		body:    input,
		options: opts,
	}, &workspace)
	if err != nil {
		return nil, err
	}
	return &workspace, nil
}

// GetWorkspace retrieves a workspace by ID.
func (c *Client) GetWorkspace(ctx context.Context, workspaceID string, opts ...RequestOption) (*Workspace, error) {
	var workspace Workspace
	err := c.do(ctx, request{
		method:  http.MethodGet,
		path:    "/v1/workspaces/" + workspaceID,
		options: opts,
	}, &workspace)
	if err != nil {
		return nil, err
	}
	return &workspace, nil
}

// UpdateWorkspace updates a workspace's settings.
func (c *Client) UpdateWorkspace(ctx context.Context, workspaceID string, input UpdateWorkspaceInput, opts ...RequestOption) (*Workspace, error) {
	var workspace Workspace
	err := c.do(ctx, request{
		method:  http.MethodPatch,
		path:    "/v1/workspaces/" + workspaceID,
		body:    input,
		options: opts,
	}, &workspace)
	if err != nil {
		return nil, err
	}
	return &workspace, nil
}

// DeleteWorkspace deletes a workspace by ID.
func (c *Client) DeleteWorkspace(ctx context.Context, workspaceID string, opts ...RequestOption) error {
	return c.do(ctx, request{
		method:  http.MethodDelete,
		path:    "/v1/workspaces/" + workspaceID,
		options: opts,
	}, nil)
}
