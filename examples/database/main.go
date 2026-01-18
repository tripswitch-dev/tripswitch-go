// Example: Database Query with Ignored Errors
//
// This example demonstrates ignoring specific errors (like sql.ErrNoRows)
// so they don't count as failures for circuit breaker purposes.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/tripswitch-dev/tripswitch-go"
)

type User struct {
	ID    int
	Name  string
	Email string
}

// Simulated database
var db *sql.DB

func main() {
	// Create Tripswitch client
	ts := tripswitch.NewClient("proj_abc123",
		tripswitch.WithAPIKey("sk_live_..."),
		tripswitch.WithIngestKey("ik_live_..."),
	)
	defer ts.Close(context.Background())

	// Wait for initial state sync
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ts.Ready(ctx); err != nil {
		log.Fatal("tripswitch failed to initialize:", err)
	}

	// Look up user - sql.ErrNoRows should NOT trip the breaker
	user, err := tripswitch.Execute(ts, ctx, "user-lookup", func() (*User, error) {
		return findUserByID(ctx, 123)
	}, tripswitch.WithIgnoreErrors(sql.ErrNoRows))

	if err != nil {
		if tripswitch.IsBreakerError(err) {
			fmt.Println("Circuit open, database unavailable")
			return
		}
		if err == sql.ErrNoRows {
			fmt.Println("User not found (this doesn't count as a failure)")
			return
		}
		log.Fatal("database error:", err)
	}

	fmt.Printf("Found user: %+v\n", user)
}

func findUserByID(ctx context.Context, id int) (*User, error) {
	// Simulated database query
	// In real code: return db.QueryRowContext(ctx, "SELECT ...").Scan(...)
	return nil, sql.ErrNoRows
}
