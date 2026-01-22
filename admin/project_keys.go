package admin

import (
	"context"
	"net/http"
)

// ListProjectKeys lists all API keys for a project.
// Requires an admin API key (eb_admin_).
func (c *Client) ListProjectKeys(ctx context.Context, projectID string, opts ...RequestOption) (*ListProjectKeysResponse, error) {
	var result ListProjectKeysResponse
	err := c.do(ctx, request{
		method:  http.MethodGet,
		path:    "/v1/projects/" + projectID + "/keys",
		options: opts,
	}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// CreateProjectKey creates a new project API key.
// The returned Key field contains the full API key (eb_pk_...) and is only
// available on creation. Store it securely as it cannot be retrieved later.
// Requires an admin API key (eb_admin_).
func (c *Client) CreateProjectKey(ctx context.Context, projectID string, input CreateProjectKeyInput, opts ...RequestOption) (*CreateProjectKeyResponse, error) {
	var result CreateProjectKeyResponse
	err := c.do(ctx, request{
		method:  http.MethodPost,
		path:    "/v1/projects/" + projectID + "/keys",
		body:    input,
		options: opts,
	}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteProjectKey revokes a project API key.
// Once revoked, the key can no longer be used for authentication.
// Requires an admin API key (eb_admin_).
func (c *Client) DeleteProjectKey(ctx context.Context, projectID, keyID string, opts ...RequestOption) error {
	return c.do(ctx, request{
		method:  http.MethodDelete,
		path:    "/v1/projects/" + projectID + "/keys/" + keyID,
		options: opts,
	}, nil)
}
