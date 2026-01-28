// Example: OpenTelemetry TraceID Integration
//
// This example demonstrates extracting trace IDs from OpenTelemetry context
// to associate Tripswitch samples with distributed traces.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/tripswitch-dev/tripswitch-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

func main() {
	// Create Tripswitch client with OpenTelemetry trace ID extraction
	ts := tripswitch.NewClient("proj_abc123",
		tripswitch.WithAPIKey("eb_pk_..."),
		tripswitch.WithIngestKey("ik_live_..."),
		// Extract trace ID from OpenTelemetry context
		tripswitch.WithTraceIDExtractor(func(ctx context.Context) string {
			span := trace.SpanFromContext(ctx)
			if span.SpanContext().IsValid() {
				return span.SpanContext().TraceID().String()
			}
			return ""
		}),
		// Add global tags for service identification
		tripswitch.WithGlobalTags(map[string]string{
			"service": "checkout-svc",
			"env":     os.Getenv("ENV"),
		}),
	)
	defer ts.Close(context.Background())

	// Wait for initial state sync
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ts.Ready(ctx); err != nil {
		log.Fatal("tripswitch failed to initialize:", err)
	}

	// Start a trace (in production, this would come from incoming request)
	tracer := otel.Tracer("checkout-svc")
	ctx, span := tracer.Start(ctx, "process-checkout")
	defer span.End()

	// Execute with automatic trace ID extraction
	result, err := tripswitch.Execute(ts, ctx, func() (bool, error) {
		return checkInventory(ctx, "SKU-12345")
	},
		tripswitch.WithRouter("inventory-check"),
		tripswitch.WithMetric("latency", tripswitch.Latency),
	)

	if err != nil {
		if tripswitch.IsBreakerError(err) {
			fmt.Println("Circuit open, inventory service unavailable")
			return
		}
		log.Fatal("inventory check failed:", err)
	}

	if result {
		fmt.Println("Item in stock")
	} else {
		fmt.Println("Item out of stock")
	}
}

func checkInventory(ctx context.Context, sku string) (bool, error) {
	// Simulated inventory check
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://inventory.example.com/check?sku="+sku, nil)
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}
