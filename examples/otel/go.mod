module github.com/tripswitch-dev/tripswitch-go/examples/otel

go 1.22

require (
	github.com/tripswitch-dev/tripswitch-go v0.0.0
	go.opentelemetry.io/otel v1.24.0
	go.opentelemetry.io/otel/trace v1.24.0
)

require (
	github.com/go-logr/logr v1.4.1 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/r3labs/sse/v2 v2.10.0 // indirect
	go.opentelemetry.io/otel/metric v1.24.0 // indirect
	golang.org/x/net v0.0.0-20191116160921-f9c825593386 // indirect
	gopkg.in/cenkalti/backoff.v1 v1.1.0 // indirect
)

replace github.com/tripswitch-dev/tripswitch-go => ../..
