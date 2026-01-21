package admin

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// ListBreakers retrieves all breakers for a project.
func (c *Client) ListBreakers(ctx context.Context, projectID string, params ListParams, opts ...RequestOption) (*ListBreakersResponse, error) {
	query := url.Values{}
	if params.Cursor != "" {
		query.Set("cursor", params.Cursor)
	}
	if params.Limit > 0 {
		query.Set("limit", strconv.Itoa(params.Limit))
	}

	var result ListBreakersResponse
	err := c.do(ctx, request{
		method:  http.MethodGet,
		path:    "/v1/projects/" + projectID + "/breakers",
		query:   query,
		options: opts,
	}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// breakerResponse wraps single breaker responses from the API.
type breakerResponse struct {
	Breaker  Breaker `json:"breaker"`
	RouterID string  `json:"router_id,omitempty"`
}

// CreateBreaker creates a new breaker.
func (c *Client) CreateBreaker(ctx context.Context, projectID string, input CreateBreakerInput, opts ...RequestOption) (*Breaker, error) {
	var resp breakerResponse
	err := c.do(ctx, request{
		method:  http.MethodPost,
		path:    "/v1/projects/" + projectID + "/breakers",
		body:    input,
		options: opts,
	}, &resp)
	if err != nil {
		return nil, err
	}
	// Copy router_id from wrapper to breaker
	resp.Breaker.RouterID = resp.RouterID
	return &resp.Breaker, nil
}

// SyncBreakers replaces all breakers for a project (bulk sync).
func (c *Client) SyncBreakers(ctx context.Context, projectID string, input SyncBreakersInput, opts ...RequestOption) ([]Breaker, error) {
	var breakers []Breaker
	err := c.do(ctx, request{
		method:  http.MethodPut,
		path:    "/v1/projects/" + projectID + "/breakers",
		body:    input,
		options: opts,
	}, &breakers)
	if err != nil {
		return nil, err
	}
	return breakers, nil
}

// GetBreaker retrieves a specific breaker.
func (c *Client) GetBreaker(ctx context.Context, projectID, breakerID string, opts ...RequestOption) (*Breaker, error) {
	var resp breakerResponse
	err := c.do(ctx, request{
		method:  http.MethodGet,
		path:    "/v1/projects/" + projectID + "/breakers/" + breakerID,
		options: opts,
	}, &resp)
	if err != nil {
		return nil, err
	}
	// Copy router_id from wrapper to breaker
	resp.Breaker.RouterID = resp.RouterID
	return &resp.Breaker, nil
}

// UpdateBreaker updates a breaker's configuration.
func (c *Client) UpdateBreaker(ctx context.Context, projectID, breakerID string, input UpdateBreakerInput, opts ...RequestOption) (*Breaker, error) {
	var resp breakerResponse
	err := c.do(ctx, request{
		method:  http.MethodPatch,
		path:    "/v1/projects/" + projectID + "/breakers/" + breakerID,
		body:    input,
		options: opts,
	}, &resp)
	if err != nil {
		return nil, err
	}
	// Copy router_id from wrapper to breaker
	resp.Breaker.RouterID = resp.RouterID
	return &resp.Breaker, nil
}

// DeleteBreaker removes a breaker.
func (c *Client) DeleteBreaker(ctx context.Context, projectID, breakerID string, opts ...RequestOption) error {
	return c.do(ctx, request{
		method:  http.MethodDelete,
		path:    "/v1/projects/" + projectID + "/breakers/" + breakerID,
		options: opts,
	}, nil)
}

// GetBreakerState retrieves the current state of a breaker.
func (c *Client) GetBreakerState(ctx context.Context, projectID, breakerID string, opts ...RequestOption) (*BreakerState, error) {
	var state BreakerState
	err := c.do(ctx, request{
		method:  http.MethodGet,
		path:    "/v1/projects/" + projectID + "/breakers/" + breakerID + "/state",
		options: opts,
	}, &state)
	if err != nil {
		return nil, err
	}
	return &state, nil
}

// BatchGetBreakerStates retrieves states for multiple breakers.
func (c *Client) BatchGetBreakerStates(ctx context.Context, projectID string, input BatchGetBreakerStatesInput, opts ...RequestOption) ([]BreakerState, error) {
	var states []BreakerState
	err := c.do(ctx, request{
		method:  http.MethodPost,
		path:    "/v1/projects/" + projectID + "/breakers/state:batch",
		body:    input,
		options: opts,
	}, &states)
	if err != nil {
		return nil, err
	}
	return states, nil
}

// BreakerPager provides paginated iteration over breakers.
type BreakerPager struct {
	client    *Client
	ctx       context.Context
	projectID string
	params    ListParams
	opts      []RequestOption

	items   []Breaker
	index   int
	cursor  string
	done    bool
	err     error
	started bool
}

// ListBreakersPager returns a pager for iterating over all breakers.
func (c *Client) ListBreakersPager(ctx context.Context, projectID string, params ListParams, opts ...RequestOption) *BreakerPager {
	return &BreakerPager{
		client:    c,
		ctx:       ctx,
		projectID: projectID,
		params:    params,
		opts:      opts,
	}
}

// Next advances the pager to the next breaker.
// Returns false when iteration is complete or an error occurs.
func (p *BreakerPager) Next() bool {
	// Check if we have more items in current page
	if p.index < len(p.items) {
		return true
	}

	// Check if we're done
	if p.done {
		return false
	}

	// Check for previous error
	if p.err != nil {
		return false
	}

	// Fetch next page
	params := p.params
	if p.started {
		params.Cursor = p.cursor
	}
	p.started = true

	result, err := p.client.ListBreakers(p.ctx, p.projectID, params, p.opts...)
	if err != nil {
		p.err = err
		return false
	}

	p.items = result.Breakers
	p.index = 0
	// API doesn't support cursor-based pagination currently
	p.done = true

	return len(p.items) > 0
}

// Item returns the current breaker.
// Only valid after Next() returns true.
func (p *BreakerPager) Item() Breaker {
	if p.index < len(p.items) {
		item := p.items[p.index]
		p.index++
		return item
	}
	return Breaker{}
}

// Err returns any error that occurred during iteration.
func (p *BreakerPager) Err() error {
	return p.err
}
