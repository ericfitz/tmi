package otel

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config holds TMI-side OpenTelemetry configuration.
type Config struct {
	Enabled        bool
	SamplingRate   float64
	PrometheusPort int
}

// Setup initializes OpenTelemetry trace and metric providers.
// Returns a shutdown function that must be called on server stop.
// When cfg.Enabled is false, registers no-op providers (zero overhead).
func Setup(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
	logger := slogging.Get()

	if !cfg.Enabled {
		logger.Info("OpenTelemetry disabled")
		return func(context.Context) error { return nil }, nil
	}

	logger.Info("Initializing OpenTelemetry")

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithProcess(),
		resource.WithAttributes(
			semconv.ServiceName("tmi"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTel resource: %w", err)
	}

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	var traceExporter sdktrace.SpanExporter
	traceExporter, err = otlptracegrpc.New(ctx)
	if err != nil {
		logger.Warn("OTLP trace exporter failed, falling back to stdout: %v", err)
		traceExporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("failed to create stdout trace exporter: %w", err)
		}
	}

	// Wrap the exporter so sensitive span attributes (Authorization, tokens,
	// cookies, secrets, client_callback, ...) are redacted before reaching
	// the OTLP collector. Defense in depth for T23 — see #349.
	traceExporter = NewRedactingSpanExporter(traceExporter)

	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SamplingRate))
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)
	otel.SetTracerProvider(tp)

	metricExporter, err := otlpmetricgrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP metric exporter: %w", err)
	}

	readers := []sdkmetric.Option{
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
	}

	if cfg.PrometheusPort > 0 {
		promExporter, err := prometheus.New()
		if err != nil {
			return nil, fmt.Errorf("failed to create Prometheus exporter: %w", err)
		}
		readers = append(readers, sdkmetric.WithReader(promExporter))

		promMux := http.NewServeMux()
		promMux.Handle("/metrics", promhttp.Handler())
		promServer := &http.Server{
			Addr:              fmt.Sprintf(":%d", cfg.PrometheusPort),
			Handler:           promMux,
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			logger.Info("Prometheus metrics endpoint listening on :%d/metrics", cfg.PrometheusPort)
			if err := promServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("Prometheus server error: %v", err)
			}
		}()
	}

	mp := sdkmetric.NewMeterProvider(readers...)
	otel.SetMeterProvider(mp)

	logger.Info("OpenTelemetry initialized (sampling_rate=%.2f)", cfg.SamplingRate)

	shutdownFn := func(ctx context.Context) error {
		logger.Info("Shutting down OpenTelemetry")
		var errs []error
		if err := tp.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("trace provider shutdown: %w", err))
		}
		if err := mp.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("meter provider shutdown: %w", err))
		}
		if len(errs) > 0 {
			return fmt.Errorf("OTel shutdown errors: %v", errs)
		}
		return nil
	}

	return shutdownFn, nil
}
