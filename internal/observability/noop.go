package observability

import (
	"context"
	"net/http"
)

// NoopBackend is a Backend that discards all observations.
// It is used when metrics are disabled (Enabled=false or Backend="none").
// All methods are safe to call concurrently.
type NoopBackend struct{}

// Compile-time interface check.
var _ Backend = (*NoopBackend)(nil)

func (n *NoopBackend) NewCounter(_ string, _ string, _ ...string) (Counter, error) {
	return noopCounter{}, nil
}

func (n *NoopBackend) NewUpDownCounter(_ string, _ string, _ ...string) (UpDownCounter, error) {
	return noopUpDownCounter{}, nil
}

func (n *NoopBackend) NewInt64Gauge(_ string, _ string, _ ...string) (Int64Gauge, error) {
	return noopInt64Gauge{}, nil
}

func (n *NoopBackend) NewFloat64Gauge(_ string, _ string, _ ...string) (Float64Gauge, error) {
	return noopFloat64Gauge{}, nil
}

func (n *NoopBackend) NewHistogram(_ string, _ string, _ []float64, _ ...string) (Histogram, error) {
	return noopHistogram{}, nil
}

func (n *NoopBackend) HTTPHandler() http.Handler {
	return nil
}

func (n *NoopBackend) Shutdown(_ context.Context) error {
	return nil
}

// --- noop instrument types ---

type noopCounter struct{}

func (noopCounter) Add(_ context.Context, _ int64, _ Labels) { /* intentionally no-op */ }

type noopUpDownCounter struct{}

func (noopUpDownCounter) Add(_ context.Context, _ int64, _ Labels) { /* intentionally no-op */ }

type noopInt64Gauge struct{}

func (noopInt64Gauge) Record(_ context.Context, _ int64, _ Labels) { /* intentionally no-op */ }

type noopFloat64Gauge struct{}

func (noopFloat64Gauge) Record(_ context.Context, _ float64, _ Labels) { /* intentionally no-op */ }

type noopHistogram struct{}

func (noopHistogram) Record(_ context.Context, _ float64, _ Labels) { /* intentionally no-op */ }
