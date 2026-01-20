package admin

import (
	"context"
	"net/http"
)

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
