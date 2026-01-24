package admin

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Sentinel errors for common API error cases.
// Use errors.Is to check for these conditions.
// These map to the canonical error taxonomy defined in the SDK Contract v0.2.
var (
	ErrNotFound     = errors.New("tripswitch: not found")
	ErrUnauthorized = errors.New("tripswitch: unauthorized")
	ErrForbidden    = errors.New("tripswitch: forbidden")
	ErrRateLimited  = errors.New("tripswitch: rate limited")
	ErrConflict     = errors.New("tripswitch: conflict")
	ErrValidation   = errors.New("tripswitch: validation error")
	ErrTransport    = errors.New("tripswitch: transport failure")
	ErrServerFault  = errors.New("tripswitch: server fault")
)

// APIError represents an error response from the Tripswitch API.
// It implements the error interface and supports errors.Is for sentinel matching.
type APIError struct {
	Status        int           // HTTP status code
	Code          string        // Tripswitch error code (if provided)
	Message       string        // Human-readable error message
	RequestID     string        // Request ID for debugging
	Body          []byte        // Raw response body
	RetryAfter    time.Duration // Parsed Retry-After duration (zero if absent)
	RetryAfterRaw string        // Raw Retry-After header value
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("tripswitch: %s (status %d, code %s)", e.Message, e.Status, e.Code)
	}
	return fmt.Sprintf("tripswitch: %s (status %d)", e.Message, e.Status)
}

// Is implements errors.Is matching for sentinel errors based on HTTP status.
func (e *APIError) Is(target error) bool {
	switch e.Status {
	case http.StatusNotFound:
		return target == ErrNotFound
	case http.StatusUnauthorized:
		return target == ErrUnauthorized
	case http.StatusForbidden:
		return target == ErrForbidden
	case http.StatusTooManyRequests:
		return target == ErrRateLimited
	case http.StatusConflict:
		return target == ErrConflict
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return target == ErrValidation
	}
	// 5xx status codes are server faults
	if e.Status >= 500 && e.Status < 600 {
		return target == ErrServerFault
	}
	return false
}

// RetryAfterDuration returns the parsed Retry-After duration and whether it was present.
func (e *APIError) RetryAfterDuration() (time.Duration, bool) {
	return e.RetryAfter, e.RetryAfter > 0
}

// parseRetryAfter parses the Retry-After header value.
// It handles both delta-seconds and HTTP-date formats.
func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}

	// Try parsing as seconds first (most common)
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second
	}

	// Try parsing as HTTP-date
	if t, err := http.ParseTime(value); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}

	return 0
}

// IsNotFound returns true if the error represents a 404 Not Found response.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// IsUnauthorized returns true if the error represents a 401 Unauthorized response.
func IsUnauthorized(err error) bool {
	return errors.Is(err, ErrUnauthorized)
}

// IsForbidden returns true if the error represents a 403 Forbidden response.
func IsForbidden(err error) bool {
	return errors.Is(err, ErrForbidden)
}

// IsRateLimited returns true if the error represents a 429 Too Many Requests response.
func IsRateLimited(err error) bool {
	return errors.Is(err, ErrRateLimited)
}

// IsConflict returns true if the error represents a 409 Conflict response.
func IsConflict(err error) bool {
	return errors.Is(err, ErrConflict)
}

// IsValidation returns true if the error represents a validation error (400 or 422).
func IsValidation(err error) bool {
	return errors.Is(err, ErrValidation)
}

// IsTransport returns true if the error represents a transport failure (connection error, timeout, etc.).
func IsTransport(err error) bool {
	return errors.Is(err, ErrTransport)
}

// IsServerFault returns true if the error represents a server fault (5xx response).
func IsServerFault(err error) bool {
	return errors.Is(err, ErrServerFault)
}
