// Package prometheus implements the native Prometheus metrics backend for Gerbil.
//
// This backend uses the Prometheus Go client directly; it does NOT depend on the
// OpenTelemetry SDK.  A dedicated Prometheus registry is used so that default
// Go/process metrics are not unintentionally included unless the caller opts in.
package prometheus

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Config holds Prometheus-backend configuration.
type Config struct {
	// Path is the HTTP endpoint path (e.g. "/metrics").
	Path string

	// IncludeGoMetrics controls whether the standard Go runtime and process
	// collectors are registered on the dedicated registry.
	// Defaults to true if not explicitly set.
	IncludeGoMetrics *bool
}

// Backend is the native Prometheus metrics backend.
// Metric instruments are created via the New* family of methods and stored
// in the backend-specific instrument types that implement the observability
// instrument interfaces.
type Backend struct {
	cfg                   Config
	registry              *prometheus.Registry
	handler               http.Handler
	droppedSamplesCounter prometheus.Counter
}

// New creates and initialises a Prometheus backend.
//
// cfg.Path sets the HTTP endpoint path (defaults to "/metrics" if empty).
// cfg.IncludeGoMetrics controls whether standard Go runtime and process metrics
// are included; defaults to true when nil.
//
// Returns an error if the registry cannot be created.
func New(cfg Config) (*Backend, error) {
	if cfg.Path == "" {
		cfg.Path = "/metrics"
	}

	registry := prometheus.NewRegistry()
	droppedSamplesCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "gerbil_dropped_metric_samples_total",
		Help: "Total number of metric samples dropped due to invalid labels or unsupported label sets",
	})
	if err := registry.Register(droppedSamplesCounter); err != nil {
		return nil, fmt.Errorf("register dropped samples counter: %w", err)
	}

	// Include Go and process metrics by default.
	includeGo := cfg.IncludeGoMetrics == nil || *cfg.IncludeGoMetrics
	if includeGo {
		if err := registry.Register(collectors.NewGoCollector()); err != nil {
			return nil, fmt.Errorf("register Go collector: %w", err)
		}
		if err := registry.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})); err != nil {
			return nil, fmt.Errorf("register process collector: %w", err)
		}
	}

	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		EnableOpenMetrics: false,
	})

	return &Backend{cfg: cfg, registry: registry, handler: handler, droppedSamplesCounter: droppedSamplesCounter}, nil
}

// HTTPHandler returns the Prometheus /metrics HTTP handler.
func (b *Backend) HTTPHandler() http.Handler {
	return b.handler
}

// Shutdown is a no-op for the Prometheus backend.
// The registry does not maintain background goroutines.
func (b *Backend) Shutdown(_ context.Context) error {
	_ = b
	return nil
}

// NewCounter creates a Prometheus CounterVec registered on the backend's registry.
func (b *Backend) NewCounter(name, desc string, labelNames ...string) (*Counter, error) {
	vec := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: name,
		Help: desc,
	}, labelNames)
	if err := b.registry.Register(vec); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			existing, ok := are.ExistingCollector.(*prometheus.CounterVec)
			if !ok {
				return nil, err
			}
			return &Counter{
				vec:                   existing,
				labelNames:            append([]string(nil), labelNames...),
				droppedSamplesCounter: b.droppedSamplesCounter,
				valueNormalizers:      valueNormalizersForInstrument(name),
			}, nil
		}
		return nil, err
	}
	return &Counter{
		vec:                   vec,
		labelNames:            append([]string(nil), labelNames...),
		droppedSamplesCounter: b.droppedSamplesCounter,
		valueNormalizers:      valueNormalizersForInstrument(name),
	}, nil
}

// NewUpDownCounter creates a Prometheus GaugeVec (Prometheus gauges are
// bidirectional) registered on the backend's registry.
func (b *Backend) NewUpDownCounter(name, desc string, labelNames ...string) (*UpDownCounter, error) {
	vec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: name,
		Help: desc,
	}, labelNames)
	if err := b.registry.Register(vec); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			existing, ok := are.ExistingCollector.(*prometheus.GaugeVec)
			if !ok {
				return nil, err
			}
			return &UpDownCounter{vec: existing, labelNames: append([]string(nil), labelNames...), droppedSamplesCounter: b.droppedSamplesCounter}, nil
		}
		return nil, err
	}
	return &UpDownCounter{vec: vec, labelNames: append([]string(nil), labelNames...), droppedSamplesCounter: b.droppedSamplesCounter}, nil
}

// NewInt64Gauge creates a Prometheus GaugeVec registered on the backend's registry.
func (b *Backend) NewInt64Gauge(name, desc string, labelNames ...string) (*Int64Gauge, error) {
	vec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: name,
		Help: desc,
	}, labelNames)
	if err := b.registry.Register(vec); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			existing, ok := are.ExistingCollector.(*prometheus.GaugeVec)
			if !ok {
				return nil, err
			}
			return &Int64Gauge{vec: existing, labelNames: append([]string(nil), labelNames...), droppedSamplesCounter: b.droppedSamplesCounter}, nil
		}
		return nil, err
	}
	return &Int64Gauge{vec: vec, labelNames: append([]string(nil), labelNames...), droppedSamplesCounter: b.droppedSamplesCounter}, nil
}

// NewFloat64Gauge creates a Prometheus GaugeVec registered on the backend's registry.
func (b *Backend) NewFloat64Gauge(name, desc string, labelNames ...string) (*Float64Gauge, error) {
	vec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: name,
		Help: desc,
	}, labelNames)
	if err := b.registry.Register(vec); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			existing, ok := are.ExistingCollector.(*prometheus.GaugeVec)
			if !ok {
				return nil, err
			}
			return &Float64Gauge{vec: existing, labelNames: append([]string(nil), labelNames...), droppedSamplesCounter: b.droppedSamplesCounter}, nil
		}
		return nil, err
	}
	return &Float64Gauge{vec: vec, labelNames: append([]string(nil), labelNames...), droppedSamplesCounter: b.droppedSamplesCounter}, nil
}

// NewHistogram creates a Prometheus HistogramVec registered on the backend's registry.
func (b *Backend) NewHistogram(name, desc string, buckets []float64, labelNames ...string) (*Histogram, error) {
	vec := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    name,
		Help:    desc,
		Buckets: buckets,
	}, labelNames)
	if err := b.registry.Register(vec); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			existing, ok := are.ExistingCollector.(*prometheus.HistogramVec)
			if !ok {
				return nil, err
			}
			return &Histogram{
				vec:                   existing,
				labelNames:            append([]string(nil), labelNames...),
				droppedSamplesCounter: b.droppedSamplesCounter,
				valueNormalizers:      valueNormalizersForInstrument(name),
			}, nil
		}
		return nil, err
	}
	return &Histogram{
		vec:                   vec,
		labelNames:            append([]string(nil), labelNames...),
		droppedSamplesCounter: b.droppedSamplesCounter,
		valueNormalizers:      valueNormalizersForInstrument(name),
	}, nil
}

// Counter is a native Prometheus counter instrument.
type Counter struct {
	vec                   *prometheus.CounterVec
	labelNames            []string
	droppedSamplesCounter prometheus.Counter
	valueNormalizers      map[string]func(string) string
}

// Add increments the counter by value for the given labels.
//
// value must be non-negative. Negative values are ignored.
func (c *Counter) Add(_ context.Context, value int64, labels map[string]string) {
	if value < 0 {
		log.Printf("WARN: counter add called with negative value=%d labels=%v expected_labels=%v", value, labels, c.labelNames)
		return
	}
	normalized, ok := normalizeLabels(c.labelNames, labels, c.droppedSamplesCounter, c.valueNormalizers)
	if !ok {
		return
	}
	defer guardMetricPanic("counter", c.labelNames, labels)
	c.vec.With(normalized).Add(float64(value))
}

// UpDownCounter is a native Prometheus gauge used as a bidirectional counter.
type UpDownCounter struct {
	vec                   *prometheus.GaugeVec
	labelNames            []string
	droppedSamplesCounter prometheus.Counter
}

// Add adjusts the gauge by value for the given labels.
func (u *UpDownCounter) Add(_ context.Context, value int64, labels map[string]string) {
	normalized, ok := normalizeLabels(u.labelNames, labels, u.droppedSamplesCounter, nil)
	if !ok {
		return
	}
	defer guardMetricPanic("updown", u.labelNames, labels)
	u.vec.With(normalized).Add(float64(value))
}

// Int64Gauge is a native Prometheus gauge recording integer snapshot values.
type Int64Gauge struct {
	vec                   *prometheus.GaugeVec
	labelNames            []string
	droppedSamplesCounter prometheus.Counter
}

// Record sets the gauge to value for the given labels.
func (g *Int64Gauge) Record(_ context.Context, value int64, labels map[string]string) {
	normalized, ok := normalizeLabels(g.labelNames, labels, g.droppedSamplesCounter, nil)
	if !ok {
		return
	}
	defer guardMetricPanic("int64-gauge", g.labelNames, labels)
	g.vec.With(normalized).Set(float64(value))
}

// Float64Gauge is a native Prometheus gauge recording float snapshot values.
type Float64Gauge struct {
	vec                   *prometheus.GaugeVec
	labelNames            []string
	droppedSamplesCounter prometheus.Counter
}

// Record sets the gauge to value for the given labels.
func (g *Float64Gauge) Record(_ context.Context, value float64, labels map[string]string) {
	normalized, ok := normalizeLabels(g.labelNames, labels, g.droppedSamplesCounter, nil)
	if !ok {
		return
	}
	defer guardMetricPanic("float64-gauge", g.labelNames, labels)
	g.vec.With(normalized).Set(value)
}

// Histogram is a native Prometheus histogram instrument.
type Histogram struct {
	vec                   *prometheus.HistogramVec
	labelNames            []string
	droppedSamplesCounter prometheus.Counter
	valueNormalizers      map[string]func(string) string
}

// Record observes value for the given labels.
func (h *Histogram) Record(_ context.Context, value float64, labels map[string]string) {
	normalized, ok := normalizeLabels(h.labelNames, labels, h.droppedSamplesCounter, h.valueNormalizers)
	if !ok {
		return
	}
	defer guardMetricPanic("histogram", h.labelNames, labels)
	h.vec.With(normalized).Observe(value)
}

func normalizeLabels(
	labelNames []string,
	labels map[string]string,
	droppedSamplesCounter prometheus.Counter,
	valueNormalizers map[string]func(string) string,
) (prometheus.Labels, bool) {
	if len(labelNames) == 0 {
		if len(labels) > 0 {
			if droppedSamplesCounter != nil {
				droppedSamplesCounter.Inc()
			}
			log.Printf("WARN: dropping metric sample due to unexpected labels: got=%v expected=none", labels)
			return nil, false
		}
		return nil, true
	}

	normalized := make(prometheus.Labels, len(labelNames))
	for _, name := range labelNames {
		normalized[name] = ""
	}

	for k, v := range labels {
		if _, ok := normalized[k]; !ok {
			if droppedSamplesCounter != nil {
				droppedSamplesCounter.Inc()
			}
			log.Printf("WARN: dropping metric sample due to unexpected label key %q (expected=%v)", k, labelNames)
			return nil, false
		}
		if normalizeValue, ok := valueNormalizers[k]; ok && normalizeValue != nil {
			v = normalizeValue(v)
		}
		normalized[k] = v
	}

	return normalized, true
}

func valueNormalizersForInstrument(name string) map[string]func(string) string {
	switch name {
	case "gerbil_http_requests_total", "gerbil_http_request_duration_seconds":
		return map[string]func(string) string{"method": normalizeHTTPMethod}
	default:
		return nil
	}
}

func normalizeHTTPMethod(method string) string {
	switch method {
	case http.MethodConnect, http.MethodDelete, http.MethodGet, http.MethodHead,
		http.MethodOptions, http.MethodPatch, http.MethodPost, http.MethodPut,
		http.MethodTrace:
		return method
	default:
		return "other"
	}
}

func guardMetricPanic(kind string, expected []string, labels map[string]string) {
	if recovered := recover(); recovered != nil {
		log.Printf("WARN: dropped %s metric sample due to label panic: expected=%v got=%v err=%v", kind, expected, labels, recovered)
	}
}
