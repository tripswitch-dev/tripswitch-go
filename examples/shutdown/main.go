// Example: Graceful Shutdown
//
// This example demonstrates proper graceful shutdown handling with OS signals,
// ensuring all buffered samples are flushed before exit.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tripswitch-dev/tripswitch-go"
)

func main() {
	// Create Tripswitch client
	ts := tripswitch.NewClient("proj_abc123",
		tripswitch.WithAPIKey("sk_live_..."),
		tripswitch.WithIngestSecret("..."), // 64-char hex string
	)

	// Wait for initial state sync
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := ts.Ready(ctx); err != nil {
		cancel()
		log.Fatal("tripswitch failed to initialize:", err)
	}
	cancel()

	// Define the breaker (get these values from your breaker config via API or dashboard)
	breaker := tripswitch.Breaker{
		Name:     "data-fetch",                            // Breaker name (matches SSE events)
		RouterID: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", // UUID from breaker config
		Metric:   "error_rate",                            // Metric name from breaker config
	}

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		result, err := tripswitch.Execute(ts, r.Context(), breaker, func() (string, error) {
			// Simulated work
			return "Hello, World!", nil
		})
		if err != nil {
			if tripswitch.IsBreakerError(err) {
				http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, result)
	})

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	// Start server in goroutine
	go func() {
		fmt.Println("Server starting on :8080")
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatal("server error:", err)
		}
	}()

	// Wait for shutdown signal
	sig := <-sigChan
	fmt.Printf("\nReceived signal %v, shutting down...\n", sig)

	// Shutdown HTTP server first (stop accepting new requests)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	// Close Tripswitch client (flushes buffered samples)
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer closeCancel()
	if err := ts.Close(closeCtx); err != nil {
		log.Printf("Tripswitch close error: %v", err)
	}

	fmt.Println("Shutdown complete")
}
