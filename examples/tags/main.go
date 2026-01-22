// Example: Per-Request Tags
//
// This example demonstrates adding diagnostic metadata (tags) to individual
// Execute calls for debugging and filtering in the Tripswitch dashboard.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/tripswitch-dev/tripswitch-go"
)

type User struct {
	ID   string
	Tier string // "free", "pro", "enterprise"
}

func main() {
	// Create Tripswitch client with global tags
	ts := tripswitch.NewClient("proj_abc123",
		tripswitch.WithAPIKey("eb_pk_..."),
		tripswitch.WithIngestSecret("..."), // 64-char hex string
		// Global tags applied to ALL samples
		tripswitch.WithGlobalTags(map[string]string{
			"service": "api-gateway",
			"env":     os.Getenv("ENV"),
			"region":  os.Getenv("AWS_REGION"),
		}),
	)
	defer ts.Close(context.Background())

	// Wait for initial state sync
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ts.Ready(ctx); err != nil {
		log.Fatal("tripswitch failed to initialize:", err)
	}

	// Define breakers (get these values from your breaker config via API or dashboard)
	checkoutBreaker := tripswitch.Breaker{
		Name:     "checkout",                              // Breaker name (matches SSE events)
		RouterID: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", // UUID from breaker config
		Metric:   "error_rate",                            // Metric name from breaker config
	}
	paymentBreaker := tripswitch.Breaker{
		Name:     "payment",                               // Breaker name (matches SSE events)
		RouterID: "yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy", // UUID from breaker config
		Metric:   "error_rate",                            // Metric name from breaker config
	}

	// Simulate handling an HTTP request
	user := &User{ID: "user_123", Tier: "enterprise"}
	endpoint := "/api/v1/checkout"

	// Execute with per-request tags (merged with global tags)
	// Per-request tags override global tags on key conflict
	result, err := tripswitch.Execute(ts, ctx, checkoutBreaker, func() (string, error) {
		return processCheckout(ctx, user)
	}, tripswitch.WithTags(map[string]string{
		"user_tier": user.Tier,
		"user_id":   user.ID,
		"endpoint":  endpoint,
	}))

	if err != nil {
		if tripswitch.IsBreakerError(err) {
			fmt.Println("Circuit open, checkout unavailable")
			return
		}
		log.Fatal("checkout failed:", err)
	}

	fmt.Printf("Checkout complete: %s\n", result)

	// You can also override the trace ID explicitly if needed
	_, _ = tripswitch.Execute(ts, ctx, paymentBreaker, func() (bool, error) {
		return processPayment(ctx)
	},
		tripswitch.WithTraceID("custom-trace-id-123"),
		tripswitch.WithTags(map[string]string{
			"payment_method": "credit_card",
		}),
	)
}

func processCheckout(ctx context.Context, user *User) (string, error) {
	// Simulated checkout processing
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://checkout.example.com/process", nil)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	return "order_12345", nil
}

func processPayment(ctx context.Context) (bool, error) {
	// Simulated payment processing
	return true, nil
}
