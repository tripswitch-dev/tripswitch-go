package admin

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// ListRouters retrieves all routers for a project.
func (c *Client) ListRouters(ctx context.Context, projectID string, params ListParams, opts ...RequestOption) (*Page[Router], error) {
	query := url.Values{}
	if params.Cursor != "" {
		query.Set("cursor", params.Cursor)
	}
	if params.Limit > 0 {
		query.Set("limit", strconv.Itoa(params.Limit))
	}

	var result Page[Router]
	err := c.do(ctx, request{
		method:  http.MethodGet,
		path:    "/v1/projects/" + projectID + "/routers",
		query:   query,
		options: opts,
	}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// CreateRouter creates a new router.
func (c *Client) CreateRouter(ctx context.Context, projectID string, input CreateRouterInput, opts ...RequestOption) (*Router, error) {
	var router Router
	err := c.do(ctx, request{
		method:  http.MethodPost,
		path:    "/v1/projects/" + projectID + "/routers",
		body:    input,
		options: opts,
	}, &router)
	if err != nil {
		return nil, err
	}
	return &router, nil
}

// GetRouter retrieves a specific router.
func (c *Client) GetRouter(ctx context.Context, projectID, routerID string, opts ...RequestOption) (*Router, error) {
	var router Router
	err := c.do(ctx, request{
		method:  http.MethodGet,
		path:    "/v1/projects/" + projectID + "/routers/" + routerID,
		options: opts,
	}, &router)
	if err != nil {
		return nil, err
	}
	return &router, nil
}

// UpdateRouter updates a router's configuration.
func (c *Client) UpdateRouter(ctx context.Context, projectID, routerID string, input UpdateRouterInput, opts ...RequestOption) (*Router, error) {
	var router Router
	err := c.do(ctx, request{
		method:  http.MethodPatch,
		path:    "/v1/projects/" + projectID + "/routers/" + routerID,
		body:    input,
		options: opts,
	}, &router)
	if err != nil {
		return nil, err
	}
	return &router, nil
}

// DeleteRouter removes a router.
// The router must have no linked breakers.
func (c *Client) DeleteRouter(ctx context.Context, projectID, routerID string, opts ...RequestOption) error {
	return c.do(ctx, request{
		method:  http.MethodDelete,
		path:    "/v1/projects/" + projectID + "/routers/" + routerID,
		options: opts,
	}, nil)
}

// LinkBreaker links a breaker to a router.
func (c *Client) LinkBreaker(ctx context.Context, projectID, routerID string, input LinkBreakerInput, opts ...RequestOption) error {
	return c.do(ctx, request{
		method:  http.MethodPost,
		path:    "/v1/projects/" + projectID + "/routers/" + routerID + "/breakers",
		body:    input,
		options: opts,
	}, nil)
}

// UnlinkBreaker removes a breaker from a router.
func (c *Client) UnlinkBreaker(ctx context.Context, projectID, routerID, breakerID string, opts ...RequestOption) error {
	return c.do(ctx, request{
		method:  http.MethodDelete,
		path:    "/v1/projects/" + projectID + "/routers/" + routerID + "/breakers/" + breakerID,
		options: opts,
	}, nil)
}

// RouterPager provides paginated iteration over routers.
type RouterPager struct {
	client    *Client
	ctx       context.Context
	projectID string
	params    ListParams
	opts      []RequestOption

	items   []Router
	index   int
	cursor  string
	done    bool
	err     error
	started bool
}

// ListRoutersPager returns a pager for iterating over all routers.
func (c *Client) ListRoutersPager(ctx context.Context, projectID string, params ListParams, opts ...RequestOption) *RouterPager {
	return &RouterPager{
		client:    c,
		ctx:       ctx,
		projectID: projectID,
		params:    params,
		opts:      opts,
	}
}

// Next advances the pager to the next router.
// Returns false when iteration is complete or an error occurs.
func (p *RouterPager) Next() bool {
	if p.index < len(p.items) {
		return true
	}

	if p.done {
		return false
	}

	if p.err != nil {
		return false
	}

	params := p.params
	if p.started {
		params.Cursor = p.cursor
	}
	p.started = true

	result, err := p.client.ListRouters(p.ctx, p.projectID, params, p.opts...)
	if err != nil {
		p.err = err
		return false
	}

	p.items = result.Items
	p.index = 0
	p.cursor = result.NextCursor
	p.done = result.NextCursor == ""

	return len(p.items) > 0
}

// Item returns the current router.
// Only valid after Next() returns true.
func (p *RouterPager) Item() Router {
	if p.index < len(p.items) {
		item := p.items[p.index]
		p.index++
		return item
	}
	return Router{}
}

// Err returns any error that occurred during iteration.
func (p *RouterPager) Err() error {
	return p.err
}
