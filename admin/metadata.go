package admin

import (
	"context"
	"net/http"
)

// UpdateBreakerMetadata performs a merge-patch update on a breaker's metadata.
func (c *Client) UpdateBreakerMetadata(ctx context.Context, projectID, breakerID string, metadata map[string]string, opts ...RequestOption) error {
	return c.do(ctx, request{
		method:  http.MethodPatch,
		path:    "/v1/projects/" + projectID + "/breakers/" + breakerID + "/metadata",
		body:    metadata,
		options: opts,
	}, nil)
}

// UpdateRouterMetadata performs a merge-patch update on a router's metadata.
func (c *Client) UpdateRouterMetadata(ctx context.Context, projectID, routerID string, metadata map[string]string, opts ...RequestOption) error {
	return c.do(ctx, request{
		method:  http.MethodPatch,
		path:    "/v1/projects/" + projectID + "/routers/" + routerID + "/metadata",
		body:    metadata,
		options: opts,
	}, nil)
}
