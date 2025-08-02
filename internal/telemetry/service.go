package telemetry

import (
	"context"
	"fmt"
	"log"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Service manages OpenTelemetry providers and configuration
type Service struct {
	config *Config

	// Providers
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider

	// Global instances
	tracer trace.Tracer
	meter  metric.Meter

	// Resource
	resource *resource.Resource
}

// NewService creates a new telemetry service
func NewService(config *Config) (*Service, error) {
	service := &Service{
		config: config,
	}

	// Create resource
	if err := service.initResource(); err != nil {
		return nil, fmt.Errorf("failed to initialize resource: %w", err)
	}

	// Initialize tracing
	if config.TracingEnabled {
		if err := service.initTracing(); err != nil {
			return nil, fmt.Errorf("failed to initialize tracing: %w", err)
		}
	}

	// Initialize metrics
	if config.MetricsEnabled {
		if err := service.initMetrics(); err != nil {
			return nil, fmt.Errorf("failed to initialize metrics: %w", err)
		}
	}

	// Set global propagator
	service.initPropagation()

	return service, nil
}

// initResource creates the OpenTelemetry resource
func (s *Service) initResource() error {
	attrs := make([]attribute.KeyValue, 0)

	for key, value := range s.config.GetResourceAttributes() {
		attrs = append(attrs, attribute.String(key, value))
	}

	res := resource.NewWithAttributes(
		resource.Default().SchemaURL(),
		attrs...,
	)

	// Merge with default resource
	var err error
	res, err = resource.Merge(resource.Default(), res)
	if err != nil {
		return fmt.Errorf("failed to merge with default resource: %w", err)
	}

	s.resource = res
	return nil
}

// initTracing initializes the tracing provider
func (s *Service) initTracing() error {
	var exporters []sdktrace.SpanExporter

	// Console exporter for development
	if s.config.ConsoleExporter || s.config.IsDevelopment {
		consoleExporter, err := stdouttrace.New(
			stdouttrace.WithPrettyPrint(),
		)
		if err != nil {
			return fmt.Errorf("failed to create console trace exporter: %w", err)
		}
		exporters = append(exporters, consoleExporter)
	}

	// OTLP HTTP exporter
	if s.config.TracingEndpoint != "" {
		// Extract just the host:port from the URL for WithEndpoint
		endpoint := s.config.TracingEndpoint
		if strings.HasPrefix(endpoint, "http://") {
			endpoint = strings.TrimPrefix(endpoint, "http://")
		} else if strings.HasPrefix(endpoint, "https://") {
			endpoint = strings.TrimPrefix(endpoint, "https://")
		}
		
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(endpoint),
			otlptracehttp.WithInsecure(), // TODO: Make configurable
		}

		// Add headers if configured
		if len(s.config.TracingHeaders) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(s.config.TracingHeaders))
		}

		otlpExporter, err := otlptracehttp.New(context.Background(), opts...)
		if err != nil {
			return fmt.Errorf("failed to create OTLP trace exporter: %w", err)
		}
		exporters = append(exporters, otlpExporter)
	}

	if len(exporters) == 0 {
		return fmt.Errorf("no trace exporters configured")
	}

	// Create span processors
	var spanProcessors []sdktrace.SpanProcessor
	for _, exporter := range exporters {
		if s.config.IsDevelopment {
			// Use simple span processor in development for immediate export
			spanProcessors = append(spanProcessors, sdktrace.NewSimpleSpanProcessor(exporter))
		} else {
			// Use batch span processor in production for better performance
			spanProcessors = append(spanProcessors, sdktrace.NewBatchSpanProcessor(exporter))
		}
	}

	// Create sampler based on configuration
	var sampler sdktrace.Sampler
	samplingConfig := s.config.GetSamplingConfig()
	if samplingConfig.TraceSampleRate >= 1.0 {
		sampler = sdktrace.AlwaysSample()
	} else if samplingConfig.TraceSampleRate <= 0.0 {
		sampler = sdktrace.NeverSample()
	} else {
		sampler = sdktrace.TraceIDRatioBased(samplingConfig.TraceSampleRate)
	}

	// Create tracer provider
	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(s.resource),
		sdktrace.WithSampler(sampler),
	}

	for _, processor := range spanProcessors {
		opts = append(opts, sdktrace.WithSpanProcessor(processor))
	}

	s.tracerProvider = sdktrace.NewTracerProvider(opts...)

	// Set global tracer provider
	otel.SetTracerProvider(s.tracerProvider)

	// Create tracer instance
	s.tracer = s.tracerProvider.Tracer(
		s.config.ServiceName,
		trace.WithInstrumentationVersion(s.config.ServiceVersion),
		trace.WithSchemaURL("https://opentelemetry.io/schemas/1.24.0"),
	)

	if s.config.DebugMode {
		log.Printf("Tracing initialized with %d exporters, sample rate: %.2f",
			len(exporters), samplingConfig.TraceSampleRate)
	}

	return nil
}

// initMetrics initializes the metrics provider
func (s *Service) initMetrics() error {
	var readers []sdkmetric.Reader

	// Prometheus exporter for pull-based metrics
	prometheusExporter, err := prometheus.New()
	if err != nil {
		return fmt.Errorf("failed to create Prometheus exporter: %w", err)
	}
	readers = append(readers, prometheusExporter)

	// OTLP HTTP exporter for push-based metrics
	if s.config.MetricsEndpoint != "" {
		// Extract just the host:port from the URL for WithEndpoint
		endpoint := s.config.MetricsEndpoint
		if strings.HasPrefix(endpoint, "http://") {
			endpoint = strings.TrimPrefix(endpoint, "http://")
		} else if strings.HasPrefix(endpoint, "https://") {
			endpoint = strings.TrimPrefix(endpoint, "https://")
		}
		
		opts := []otlpmetrichttp.Option{
			otlpmetrichttp.WithEndpoint(endpoint),
			otlpmetrichttp.WithInsecure(), // TODO: Make configurable
		}

		// Add headers if configured
		if len(s.config.MetricsHeaders) > 0 {
			opts = append(opts, otlpmetrichttp.WithHeaders(s.config.MetricsHeaders))
		}

		otlpExporter, err := otlpmetrichttp.New(context.Background(), opts...)
		if err != nil {
			return fmt.Errorf("failed to create OTLP metrics exporter: %w", err)
		}

		// Create periodic reader for OTLP export
		periodicReader := sdkmetric.NewPeriodicReader(
			otlpExporter,
			sdkmetric.WithInterval(s.config.MetricsInterval),
		)
		readers = append(readers, periodicReader)
	}

	if len(readers) == 0 {
		return fmt.Errorf("no metrics readers configured")
	}

	// Create meter provider
	opts := []sdkmetric.Option{
		sdkmetric.WithResource(s.resource),
	}

	for _, reader := range readers {
		opts = append(opts, sdkmetric.WithReader(reader))
	}

	s.meterProvider = sdkmetric.NewMeterProvider(opts...)

	// Set global meter provider
	otel.SetMeterProvider(s.meterProvider)

	// Create meter instance
	s.meter = s.meterProvider.Meter(
		s.config.ServiceName,
		metric.WithInstrumentationVersion(s.config.ServiceVersion),
		metric.WithSchemaURL("https://opentelemetry.io/schemas/1.24.0"),
	)

	if s.config.DebugMode {
		log.Printf("Metrics initialized with %d readers, interval: %v",
			len(readers), s.config.MetricsInterval)
	}

	return nil
}

// initPropagation sets up context propagation
func (s *Service) initPropagation() {
	// Set up W3C Trace Context and Baggage propagation
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if s.config.DebugMode {
		log.Printf("Context propagation initialized with W3C Trace Context and Baggage")
	}
}

// GetTracer returns the global tracer instance
func (s *Service) GetTracer() trace.Tracer {
	return s.tracer
}

// GetMeter returns the global meter instance
func (s *Service) GetMeter() metric.Meter {
	return s.meter
}

// GetTracerProvider returns the tracer provider
func (s *Service) GetTracerProvider() *sdktrace.TracerProvider {
	return s.tracerProvider
}

// GetMeterProvider returns the meter provider
func (s *Service) GetMeterProvider() *sdkmetric.MeterProvider {
	return s.meterProvider
}

// Shutdown gracefully shuts down all telemetry providers
func (s *Service) Shutdown(ctx context.Context) error {
	var errors []error

	// Shutdown tracer provider
	if s.tracerProvider != nil {
		if err := s.tracerProvider.Shutdown(ctx); err != nil {
			errors = append(errors, fmt.Errorf("failed to shutdown tracer provider: %w", err))
		}
	}

	// Shutdown meter provider
	if s.meterProvider != nil {
		if err := s.meterProvider.Shutdown(ctx); err != nil {
			errors = append(errors, fmt.Errorf("failed to shutdown meter provider: %w", err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("shutdown errors: %v", errors)
	}

	if s.config.DebugMode {
		log.Printf("Telemetry service shutdown completed")
	}

	return nil
}

// ForceFlush forces all pending telemetry data to be exported
func (s *Service) ForceFlush(ctx context.Context) error {
	var errors []error

	// Force flush tracer provider
	if s.tracerProvider != nil {
		if err := s.tracerProvider.ForceFlush(ctx); err != nil {
			errors = append(errors, fmt.Errorf("failed to flush tracer provider: %w", err))
		}
	}

	// Force flush meter provider
	if s.meterProvider != nil {
		if err := s.meterProvider.ForceFlush(ctx); err != nil {
			errors = append(errors, fmt.Errorf("failed to flush meter provider: %w", err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("flush errors: %v", errors)
	}

	return nil
}

// Health checks the health of the telemetry service
func (s *Service) Health() HealthStatus {
	status := HealthStatus{
		Healthy: true,
		Details: make(map[string]interface{}),
	}

	// Check tracer provider
	if s.config.TracingEnabled {
		status.Details["tracing_enabled"] = s.tracerProvider != nil
	}

	// Check meter provider
	if s.config.MetricsEnabled {
		status.Details["metrics_enabled"] = s.meterProvider != nil
	}

	status.Details["service_name"] = s.config.ServiceName
	status.Details["service_version"] = s.config.ServiceVersion
	status.Details["environment"] = s.config.Environment

	return status
}

// HealthStatus represents the health status of the telemetry service
type HealthStatus struct {
	Healthy bool                   `json:"healthy"`
	Details map[string]interface{} `json:"details"`
}

// Global service instance
var globalService *Service

// Initialize initializes the global telemetry service
func Initialize(config *Config) error {
	service, err := NewService(config)
	if err != nil {
		return fmt.Errorf("failed to create telemetry service: %w", err)
	}

	globalService = service
	return nil
}

// GetService returns the global telemetry service
func GetService() *Service {
	return globalService
}

// GetTracer returns the global tracer
func GetTracer() trace.Tracer {
	if globalService != nil {
		return globalService.GetTracer()
	}
	return otel.Tracer("noop")
}

// GetMeter returns the global meter
func GetMeter() metric.Meter {
	if globalService != nil {
		return globalService.GetMeter()
	}
	return otel.Meter("noop")
}

// Shutdown shuts down the global telemetry service
func Shutdown(ctx context.Context) error {
	if globalService != nil {
		return globalService.Shutdown(ctx)
	}
	return nil
}
