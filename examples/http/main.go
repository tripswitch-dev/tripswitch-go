// Example: HTTP Client Wrap
//
// This example demonstrates wrapping HTTP client calls with Tripswitch
// circuit breaker protection.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/tripswitch-dev/tripswitch-go"
)

func main() {
	// Create Tripswitch client
	ts := tripswitch.NewClient("proj_abc123",
		tripswitch.WithAPIKey("sk_live_..."),
		tripswitch.WithIngestSecret("..."), // 64-char hex string
	)
	defer ts.Close(context.Background())

	// Wait for initial state sync
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ts.Ready(ctx); err != nil {
		log.Fatal("tripswitch failed to initialize:", err)
	}

	// Define the breaker (get these values from your breaker config via API or dashboard)
	breaker := tripswitch.Breaker{
		Name:     "external-api",                          // Breaker name (matches SSE events)
		RouterID: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", // UUID from breaker config
		Metric:   "error_rate",                            // Metric name from breaker config
	}

	// Create HTTP client
	client := &http.Client{Timeout: 5 * time.Second}

	// Wrap HTTP call with circuit breaker
	resp, err := tripswitch.Execute(ts, ctx, breaker, func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.com/data", nil)
		if err != nil {
			return nil, err
		}
		return client.Do(req)
	})

	if err != nil {
		if tripswitch.IsBreakerError(err) {
			// Circuit is open - return cached/fallback response
			fmt.Println("Circuit open, returning fallback response")
			return
		}
		log.Fatal("request failed:", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("Response: %s\n", body)
}
