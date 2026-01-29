package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// ErrDeleteConfirmation is returned when DeleteProject is called without
// WithConfirmDeleteProjectName or with a name that doesn't match.
var ErrDeleteConfirmation = errors.New("tripswitch: delete confirmation failed")

// ListProjects retrieves all projects for the authenticated org.
func (c *Client) ListProjects(ctx context.Context, opts ...RequestOption) (*ListProjectsResponse, error) {
	var result ListProjectsResponse
	err := c.do(ctx, request{
		method:  http.MethodGet,
		path:    "/v1/projects",
		options: opts,
	}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// CreateProject creates a new project.
func (c *Client) CreateProject(ctx context.Context, input CreateProjectInput, opts ...RequestOption) (*Project, error) {
	var project Project
	err := c.do(ctx, request{
		method:  http.MethodPost,
		path:    "/v1/projects",
		body:    input,
		options: opts,
	}, &project)
	if err != nil {
		return nil, err
	}
	return &project, nil
}

// DeleteProject deletes a project by ID.
//
// This method requires WithConfirmDeleteProjectName to prevent accidental
// deletion. The provided name is verified against the project before the
// delete request is sent.
//
//	err := client.DeleteProject(ctx, "proj_123",
//	    admin.WithConfirmDeleteProjectName("prod-payments"),
//	)
func (c *Client) DeleteProject(ctx context.Context, projectID string, opts ...DeleteProjectOption) error {
	cfg := &deleteProjectConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.confirmName == "" {
		return fmt.Errorf("%w: WithConfirmDeleteProjectName is required", ErrDeleteConfirmation)
	}

	// Fetch the project to verify the name matches.
	project, err := c.GetProject(ctx, projectID, cfg.requestOptions...)
	if err != nil {
		return err
	}

	if project.Name != cfg.confirmName {
		return fmt.Errorf("%w: project name %q does not match confirmation name %q", ErrDeleteConfirmation, project.Name, cfg.confirmName)
	}

	return c.do(ctx, request{
		method:  http.MethodDelete,
		path:    "/v1/projects/" + projectID,
		options: cfg.requestOptions,
	}, nil)
}

// GetProject retrieves a project by ID.
func (c *Client) GetProject(ctx context.Context, projectID string, opts ...RequestOption) (*Project, error) {
	var project Project
	err := c.do(ctx, request{
		method:  http.MethodGet,
		path:    "/v1/projects/" + projectID,
		options: opts,
	}, &project)
	if err != nil {
		return nil, err
	}
	return &project, nil
}

// UpdateProject updates a project's settings.
func (c *Client) UpdateProject(ctx context.Context, projectID string, input UpdateProjectInput, opts ...RequestOption) (*Project, error) {
	var project Project
	err := c.do(ctx, request{
		method:  http.MethodPatch,
		path:    "/v1/projects/" + projectID,
		body:    input,
		options: opts,
	}, &project)
	if err != nil {
		return nil, err
	}
	return &project, nil
}

// RotateIngestSecret rotates the ingest secret for a project.
// Returns the new ingest secret.
func (c *Client) RotateIngestSecret(ctx context.Context, projectID string, opts ...RequestOption) (*IngestSecretRotation, error) {
	var result IngestSecretRotation
	err := c.do(ctx, request{
		method:  http.MethodPost,
		path:    "/v1/projects/" + projectID + "/ingest_secret/rotate",
		options: opts,
	}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}
