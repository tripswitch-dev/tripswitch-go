package tripswitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// metadataTestServer creates a test server that handles SSE on any non-target
// path and delegates target paths to the provided handler.
func metadataTestServer(t *testing.T, targetPath string, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != targetPath {
			// Handle SSE subscription
			flusher, ok := w.(http.Flusher)
			if !ok {
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"breaker\": \"test\", \"state\": \"closed\", \"allow_rate\": 1.0}\n\n")
			flusher.Flush()
			<-r.Context().Done()
			return
		}
		handler(w, r)
	}))
}

func TestListBreakersMetadata(t *testing.T) {
	target := "/v1/projects/proj_123/breakers/metadata"
	server := metadataTestServer(t, target, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer eb_pk_test" {
			t.Errorf("unexpected auth header: %s", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"abc123"`)
		json.NewEncoder(w).Encode(breakersMetadataResponse{
			Breakers: []BreakerMeta{
				{ID: "brk_1", Name: "latency", Metadata: map[string]string{"env": "prod"}},
			},
		})
	})
	defer server.Close()

	ts := NewClient("proj_123",
		WithAPIKey("eb_pk_test"),
		WithBaseURL(server.URL),
	)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		ts.Close(ctx)
	}()

	breakers, etag, err := ts.ListBreakersMetadata(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if etag != `"abc123"` {
		t.Errorf("expected etag %q, got %q", `"abc123"`, etag)
	}
	if len(breakers) != 1 {
		t.Fatalf("expected 1 breaker, got %d", len(breakers))
	}
	if breakers[0].ID != "brk_1" {
		t.Errorf("expected ID brk_1, got %q", breakers[0].ID)
	}
	if breakers[0].Metadata["env"] != "prod" {
		t.Errorf("expected env=prod, got %q", breakers[0].Metadata["env"])
	}
}

func TestListBreakersMetadata_IfNoneMatchHeader(t *testing.T) {
	target := "/v1/projects/proj_123/breakers/metadata"
	server := metadataTestServer(t, target, func(w http.ResponseWriter, r *http.Request) {
		if inm := r.Header.Get("If-None-Match"); inm != `"abc123"` {
			t.Errorf("expected If-None-Match %q, got %q", `"abc123"`, inm)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"abc123"`)
		json.NewEncoder(w).Encode(breakersMetadataResponse{
			Breakers: []BreakerMeta{},
		})
	})
	defer server.Close()

	ts := NewClient("proj_123",
		WithAPIKey("eb_pk_test"),
		WithBaseURL(server.URL),
	)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		ts.Close(ctx)
	}()

	_, _, err := ts.ListBreakersMetadata(context.Background(), `"abc123"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListBreakersMetadata_NotModified(t *testing.T) {
	target := "/v1/projects/proj_123/breakers/metadata"
	server := metadataTestServer(t, target, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	})
	defer server.Close()

	ts := NewClient("proj_123",
		WithAPIKey("eb_pk_test"),
		WithBaseURL(server.URL),
	)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		ts.Close(ctx)
	}()

	breakers, etag, err := ts.ListBreakersMetadata(context.Background(), `"abc123"`)
	if !errors.Is(err, ErrNotModified) {
		t.Fatalf("expected ErrNotModified, got %v", err)
	}
	if breakers != nil {
		t.Errorf("expected nil breakers, got %v", breakers)
	}
	if etag != `"abc123"` {
		t.Errorf("expected etag returned back, got %q", etag)
	}
}

func TestListRoutersMetadata(t *testing.T) {
	target := "/v1/projects/proj_123/routers/metadata"
	server := metadataTestServer(t, target, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"rtr456"`)
		json.NewEncoder(w).Encode(routersMetadataResponse{
			Routers: []RouterMeta{
				{ID: "rtr_1", Name: "main-router", Metadata: map[string]string{"region": "us-east"}},
			},
		})
	})
	defer server.Close()

	ts := NewClient("proj_123",
		WithAPIKey("eb_pk_test"),
		WithBaseURL(server.URL),
	)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		ts.Close(ctx)
	}()

	routers, etag, err := ts.ListRoutersMetadata(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if etag != `"rtr456"` {
		t.Errorf("expected etag %q, got %q", `"rtr456"`, etag)
	}
	if len(routers) != 1 {
		t.Fatalf("expected 1 router, got %d", len(routers))
	}
	if routers[0].Metadata["region"] != "us-east" {
		t.Errorf("expected region=us-east, got %q", routers[0].Metadata["region"])
	}
}

func TestListRoutersMetadata_NotModified(t *testing.T) {
	target := "/v1/projects/proj_123/routers/metadata"
	server := metadataTestServer(t, target, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	})
	defer server.Close()

	ts := NewClient("proj_123",
		WithAPIKey("eb_pk_test"),
		WithBaseURL(server.URL),
	)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		ts.Close(ctx)
	}()

	routers, etag, err := ts.ListRoutersMetadata(context.Background(), `"rtr456"`)
	if !errors.Is(err, ErrNotModified) {
		t.Fatalf("expected ErrNotModified, got %v", err)
	}
	if routers != nil {
		t.Errorf("expected nil routers, got %v", routers)
	}
	if etag != `"rtr456"` {
		t.Errorf("expected etag returned back, got %q", etag)
	}
}
