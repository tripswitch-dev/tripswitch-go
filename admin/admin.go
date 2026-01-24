// Package admin provides a client for the Tripswitch management API.
//
// This package complements the runtime tripswitch client by exposing
// administrative operations for managing projects, breakers, routers,
// notification channels, and querying events and status.
//
// # Authentication
//
// The admin client requires an admin API key (prefix: eb_admin_).
// Admin keys are org-scoped and grant access to all projects within the org.
// Create admin keys via the Tripswitch dashboard or API.
//
// # Quick Start
//
//	client := admin.NewClient(
//	    admin.WithAPIKey("eb_admin_..."),
//	)
//
//	// Get project details
//	project, err := client.GetProject(ctx, "proj_abc123")
//
//	// List breakers
//	page, err := client.ListBreakers(ctx, "proj_abc123", admin.ListParams{Limit: 100})
//
//	// Create a breaker
//	breaker, err := client.CreateBreaker(ctx, "proj_abc123", admin.CreateBreakerInput{
//	    Name:      "api-latency",
//	    Metric:    "latency_ms",
//	    Kind:      admin.BreakerKindP95,
//	    Op:        admin.BreakerOpGt,
//	    Threshold: 500,
//	})
//
// # Error Handling
//
// All methods return typed errors that can be inspected:
//
//	breaker, err := client.GetBreaker(ctx, projectID, breakerID)
//	if errors.Is(err, admin.ErrNotFound) {
//	    // Handle 404
//	}
//
//	var apiErr *admin.APIError
//	if errors.As(err, &apiErr) {
//	    log.Printf("status=%d code=%s", apiErr.Status, apiErr.Code)
//	}
package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultBaseURL = "https://api.tripswitch.dev"
	defaultTimeout = 30 * time.Second
)

// Client is the Tripswitch admin API client.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// Option configures a Client.
type Option func(*Client)

// NewClient creates a new admin API client.
func NewClient(opts ...Option) *Client {
	c := &Client{
		baseURL: defaultBaseURL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// WithAPIKey sets the admin API key for authentication.
// Admin keys have the prefix "eb_admin_" and are org-scoped.
func WithAPIKey(key string) Option {
	return func(c *Client) {
		c.apiKey = key
	}
}

// WithBaseURL overrides the default API base URL.
func WithBaseURL(url string) Option {
	return func(c *Client) {
		c.baseURL = url
	}
}

// WithHTTPClient sets a custom HTTP client.
// The provided client is used as-is; its Timeout is not modified.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		c.httpClient = client
	}
}

// RequestOption configures a single API request.
type RequestOption func(*requestConfig)

type requestConfig struct {
	idempotencyKey string
	timeout        time.Duration
	headers        http.Header
	requestID      string
}

// WithIdempotencyKey sets an idempotency key for the request.
// This is primarily useful for create operations to enable safe retries.
func WithIdempotencyKey(key string) RequestOption {
	return func(cfg *requestConfig) {
		cfg.idempotencyKey = key
	}
}

// WithTimeout sets a timeout for this specific request.
// This is implemented via context deadline.
func WithTimeout(d time.Duration) RequestOption {
	return func(cfg *requestConfig) {
		cfg.timeout = d
	}
}

// WithHeader adds a custom header to the request.
func WithHeader(key, value string) RequestOption {
	return func(cfg *requestConfig) {
		if cfg.headers == nil {
			cfg.headers = make(http.Header)
		}
		cfg.headers.Set(key, value)
	}
}

// WithRequestID sets a request ID for correlation and tracing.
func WithRequestID(id string) RequestOption {
	return func(cfg *requestConfig) {
		cfg.requestID = id
	}
}

// request represents an API request to be executed.
type request struct {
	method  string
	path    string
	query   url.Values
	body    any
	options []RequestOption
}

// do executes an API request and decodes the response into result.
// If result is nil, the response body is discarded.
func (c *Client) do(ctx context.Context, req request, result any) error {
	// Apply request options
	cfg := &requestConfig{}
	for _, opt := range req.options {
		opt(cfg)
	}

	// Apply timeout if specified
	if cfg.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.timeout)
		defer cancel()
	}

	// Build URL
	u := c.baseURL + req.path
	if len(req.query) > 0 {
		u += "?" + req.query.Encode()
	}

	// Build request body
	var bodyReader io.Reader
	if req.body != nil {
		data, err := json.Marshal(req.body)
		if err != nil {
			return fmt.Errorf("tripswitch: failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, req.method, u, bodyReader)
	if err != nil {
		return fmt.Errorf("tripswitch: failed to create request: %w", err)
	}

	// Set headers
	if req.body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Accept", "application/json")

	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	if cfg.idempotencyKey != "" {
		httpReq.Header.Set("Idempotency-Key", cfg.idempotencyKey)
	}

	if cfg.requestID != "" {
		httpReq.Header.Set("X-Request-ID", cfg.requestID)
	}

	// Apply custom headers
	for key, values := range cfg.headers {
		for _, value := range values {
			httpReq.Header.Add(key, value)
		}
	}

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTransport, err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%w: failed to read response body: %v", ErrTransport, err)
	}

	// Check for errors
	if resp.StatusCode >= 400 {
		return c.parseError(resp, body)
	}

	// Decode successful response
	if result != nil && len(body) > 0 {
		if err := json.Unmarshal(body, result); err != nil {
			return fmt.Errorf("tripswitch: failed to decode response: %w", err)
		}
	}

	return nil
}

// errorResponse represents the JSON error response from the API.
type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// parseError creates an APIError from an HTTP response.
func (c *Client) parseError(resp *http.Response, body []byte) *APIError {
	apiErr := &APIError{
		Status:    resp.StatusCode,
		RequestID: resp.Header.Get("X-Request-ID"),
		Body:      body,
	}

	// Parse Retry-After header for rate limiting
	if resp.StatusCode == http.StatusTooManyRequests {
		raw := resp.Header.Get("Retry-After")
		apiErr.RetryAfterRaw = raw
		apiErr.RetryAfter = parseRetryAfter(raw)
	}

	// Try to parse JSON error response
	var errResp errorResponse
	if err := json.Unmarshal(body, &errResp); err == nil {
		apiErr.Code = errResp.Code
		apiErr.Message = errResp.Message
	}

	// Fallback message based on status code
	if apiErr.Message == "" {
		apiErr.Message = http.StatusText(resp.StatusCode)
	}

	return apiErr
}
