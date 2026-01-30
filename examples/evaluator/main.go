// Example: Custom Error Evaluator
//
// This example demonstrates using a custom error evaluator to ignore 4xx errors
// (client errors) while counting 5xx errors (server errors) as failures.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/tripswitch-dev/tripswitch-go"
)

// HTTPError represents an HTTP response error with status code
type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}

func main() {
	// Create Tripswitch client
	ts := tripswitch.NewClient("proj_abc123",
		tripswitch.WithAPIKey("eb_pk_..."),
		tripswitch.WithIngestSecret("..."), // 64-char hex string
	)
	defer ts.Close(context.Background())

	// Wait for initial state sync
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ts.Ready(ctx); err != nil {
		log.Fatal("tripswitch failed to initialize:", err)
	}

	// Configuration (get these values from your breaker config via API or dashboard)
	const (
		routerID    = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" // UUID from breaker config
		breakerName = "payment-api"                           // Breaker name (matches SSE events)
	)

	// Custom error evaluator: only count 5xx errors as failures
	isServerError := func(err error) bool {
		var httpErr *HTTPError
		if errors.As(err, &httpErr) {
			// 5xx = server error = failure
			// 4xx = client error = not a failure (don't trip breaker)
			return httpErr.StatusCode >= 500
		}
		// Non-HTTP errors are failures
		return true
	}

	// Make API call with custom evaluator
	result, err := tripswitch.Execute(ts, ctx, func() (string, error) {
		return callPaymentAPI(ctx)
	},
		tripswitch.WithBreakers(breakerName),
		tripswitch.WithRouter(routerID),
		tripswitch.WithMetrics(map[string]any{"latency": tripswitch.Latency}),
		tripswitch.WithErrorEvaluator(isServerError),
	)

	if err != nil {
		if tripswitch.IsBreakerError(err) {
			fmt.Println("Circuit open, payment service unavailable")
			return
		}

		var httpErr *HTTPError
		if errors.As(err, &httpErr) {
			if httpErr.StatusCode >= 400 && httpErr.StatusCode < 500 {
				fmt.Printf("Client error (not counted as failure): %v\n", err)
				return
			}
		}
		log.Fatal("payment failed:", err)
	}

	fmt.Printf("Payment result: %s\n", result)
}

func callPaymentAPI(ctx context.Context) (string, error) {
	// Simulated API call
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.example.com/payment", nil)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", &HTTPError{
			StatusCode: resp.StatusCode,
			Message:    resp.Status,
		}
	}

	return "payment_id_123", nil
}
