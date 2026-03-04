package observability

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/iw2rmb/shiva/internal/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/iw2rmb/shiva"

type Telemetry struct {
	metrics       *Metrics
	tracer        trace.Tracer
	traceProvider *sdktrace.TracerProvider
	traceShutdown func(context.Context) error
}

func New(cfg config.Config, logger *slog.Logger) (*Telemetry, error) {
	telemetry := &Telemetry{
		metrics: NewMetrics(),
		tracer:  otel.Tracer(tracerName),
		traceShutdown: func(context.Context) error {
			return nil
		},
	}

	if !cfg.TracingEnabled {
		telemetry.tracer = trace.NewNoopTracerProvider().Tracer(tracerName)
		return telemetry, nil
	}

	if !cfg.TracingStdout {
		telemetry.tracer = otel.Tracer(tracerName)
		return telemetry, nil
	}

	exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, fmt.Errorf("initialize stdout trace exporter: %w", err)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			"",
			attribute.String("service.name", "shiva"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("initialize trace resource: %w", err)
	}

	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(traceProvider)

	telemetry.traceProvider = traceProvider
	telemetry.tracer = traceProvider.Tracer(tracerName)
	telemetry.traceShutdown = traceProvider.Shutdown

	if logger != nil {
		logger.Info("tracing enabled with stdout exporter")
	}

	return telemetry, nil
}

func (t *Telemetry) Metrics() *Metrics {
	if t == nil {
		return nil
	}
	return t.metrics
}

func (t *Telemetry) Tracer() trace.Tracer {
	if t == nil {
		return trace.NewNoopTracerProvider().Tracer(tracerName)
	}
	return t.tracer
}

func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t == nil || t.traceShutdown == nil {
		return nil
	}
	return t.traceShutdown(ctx)
}
