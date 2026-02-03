package tripswitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ErrNotModified is returned when the server responds with 304 Not Modified.
var ErrNotModified = errors.New("tripswitch: not modified")

// ErrUnauthorized is returned when the server responds with 401 or 403.
var ErrUnauthorized = errors.New("tripswitch: unauthorized")

// BreakerMeta contains a breaker's identity and metadata.
type BreakerMeta struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// RouterMeta contains a router's identity and metadata.
type RouterMeta struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type breakersMetadataResponse struct {
	Breakers []BreakerMeta `json:"breakers"`
}

type routersMetadataResponse struct {
	Routers []RouterMeta `json:"routers"`
}

// ListBreakersMetadata retrieves metadata for all breakers in the project.
// If etag is non-empty it is sent as If-None-Match; a 304 response returns
// (nil, etag, ErrNotModified).
func (c *Client) ListBreakersMetadata(ctx context.Context, etag string) ([]BreakerMeta, string, error) {
	url := c.baseURL + "/v1/projects/" + c.projectID + "/breakers/metadata"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("tripswitch: failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("tripswitch: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, etag, ErrNotModified
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, "", ErrUnauthorized
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("tripswitch: unexpected status code: %d", resp.StatusCode)
	}

	var result breakersMetadataResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, "", fmt.Errorf("tripswitch: failed to decode response: %w", err)
	}

	return result.Breakers, resp.Header.Get("ETag"), nil
}

// ListRoutersMetadata retrieves metadata for all routers in the project.
// If etag is non-empty it is sent as If-None-Match; a 304 response returns
// (nil, etag, ErrNotModified).
func (c *Client) ListRoutersMetadata(ctx context.Context, etag string) ([]RouterMeta, string, error) {
	url := c.baseURL + "/v1/projects/" + c.projectID + "/routers/metadata"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("tripswitch: failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("tripswitch: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, etag, ErrNotModified
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, "", ErrUnauthorized
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("tripswitch: unexpected status code: %d", resp.StatusCode)
	}

	var result routersMetadataResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, "", fmt.Errorf("tripswitch: failed to decode response: %w", err)
	}

	return result.Routers, resp.Header.Get("ETag"), nil
}
