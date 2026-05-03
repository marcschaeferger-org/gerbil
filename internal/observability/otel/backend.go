// Package otel implements the OpenTelemetry metrics backend for Gerbil.
//
// Metrics are exported via OTLP (gRPC or HTTP) to an external collector.
// No Prometheus /metrics endpoint is exposed in this mode.
// Future OTel tracing and logging can be added alongside this package
// without touching the Prometheus-native path.
package otel

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

var metricLabelNameRE = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// Config holds OTel backend configuration.
type Config struct {
	// Protocol is "grpc" (default) or "http".
	Protocol string

	// Endpoint is the OTLP collector address.
	Endpoint string

	// Insecure disables TLS.
	Insecure bool

	// ExportInterval is the period between pushes to the collector.
	ExportInterval time.Duration

	// Timeout bounds exporter construction calls.
	Timeout time.Duration

	ServiceName           string
	ServiceVersion        string
	DeploymentEnvironment string
}

// Backend is the OTel metrics backend.
type Backend struct {
	cfg      Config
	provider *sdkmetric.MeterProvider
	meter    metric.Meter
}

// New creates and initialises an OTel backend.
//
// cfg.Protocol must be "grpc" (default) or "http".
// cfg.Endpoint is the OTLP collector address (e.g. "localhost:4317").
// cfg.ExportInterval sets the push period (defaults to 60 s if ≤ 0).
// cfg.Insecure disables TLS on the OTLP connection.
//
// Connection to the collector is established lazily; New only validates cfg
// and creates the SDK components. It returns an error only if the OTel resource
// or exporter cannot be constructed.
func New(cfg Config) (*Backend, error) {
	if cfg.Protocol == "" {
		cfg.Protocol = "grpc"
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, fmt.Errorf("otel backend: empty cfg.Endpoint")
	}
	if cfg.ExportInterval <= 0 {
		cfg.ExportInterval = 60 * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = "gerbil"
	}

	res, err := newResource(cfg.ServiceName, cfg.ServiceVersion, cfg.DeploymentEnvironment)
	if err != nil {
		return nil, fmt.Errorf("otel backend: build resource: %w", err)
	}

	exp, err := newExporter(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("otel backend: create exporter: %w", err)
	}

	reader := sdkmetric.NewPeriodicReader(exp,
		sdkmetric.WithInterval(cfg.ExportInterval),
	)

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(reader),
	)

	meter := provider.Meter("github.com/fosrl/gerbil")

	return &Backend{cfg: cfg, provider: provider, meter: meter}, nil
}

// HTTPHandler returns nil – the OTel backend does not expose an HTTP endpoint.
func (b *Backend) HTTPHandler() http.Handler {
	_ = b
	return nil
}

// Shutdown flushes pending metrics and shuts down the MeterProvider.
func (b *Backend) Shutdown(ctx context.Context) error {
	return b.provider.Shutdown(ctx)
}

// NewCounter creates an OTel Int64Counter.
func (b *Backend) NewCounter(name, desc string, labelNames ...string) (*Counter, error) {
	normalizedLabelNames, err := validateLabelNames(labelNames)
	if err != nil {
		return nil, fmt.Errorf("otel: create counter %q: %w", name, err)
	}
	c, err := b.meter.Int64Counter(name, metric.WithDescription(desc))
	if err != nil {
		return nil, fmt.Errorf("otel: create counter %q: %w", name, err)
	}
	return &Counter{c: c, labelNames: normalizedLabelNames}, nil
}

// NewUpDownCounter creates an OTel Int64UpDownCounter.
func (b *Backend) NewUpDownCounter(name, desc string, labelNames ...string) (*UpDownCounter, error) {
	normalizedLabelNames, err := validateLabelNames(labelNames)
	if err != nil {
		return nil, fmt.Errorf("otel: create up-down counter %q: %w", name, err)
	}
	c, err := b.meter.Int64UpDownCounter(name, metric.WithDescription(desc))
	if err != nil {
		return nil, fmt.Errorf("otel: create up-down counter %q: %w", name, err)
	}
	return &UpDownCounter{c: c, labelNames: normalizedLabelNames}, nil
}

// NewInt64Gauge creates an OTel Int64Gauge.
func (b *Backend) NewInt64Gauge(name, desc string, labelNames ...string) (*Int64Gauge, error) {
	normalizedLabelNames, err := validateLabelNames(labelNames)
	if err != nil {
		return nil, fmt.Errorf("otel: create int64 gauge %q: %w", name, err)
	}
	g, err := b.meter.Int64Gauge(name, metric.WithDescription(desc))
	if err != nil {
		return nil, fmt.Errorf("otel: create int64 gauge %q: %w", name, err)
	}
	return &Int64Gauge{g: g, labelNames: normalizedLabelNames}, nil
}

// NewFloat64Gauge creates an OTel Float64Gauge.
func (b *Backend) NewFloat64Gauge(name, desc string, labelNames ...string) (*Float64Gauge, error) {
	normalizedLabelNames, err := validateLabelNames(labelNames)
	if err != nil {
		return nil, fmt.Errorf("otel: create float64 gauge %q: %w", name, err)
	}
	g, err := b.meter.Float64Gauge(name, metric.WithDescription(desc))
	if err != nil {
		return nil, fmt.Errorf("otel: create float64 gauge %q: %w", name, err)
	}
	return &Float64Gauge{g: g, labelNames: normalizedLabelNames}, nil
}

// NewHistogram creates an OTel Float64Histogram with explicit bucket boundaries.
func (b *Backend) NewHistogram(name, desc string, buckets []float64, labelNames ...string) (*Histogram, error) {
	normalizedLabelNames, err := validateLabelNames(labelNames)
	if err != nil {
		return nil, fmt.Errorf("otel: create histogram %q: %w", name, err)
	}
	h, err := b.meter.Float64Histogram(name,
		metric.WithDescription(desc),
		metric.WithExplicitBucketBoundaries(buckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("otel: create histogram %q: %w", name, err)
	}
	return &Histogram{h: h, labelNames: normalizedLabelNames}, nil
}

func validateLabelNames(labelNames []string) ([]string, error) {
	if len(labelNames) == 0 {
		return nil, nil
	}

	normalized := make([]string, len(labelNames))
	seen := make(map[string]struct{}, len(labelNames))
	for i, name := range labelNames {
		if !metricLabelNameRE.MatchString(name) {
			return nil, fmt.Errorf("invalid label name %q", name)
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("duplicate label name %q", name)
		}
		seen[name] = struct{}{}
		normalized[i] = name
	}

	return normalized, nil
}

func labelsToAttrs(labelNames []string, labels map[string]string) []attribute.KeyValue {
	if len(labelNames) == 0 {
		if len(labels) > 0 {
			log.Printf("WARN: dropping otel metric sample due to unexpected labels: got=%v expected=none", labels)
			return nil
		}
		return []attribute.KeyValue{}
	}

	attrs := make([]attribute.KeyValue, 0, len(labelNames))
	for _, labelName := range labelNames {
		attrs = append(attrs, attribute.String(labelName, labels[labelName]))
	}

	for got := range labels {
		found := false
		for _, expected := range labelNames {
			if got == expected {
				found = true
				break
			}
		}
		if !found {
			log.Printf("WARN: dropping otel metric sample due to unexpected label key %q (expected=%v)", got, labelNames)
			return nil
		}
	}

	return attrs
}

// Counter wraps an OTel Int64Counter.
type Counter struct {
	c          metric.Int64Counter
	labelNames []string
}

// Add increments the counter by value.
func (c *Counter) Add(ctx context.Context, value int64, labels map[string]string) {
	attrs := labelsToAttrs(c.labelNames, labels)
	if attrs == nil {
		return
	}
	c.c.Add(ctx, value, metric.WithAttributes(attrs...))
}

// UpDownCounter wraps an OTel Int64UpDownCounter.
type UpDownCounter struct {
	c          metric.Int64UpDownCounter
	labelNames []string
}

// Add adjusts the up-down counter by value.
func (u *UpDownCounter) Add(ctx context.Context, value int64, labels map[string]string) {
	attrs := labelsToAttrs(u.labelNames, labels)
	if attrs == nil {
		return
	}
	u.c.Add(ctx, value, metric.WithAttributes(attrs...))
}

// Int64Gauge wraps an OTel Int64Gauge.
type Int64Gauge struct {
	g          metric.Int64Gauge
	labelNames []string
}

// Record sets the gauge to value.
func (g *Int64Gauge) Record(ctx context.Context, value int64, labels map[string]string) {
	attrs := labelsToAttrs(g.labelNames, labels)
	if attrs == nil {
		return
	}
	g.g.Record(ctx, value, metric.WithAttributes(attrs...))
}

// Float64Gauge wraps an OTel Float64Gauge.
type Float64Gauge struct {
	g          metric.Float64Gauge
	labelNames []string
}

// Record sets the gauge to value.
func (g *Float64Gauge) Record(ctx context.Context, value float64, labels map[string]string) {
	attrs := labelsToAttrs(g.labelNames, labels)
	if attrs == nil {
		return
	}
	g.g.Record(ctx, value, metric.WithAttributes(attrs...))
}

// Histogram wraps an OTel Float64Histogram.
type Histogram struct {
	h          metric.Float64Histogram
	labelNames []string
}

// Record observes value in the histogram.
func (h *Histogram) Record(ctx context.Context, value float64, labels map[string]string) {
	attrs := labelsToAttrs(h.labelNames, labels)
	if attrs == nil {
		return
	}
	h.h.Record(ctx, value, metric.WithAttributes(attrs...))
}
