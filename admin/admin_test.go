package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	client := NewClient(
		WithAPIKey("eb_admin_test"),
		WithBaseURL("https://custom.api.dev"),
	)

	if client.apiKey != "eb_admin_test" {
		t.Errorf("expected apiKey 'eb_admin_test', got %q", client.apiKey)
	}
	if client.baseURL != "https://custom.api.dev" {
		t.Errorf("expected baseURL 'https://custom.api.dev', got %q", client.baseURL)
	}
}

func TestGetProject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/projects/proj_123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer eb_admin_test" {
			t.Errorf("unexpected auth header: %s", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Project{
			ID:   "proj_123",
			Name: "Test Project",
		})
	}))
	defer server.Close()

	client := NewClient(
		WithAPIKey("eb_admin_test"),
		WithBaseURL(server.URL),
	)

	project, err := client.GetProject(context.Background(), "proj_123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if project.ID != "proj_123" {
		t.Errorf("expected ID 'proj_123', got %q", project.ID)
	}
	if project.Name != "Test Project" {
		t.Errorf("expected Name 'Test Project', got %q", project.Name)
	}
}

func TestUpdateProject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}

		var input UpdateProjectInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if input.Name == nil || *input.Name != "Updated Name" {
			t.Errorf("unexpected name in request: %v", input.Name)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Project{
			ID:   "proj_123",
			Name: "Updated Name",
		})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	project, err := client.UpdateProject(context.Background(), "proj_123", UpdateProjectInput{
		Name: Ptr("Updated Name"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if project.Name != "Updated Name" {
		t.Errorf("expected Name 'Updated Name', got %q", project.Name)
	}
}

func TestCreateBreaker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/projects/proj_123/breakers" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var input CreateBreakerInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if input.Name != "api-latency" {
			t.Errorf("unexpected name: %s", input.Name)
		}
		if input.Kind != BreakerKindP95 {
			t.Errorf("unexpected kind: %s", input.Kind)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"breaker": Breaker{
				ID:   "breaker_456",
				Name: input.Name,
				Kind: input.Kind,
			},
			"router_id": "router_789",
		})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	breaker, err := client.CreateBreaker(context.Background(), "proj_123", CreateBreakerInput{
		Name:      "api-latency",
		Metric:    "latency_ms",
		Kind:      BreakerKindP95,
		Op:        BreakerOpGt,
		Threshold: 500,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if breaker.ID != "breaker_456" {
		t.Errorf("expected ID 'breaker_456', got %q", breaker.ID)
	}
}

func TestListBreakers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if limit := r.URL.Query().Get("limit"); limit != "10" {
			t.Errorf("expected limit=10, got %s", limit)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ListBreakersResponse{
			Breakers: []Breaker{
				{ID: "breaker_1", Name: "breaker-one"},
				{ID: "breaker_2", Name: "breaker-two"},
			},
			Count: 2,
			Hash:  "abc123",
		})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	result, err := client.ListBreakers(context.Background(), "proj_123", ListParams{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Breakers) != 2 {
		t.Errorf("expected 2 breakers, got %d", len(result.Breakers))
	}
	if result.Count != 2 {
		t.Errorf("expected count 2, got %d", result.Count)
	}
}

func TestDeleteBreaker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/v1/projects/proj_123/breakers/breaker_456" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	err := client.DeleteBreaker(context.Background(), "proj_123", "breaker_456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAPIError_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", "req_123")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"code":    "not_found",
			"message": "Breaker not found",
		})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	_, err := client.GetBreaker(context.Background(), "proj_123", "nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Status != 404 {
		t.Errorf("expected status 404, got %d", apiErr.Status)
	}
	if apiErr.Code != "not_found" {
		t.Errorf("expected code 'not_found', got %q", apiErr.Code)
	}
	if apiErr.RequestID != "req_123" {
		t.Errorf("expected request ID 'req_123', got %q", apiErr.RequestID)
	}
}

func TestAPIError_RateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Rate limit exceeded",
		})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	_, err := client.GetProject(context.Background(), "proj_123")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.RetryAfter != 30*time.Second {
		t.Errorf("expected RetryAfter 30s, got %v", apiErr.RetryAfter)
	}
	if apiErr.RetryAfterRaw != "30" {
		t.Errorf("expected RetryAfterRaw '30', got %q", apiErr.RetryAfterRaw)
	}
}

func TestAPIError_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Invalid API key",
		})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	_, err := client.GetProject(context.Background(), "proj_123")
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

func TestAPIError_Conflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Breaker already exists",
		})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	_, err := client.CreateBreaker(context.Background(), "proj_123", CreateBreakerInput{
		Name: "duplicate",
	})
	if !errors.Is(err, ErrConflict) {
		t.Errorf("expected ErrConflict, got %v", err)
	}
}

func TestAPIError_Validation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Invalid threshold value",
		})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	_, err := client.CreateBreaker(context.Background(), "proj_123", CreateBreakerInput{
		Name: "invalid",
	})
	if !errors.Is(err, ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

func TestRequestOptions(t *testing.T) {
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Project{ID: "proj_123"})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL), WithAPIKey("eb_admin_test"))

	_, err := client.GetProject(context.Background(), "proj_123",
		WithRequestID("trace_123"),
		WithIdempotencyKey("idem_456"),
		WithHeader("X-Custom", "value"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if id := receivedHeaders.Get("X-Request-ID"); id != "trace_123" {
		t.Errorf("expected X-Request-ID 'trace_123', got %q", id)
	}
	if key := receivedHeaders.Get("Idempotency-Key"); key != "idem_456" {
		t.Errorf("expected Idempotency-Key 'idem_456', got %q", key)
	}
	if custom := receivedHeaders.Get("X-Custom"); custom != "value" {
		t.Errorf("expected X-Custom 'value', got %q", custom)
	}
}

func TestWithTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Project{ID: "proj_123"})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	_, err := client.GetProject(context.Background(), "proj_123",
		WithTimeout(10*time.Millisecond),
	)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestCreateRouter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var input CreateRouterInput
		json.NewDecoder(r.Body).Decode(&input)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(Router{
			ID:   "router_789",
			Name: input.Name,
			Mode: input.Mode,
		})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	router, err := client.CreateRouter(context.Background(), "proj_123", CreateRouterInput{
		Name: "canary-router",
		Mode: RouterModeCanary,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if router.Mode != RouterModeCanary {
		t.Errorf("expected mode 'canary', got %q", router.Mode)
	}
}

func TestLinkBreaker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/projects/proj_123/routers/router_789/breakers" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var input LinkBreakerInput
		json.NewDecoder(r.Body).Decode(&input)
		if input.BreakerID != "breaker_456" {
			t.Errorf("unexpected breaker ID: %s", input.BreakerID)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	err := client.LinkBreaker(context.Background(), "proj_123", "router_789", LinkBreakerInput{
		BreakerID: "breaker_456",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateNotificationChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input CreateNotificationChannelInput
		json.NewDecoder(r.Body).Decode(&input)

		if input.Channel != NotificationChannelSlack {
			t.Errorf("expected channel 'slack', got %q", input.Channel)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(NotificationChannel{
			ID:      "channel_abc",
			Name:    input.Name,
			Channel: input.Channel,
			Events:  input.Events,
		})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	channel, err := client.CreateNotificationChannel(context.Background(), "proj_123", CreateNotificationChannelInput{
		Name:    "alerts",
		Channel: NotificationChannelSlack,
		Config: map[string]any{
			"webhook_url": "https://hooks.slack.com/...",
		},
		Events: []NotificationEventType{NotificationEventTrip, NotificationEventRecover},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(channel.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(channel.Events))
	}
}

func TestListEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if breakerID := r.URL.Query().Get("breaker_id"); breakerID != "breaker_456" {
			t.Errorf("expected breaker_id=breaker_456, got %s", breakerID)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ListEventsResponse{
			Events: []Event{
				{ID: "event_1", FromState: "closed", ToState: "open"},
				{ID: "event_2", FromState: "open", ToState: "half_open"},
			},
			Returned: 2,
		})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	result, err := client.ListEvents(context.Background(), "proj_123", ListEventsParams{
		BreakerID: "breaker_456",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(result.Events))
	}
}

// TestGetStatus removed - GetStatus moved to runtime client (tripswitch.Client)
// See tripswitch_test.go for the new test.

func TestBreakerPager(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		resp := ListBreakersResponse{
			Breakers: []Breaker{{ID: "b1"}, {ID: "b2"}, {ID: "b3"}},
			Count:    3,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	var ids []string
	pager := client.ListBreakersPager(context.Background(), "proj_123", ListParams{Limit: 10})
	for pager.Next() {
		ids = append(ids, pager.Item().ID)
	}
	if err := pager.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ids) != 3 {
		t.Errorf("expected 3 items, got %d", len(ids))
	}
	// API doesn't support pagination, so only 1 call
	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}
}

func TestPtr(t *testing.T) {
	s := Ptr("hello")
	if *s != "hello" {
		t.Errorf("expected 'hello', got %q", *s)
	}

	i := Ptr(42)
	if *i != 42 {
		t.Errorf("expected 42, got %d", *i)
	}

	b := Ptr(true)
	if *b != true {
		t.Errorf("expected true, got %v", *b)
	}

	f := Ptr(3.14)
	if *f != 3.14 {
		t.Errorf("expected 3.14, got %f", *f)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{"30", 30 * time.Second},
		{"120", 120 * time.Second},
		{"0", 0},
		{"", 0},
		{"invalid", 0},
	}

	for _, tt := range tests {
		result := parseRetryAfter(tt.input)
		if result != tt.expected {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestWithHTTPClient(t *testing.T) {
	customClient := &http.Client{Timeout: 5 * time.Second}
	client := NewClient(WithHTTPClient(customClient))

	if client.httpClient != customClient {
		t.Error("expected custom HTTP client to be used")
	}
}

func TestErrorHelpers(t *testing.T) {
	tests := []struct {
		err      error
		checker  func(error) bool
		expected bool
	}{
		{&APIError{Status: 404}, IsNotFound, true},
		{&APIError{Status: 401}, IsUnauthorized, true},
		{&APIError{Status: 403}, IsForbidden, true},
		{&APIError{Status: 429}, IsRateLimited, true},
		{&APIError{Status: 409}, IsConflict, true},
		{&APIError{Status: 400}, IsValidation, true},
		{&APIError{Status: 422}, IsValidation, true},
		{&APIError{Status: 500}, IsNotFound, false},
		{errors.New("other error"), IsNotFound, false},
	}

	for _, tt := range tests {
		result := tt.checker(tt.err)
		if result != tt.expected {
			t.Errorf("checker(%v) = %v, want %v", tt.err, result, tt.expected)
		}
	}
}
