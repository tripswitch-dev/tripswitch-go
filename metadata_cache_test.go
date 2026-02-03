package tripswitch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// cacheTestServer creates a test server that handles SSE, breakers metadata,
// and routers metadata paths. The breakers/routers handlers are caller-provided.
func cacheTestServer(t *testing.T, breakersHandler, routersHandler http.HandlerFunc) *httptest.Server {
	t.Helper()
	breakersPath := "/v1/projects/proj_123/breakers/metadata"
	routersPath := "/v1/projects/proj_123/routers/metadata"

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case breakersPath:
			if breakersHandler != nil {
				breakersHandler(w, r)
			}
		case routersPath:
			if routersHandler != nil {
				routersHandler(w, r)
			}
		default:
			// SSE endpoint
			flusher, ok := w.(http.Flusher)
			if !ok {
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"breaker\": \"test\", \"state\": \"closed\", \"allow_rate\": 1.0}\n\n")
			flusher.Flush()
			<-r.Context().Done()
		}
	}))
}

func TestMetadataSync_InitialFetch(t *testing.T) {
	bHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"b1"`)
		json.NewEncoder(w).Encode(breakersMetadataResponse{
			Breakers: []BreakerMeta{{ID: "brk_1", Name: "latency"}},
		})
	}
	rHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"r1"`)
		json.NewEncoder(w).Encode(routersMetadataResponse{
			Routers: []RouterMeta{{ID: "rtr_1", Name: "main"}},
		})
	}

	server := cacheTestServer(t, bHandler, rHandler)
	defer server.Close()

	ts := NewClient("proj_123",
		WithAPIKey("eb_pk_test"),
		WithBaseURL(server.URL),
		WithMetadataSyncInterval(time.Hour), // large interval so only initial fetch runs
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ts.Ready(ctx); err != nil {
		t.Fatalf("ready: %v", err)
	}

	// Give metadata sync goroutine time to complete initial fetch
	time.Sleep(200 * time.Millisecond)

	breakers := ts.GetBreakersMetadata()
	if len(breakers) != 1 || breakers[0].ID != "brk_1" {
		t.Errorf("expected 1 breaker brk_1, got %v", breakers)
	}

	routers := ts.GetRoutersMetadata()
	if len(routers) != 1 || routers[0].ID != "rtr_1" {
		t.Errorf("expected 1 router rtr_1, got %v", routers)
	}

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer closeCancel()
	ts.Close(closeCtx)
}

func TestMetadataSync_Refresh(t *testing.T) {
	var callCount atomic.Int32

	bHandler := func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n == 1 {
			w.Header().Set("ETag", `"b1"`)
			json.NewEncoder(w).Encode(breakersMetadataResponse{
				Breakers: []BreakerMeta{{ID: "brk_1", Name: "v1"}},
			})
		} else {
			// Subsequent calls should have an If-None-Match header
			if inm := r.Header.Get("If-None-Match"); inm == "" {
				t.Errorf("expected If-None-Match header, got empty")
			}
			w.Header().Set("ETag", `"b2"`)
			json.NewEncoder(w).Encode(breakersMetadataResponse{
				Breakers: []BreakerMeta{{ID: "brk_1", Name: "v2"}},
			})
		}
	}
	rHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"r1"`)
		json.NewEncoder(w).Encode(routersMetadataResponse{})
	}

	server := cacheTestServer(t, bHandler, rHandler)
	defer server.Close()

	ts := NewClient("proj_123",
		WithAPIKey("eb_pk_test"),
		WithBaseURL(server.URL),
		WithMetadataSyncInterval(100*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ts.Ready(ctx)

	// Wait for at least one refresh cycle
	time.Sleep(400 * time.Millisecond)

	breakers := ts.GetBreakersMetadata()
	if len(breakers) != 1 || breakers[0].Name != "v2" {
		t.Errorf("expected updated breaker name v2, got %v", breakers)
	}

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer closeCancel()
	ts.Close(closeCtx)
}

func TestMetadataSync_NotModified(t *testing.T) {
	var bCallCount atomic.Int32

	bHandler := func(w http.ResponseWriter, r *http.Request) {
		n := bCallCount.Add(1)
		if n == 1 {
			w.Header().Set("ETag", `"b1"`)
			json.NewEncoder(w).Encode(breakersMetadataResponse{
				Breakers: []BreakerMeta{{ID: "brk_1", Name: "original"}},
			})
		} else {
			w.WriteHeader(http.StatusNotModified)
		}
	}
	rHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"r1"`)
		json.NewEncoder(w).Encode(routersMetadataResponse{})
	}

	server := cacheTestServer(t, bHandler, rHandler)
	defer server.Close()

	ts := NewClient("proj_123",
		WithAPIKey("eb_pk_test"),
		WithBaseURL(server.URL),
		WithMetadataSyncInterval(100*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ts.Ready(ctx)

	// Wait for refresh
	time.Sleep(300 * time.Millisecond)

	breakers := ts.GetBreakersMetadata()
	if len(breakers) != 1 || breakers[0].Name != "original" {
		t.Errorf("expected cache unchanged with name 'original', got %v", breakers)
	}

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer closeCancel()
	ts.Close(closeCtx)
}

func TestMetadataSync_InitFailure(t *testing.T) {
	bHandler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}
	rHandler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}

	server := cacheTestServer(t, bHandler, rHandler)
	defer server.Close()

	ts := NewClient("proj_123",
		WithAPIKey("eb_pk_test"),
		WithBaseURL(server.URL),
		WithMetadataSyncInterval(time.Hour),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ts.Ready(ctx)

	// Give initial fetch time to complete (and fail)
	time.Sleep(200 * time.Millisecond)

	// Client should still work with empty cache
	breakers := ts.GetBreakersMetadata()
	if len(breakers) != 0 {
		t.Errorf("expected empty breakers cache, got %v", breakers)
	}
	routers := ts.GetRoutersMetadata()
	if len(routers) != 0 {
		t.Errorf("expected empty routers cache, got %v", routers)
	}

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer closeCancel()
	ts.Close(closeCtx)
}

func TestMetadataSync_AuthFailure(t *testing.T) {
	var callCount atomic.Int32

	bHandler := func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}
	rHandler := func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}

	server := cacheTestServer(t, bHandler, rHandler)
	defer server.Close()

	ts := NewClient("proj_123",
		WithAPIKey("eb_pk_bad"),
		WithBaseURL(server.URL),
		WithMetadataSyncInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ts.Ready(ctx)

	// Give initial fetch time to complete and fail
	time.Sleep(100 * time.Millisecond)
	countAfterInit := callCount.Load()

	// Wait and verify no more calls (sync should have stopped)
	time.Sleep(200 * time.Millisecond)
	countLater := callCount.Load()

	if countLater != countAfterInit {
		t.Errorf("metadata sync continued after auth failure: %d -> %d", countAfterInit, countLater)
	}

	// Should have only been 1 call (breakers returns 401, sync stops before routers)
	if countAfterInit != 1 {
		t.Errorf("expected 1 call before stopping, got %d", countAfterInit)
	}

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer closeCancel()
	ts.Close(closeCtx)
}

func TestGetBreakersMetadata_Empty(t *testing.T) {
	server := cacheTestServer(t, nil, nil)
	defer server.Close()

	ts := NewClient("proj_123",
		WithBaseURL(server.URL),
		withMetadataSyncDisabled(),
	)

	// No sync running — cache should be empty
	breakers := ts.GetBreakersMetadata()
	if breakers != nil && len(breakers) != 0 {
		t.Errorf("expected nil/empty breakers, got %v", breakers)
	}
	routers := ts.GetRoutersMetadata()
	if routers != nil && len(routers) != 0 {
		t.Errorf("expected nil/empty routers, got %v", routers)
	}

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer closeCancel()
	ts.Close(closeCtx)
}

func TestMetadataSync_Shutdown(t *testing.T) {
	var callCount atomic.Int32

	bHandler := func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("ETag", `"b1"`)
		json.NewEncoder(w).Encode(breakersMetadataResponse{})
	}
	rHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"r1"`)
		json.NewEncoder(w).Encode(routersMetadataResponse{})
	}

	server := cacheTestServer(t, bHandler, rHandler)
	defer server.Close()

	ts := NewClient("proj_123",
		WithAPIKey("eb_pk_test"),
		WithBaseURL(server.URL),
		WithMetadataSyncInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ts.Ready(ctx)

	// Let a few refreshes happen
	time.Sleep(200 * time.Millisecond)

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer closeCancel()
	if err := ts.Close(closeCtx); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Wait a bit for any in-flight requests to complete, then snapshot
	time.Sleep(100 * time.Millisecond)
	countAfterClose := callCount.Load()

	// Verify no more calls happen after shutdown settles
	time.Sleep(300 * time.Millisecond)
	countLater := callCount.Load()

	if countLater != countAfterClose {
		t.Errorf("metadata sync continued after close: %d -> %d", countAfterClose, countLater)
	}
}
