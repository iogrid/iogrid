// Package otel initialises the OpenTelemetry SDK for iogrid coordinator
// services. Traces are exported via OTLP-gRPC; if no collector endpoint is
// configured the SDK is wired up but no spans are exported (no-op exporter
// is omitted intentionally — the SDK already handles missing exporter
// gracefully via the OTEL_EXPORTER_OTLP_ENDPOINT env var).
package otel

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Shutdown is called by main() during graceful shutdown to flush any
// pending spans.
type Shutdown func(context.Context) error

// Setup wires up the OTel SDK. Returns a Shutdown closure the caller must
// invoke before process exit. If OTEL_EXPORTER_OTLP_ENDPOINT is unset, a
// tracer provider with no exporter is installed (spans are created but not
// shipped — useful for local dev where there's no collector).
func Setup(ctx context.Context, serviceName, serviceVersion string) (Shutdown, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(serviceVersion),
			semconv.DeploymentEnvironment(getenv("DEPLOY_ENV", "dev")),
		),
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithContainer(),
		resource.WithHost(),
	)
	if err != nil {
		return nil, fmt.Errorf("otel resource: %w", err)
	}

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		// No collector configured — install a tracer provider with no
		// exporter so otelhttp / otelgrpc still propagate context but
		// don't ship spans anywhere.
		tp := sdktrace.NewTracerProvider(sdktrace.WithResource(res))
		otel.SetTracerProvider(tp)
		return tp.Shutdown, nil
	}

	exp, err := otlptrace.New(ctx, otlptracegrpc.NewClient(
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithTimeout(5*time.Second),
	))
	if err != nil {
		return nil, fmt.Errorf("otel trace exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp,
			sdktrace.WithBatchTimeout(5*time.Second),
		),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
