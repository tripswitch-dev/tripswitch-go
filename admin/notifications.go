package admin

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// ListNotificationChannels retrieves all notification channels for a project.
func (c *Client) ListNotificationChannels(ctx context.Context, projectID string, params ListParams, opts ...RequestOption) (*Page[NotificationChannel], error) {
	query := url.Values{}
	if params.Cursor != "" {
		query.Set("cursor", params.Cursor)
	}
	if params.Limit > 0 {
		query.Set("limit", strconv.Itoa(params.Limit))
	}

	var result Page[NotificationChannel]
	err := c.do(ctx, request{
		method:  http.MethodGet,
		path:    "/v1/projects/" + projectID + "/notification-channels",
		query:   query,
		options: opts,
	}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// CreateNotificationChannel creates a new notification channel.
func (c *Client) CreateNotificationChannel(ctx context.Context, projectID string, input CreateNotificationChannelInput, opts ...RequestOption) (*NotificationChannel, error) {
	var channel NotificationChannel
	err := c.do(ctx, request{
		method:  http.MethodPost,
		path:    "/v1/projects/" + projectID + "/notification-channels",
		body:    input,
		options: opts,
	}, &channel)
	if err != nil {
		return nil, err
	}
	return &channel, nil
}

// GetNotificationChannel retrieves a specific notification channel.
func (c *Client) GetNotificationChannel(ctx context.Context, projectID, channelID string, opts ...RequestOption) (*NotificationChannel, error) {
	var channel NotificationChannel
	err := c.do(ctx, request{
		method:  http.MethodGet,
		path:    "/v1/projects/" + projectID + "/notification-channels/" + channelID,
		options: opts,
	}, &channel)
	if err != nil {
		return nil, err
	}
	return &channel, nil
}

// UpdateNotificationChannel updates a notification channel's configuration.
func (c *Client) UpdateNotificationChannel(ctx context.Context, projectID, channelID string, input UpdateNotificationChannelInput, opts ...RequestOption) (*NotificationChannel, error) {
	var channel NotificationChannel
	err := c.do(ctx, request{
		method:  http.MethodPatch,
		path:    "/v1/projects/" + projectID + "/notification-channels/" + channelID,
		body:    input,
		options: opts,
	}, &channel)
	if err != nil {
		return nil, err
	}
	return &channel, nil
}

// DeleteNotificationChannel removes a notification channel.
func (c *Client) DeleteNotificationChannel(ctx context.Context, projectID, channelID string, opts ...RequestOption) error {
	return c.do(ctx, request{
		method:  http.MethodDelete,
		path:    "/v1/projects/" + projectID + "/notification-channels/" + channelID,
		options: opts,
	}, nil)
}

// TestNotificationChannel sends a test notification to a channel.
func (c *Client) TestNotificationChannel(ctx context.Context, projectID, channelID string, opts ...RequestOption) error {
	return c.do(ctx, request{
		method:  http.MethodPost,
		path:    "/v1/projects/" + projectID + "/notification-channels/" + channelID + "/test",
		options: opts,
	}, nil)
}

// NotificationChannelPager provides paginated iteration over notification channels.
type NotificationChannelPager struct {
	client    *Client
	ctx       context.Context
	projectID string
	params    ListParams
	opts      []RequestOption

	items   []NotificationChannel
	index   int
	cursor  string
	done    bool
	err     error
	started bool
}

// ListNotificationChannelsPager returns a pager for iterating over all notification channels.
func (c *Client) ListNotificationChannelsPager(ctx context.Context, projectID string, params ListParams, opts ...RequestOption) *NotificationChannelPager {
	return &NotificationChannelPager{
		client:    c,
		ctx:       ctx,
		projectID: projectID,
		params:    params,
		opts:      opts,
	}
}

// Next advances the pager to the next notification channel.
// Returns false when iteration is complete or an error occurs.
func (p *NotificationChannelPager) Next() bool {
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

	result, err := p.client.ListNotificationChannels(p.ctx, p.projectID, params, p.opts...)
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

// Item returns the current notification channel.
// Only valid after Next() returns true.
func (p *NotificationChannelPager) Item() NotificationChannel {
	if p.index < len(p.items) {
		item := p.items[p.index]
		p.index++
		return item
	}
	return NotificationChannel{}
}

// Err returns any error that occurred during iteration.
func (p *NotificationChannelPager) Err() error {
	return p.err
}
