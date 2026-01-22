package admin

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// ListEvents retrieves state transition events for a project.
func (c *Client) ListEvents(ctx context.Context, projectID string, params ListEventsParams, opts ...RequestOption) (*ListEventsResponse, error) {
	query := url.Values{}
	if params.BreakerID != "" {
		query.Set("breaker_id", params.BreakerID)
	}
	if !params.StartTime.IsZero() {
		query.Set("start_time", params.StartTime.Format("2006-01-02T15:04:05Z07:00"))
	}
	if !params.EndTime.IsZero() {
		query.Set("end_time", params.EndTime.Format("2006-01-02T15:04:05Z07:00"))
	}
	if params.Cursor != "" {
		query.Set("cursor", params.Cursor)
	}
	if params.Limit > 0 {
		query.Set("limit", strconv.Itoa(params.Limit))
	}

	var result ListEventsResponse
	err := c.do(ctx, request{
		method:  http.MethodGet,
		path:    "/v1/projects/" + projectID + "/events",
		query:   query,
		options: opts,
	}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// GetStatus is no longer available in the admin client.
// Use tripswitch.Client.GetStatus() instead, which requires a project API key (eb_pk_).
// The status endpoint is a runtime read operation and belongs with the runtime client.

// EventPager provides paginated iteration over events.
type EventPager struct {
	client    *Client
	ctx       context.Context
	projectID string
	params    ListEventsParams
	opts      []RequestOption

	items   []Event
	index   int
	cursor  string
	done    bool
	err     error
	started bool
}

// ListEventsPager returns a pager for iterating over all events.
func (c *Client) ListEventsPager(ctx context.Context, projectID string, params ListEventsParams, opts ...RequestOption) *EventPager {
	return &EventPager{
		client:    c,
		ctx:       ctx,
		projectID: projectID,
		params:    params,
		opts:      opts,
	}
}

// Next advances the pager to the next event.
// Returns false when iteration is complete or an error occurs.
func (p *EventPager) Next() bool {
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

	result, err := p.client.ListEvents(p.ctx, p.projectID, params, p.opts...)
	if err != nil {
		p.err = err
		return false
	}

	p.items = result.Events
	p.index = 0
	if result.NextCursor != nil {
		p.cursor = *result.NextCursor
		p.done = false
	} else {
		p.done = true
	}

	return len(p.items) > 0
}

// Item returns the current event.
// Only valid after Next() returns true.
func (p *EventPager) Item() Event {
	if p.index < len(p.items) {
		item := p.items[p.index]
		p.index++
		return item
	}
	return Event{}
}

// Err returns any error that occurred during iteration.
func (p *EventPager) Err() error {
	return p.err
}
