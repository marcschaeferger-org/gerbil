package observability_test

import (
	"context"
	"testing"

	"github.com/fosrl/gerbil/internal/observability"
)

func TestNoopBackendAllInstruments(t *testing.T) {
	n := &observability.NoopBackend{}

	ctx := context.Background()
	labels := observability.Labels{"k": "v"}

	t.Run("Counter", func(t *testing.T) {
		c, _ := n.NewCounter("test_counter", "desc")
		c.Add(ctx, 1, labels)
		c.Add(ctx, 0, nil)
	})

	t.Run("UpDownCounter", func(t *testing.T) {
		u, _ := n.NewUpDownCounter("test_updown", "desc")
		u.Add(ctx, 1, labels)
		u.Add(ctx, -1, nil)
	})

	t.Run("Int64Gauge", func(t *testing.T) {
		g, _ := n.NewInt64Gauge("test_int64gauge", "desc")
		g.Record(ctx, 42, labels)
		g.Record(ctx, 0, nil)
	})

	t.Run("Float64Gauge", func(t *testing.T) {
		g, _ := n.NewFloat64Gauge("test_float64gauge", "desc")
		g.Record(ctx, 3.14, labels)
		g.Record(ctx, 0, nil)
	})

	t.Run("Histogram", func(t *testing.T) {
		h, _ := n.NewHistogram("test_histogram", "desc", []float64{1, 5, 10})
		h.Record(ctx, 2.5, labels)
		h.Record(ctx, 0, nil)
	})

	t.Run("HTTPHandler", func(t *testing.T) {
		if n.HTTPHandler() != nil {
			t.Error("noop HTTPHandler should be nil")
		}
	})

	t.Run("Shutdown", func(t *testing.T) {
		if err := n.Shutdown(ctx); err != nil {
			t.Errorf("noop Shutdown should not error: %v", err)
		}
	})
}

func TestNoopBackendLabelNames(t *testing.T) {
	// Verify that label names passed at creation time are accepted without panic.
	n := &observability.NoopBackend{}

	assertNoPanic := func(t *testing.T, constructor string, fn func()) {
		t.Helper()
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("%s panicked: %v", constructor, r)
			}
		}()
		fn()
	}

	t.Run("NewCounter", func(t *testing.T) {
		assertNoPanic(t, "NewCounter", func() {
			_, _ = n.NewCounter("c", "d", "label1", "label2")
		})
	})

	t.Run("NewUpDownCounter", func(t *testing.T) {
		assertNoPanic(t, "NewUpDownCounter", func() {
			_, _ = n.NewUpDownCounter("u", "d", "l1")
		})
	})

	t.Run("NewInt64Gauge", func(t *testing.T) {
		assertNoPanic(t, "NewInt64Gauge", func() {
			_, _ = n.NewInt64Gauge("g1", "d", "l1", "l2", "l3")
		})
	})

	t.Run("NewFloat64Gauge", func(t *testing.T) {
		assertNoPanic(t, "NewFloat64Gauge", func() {
			_, _ = n.NewFloat64Gauge("g2", "d")
		})
	})

	t.Run("NewHistogram", func(t *testing.T) {
		assertNoPanic(t, "NewHistogram", func() {
			_, _ = n.NewHistogram("h", "d", []float64{0.1, 1.0}, "l1")
		})
	})
}
