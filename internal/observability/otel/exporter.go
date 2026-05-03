package otel

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// newExporter creates the appropriate OTLP exporter based on cfg.Protocol.
func newExporter(ctx context.Context, cfg Config) (sdkmetric.Exporter, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, fmt.Errorf("otel: cfg.Endpoint is empty")
	}

	switch cfg.Protocol {
	case "grpc", "":
		return newGRPCExporter(ctx, cfg)
	case "http":
		return newHTTPExporter(ctx, cfg)
	default:
		return nil, fmt.Errorf("otel: unknown protocol %q (must be \"grpc\" or \"http\")", cfg.Protocol)
	}
}

func newGRPCExporter(ctx context.Context, cfg Config) (sdkmetric.Exporter, error) {
	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(cfg.Endpoint),
	}
	if cfg.Insecure {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	}
	exp, err := otlpmetricgrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("otlp grpc exporter: %w", err)
	}
	return exp, nil
}

func newHTTPExporter(ctx context.Context, cfg Config) (sdkmetric.Exporter, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)

	opts := make([]otlpmetrichttp.Option, 0, 3)
	if strings.Contains(endpoint, "://") {
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return nil, fmt.Errorf("otlp http exporter: parse endpoint URL %q: %w", endpoint, err)
		}
		opts = append(opts, otlpmetrichttp.WithEndpointURL(parsed.String()))
	} else {
		opts = append(opts,
			otlpmetrichttp.WithEndpoint(endpoint),
			otlpmetrichttp.WithURLPath("/v1/metrics"),
		)
	}
	if cfg.Insecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}
	exp, err := otlpmetrichttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("otlp http exporter: %w", err)
	}
	return exp, nil
}
