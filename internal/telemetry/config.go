package telemetry

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Config holds configuration options for OpenTelemetry
type Config struct {
	// Service information
	ServiceName    string
	ServiceVersion string
	Environment    string

	// Tracing configuration
	TracingEnabled    bool
	TracingSampleRate float64
	TracingEndpoint   string
	TracingHeaders    map[string]string

	// Metrics configuration
	MetricsEnabled  bool
	MetricsInterval time.Duration
	MetricsEndpoint string
	MetricsHeaders  map[string]string

	// Logging configuration
	LoggingEnabled        bool
	LoggingEndpoint       string
	LogCorrelationEnabled bool
	LoggingHeaders        map[string]string

	// Resource attributes
	ResourceAttributes map[string]string

	// Development settings
	IsDevelopment   bool
	ConsoleExporter bool
	DebugMode       bool
}

// LoadConfig loads OpenTelemetry configuration from environment variables
func LoadConfig() (*Config, error) {
	config := &Config{
		// Default service information
		ServiceName:    getEnv("OTEL_SERVICE_NAME", "tmi-api"),
		ServiceVersion: getEnv("OTEL_SERVICE_VERSION", "1.0.0"),
		Environment:    getEnv("OTEL_ENVIRONMENT", "development"),

		// Default tracing configuration
		TracingEnabled:    getBoolEnv("OTEL_TRACING_ENABLED", true),
		TracingSampleRate: getFloatEnv("OTEL_TRACING_SAMPLE_RATE", 1.0),
		TracingEndpoint:   getEnv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://localhost:4318"),
		TracingHeaders:    parseHeaders(getEnv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "")),

		// Default metrics configuration
		MetricsEnabled:  getBoolEnv("OTEL_METRICS_ENABLED", true),
		MetricsInterval: getDurationEnv("OTEL_METRICS_INTERVAL", 30*time.Second),
		MetricsEndpoint: getEnv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "http://localhost:4318"),
		MetricsHeaders:  parseHeaders(getEnv("OTEL_EXPORTER_OTLP_METRICS_HEADERS", "")),

		// Default logging configuration
		LoggingEnabled:        getBoolEnv("OTEL_LOGGING_ENABLED", true),
		LoggingEndpoint:       getEnv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "http://localhost:4318"),
		LogCorrelationEnabled: getBoolEnv("OTEL_LOG_CORRELATION_ENABLED", true),
		LoggingHeaders:        parseHeaders(getEnv("OTEL_EXPORTER_OTLP_LOGS_HEADERS", "")),

		// Default resource attributes
		ResourceAttributes: parseResourceAttributes(),

		// Development settings
		IsDevelopment:   strings.ToLower(getEnv("OTEL_ENVIRONMENT", "development")) == "development",
		ConsoleExporter: getBoolEnv("OTEL_CONSOLE_EXPORTER", false),
		DebugMode:       getBoolEnv("OTEL_DEBUG", false),
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid telemetry configuration: %w", err)
	}

	return config, nil
}

// LoadFromRuntimeConfig creates telemetry configuration from the main runtime configuration
func LoadFromRuntimeConfig(runtimeConfig interface{}) (*Config, error) {
	// Use reflection to extract fields from the runtime config
	// This allows us to avoid importing the config package (circular dependency)
	rv := reflect.ValueOf(runtimeConfig)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}

	config := &Config{
		ServiceName:    getStringField(rv, "ServiceName", "tmi-api"),
		ServiceVersion: getStringField(rv, "ServiceVersion", "1.0.0"),
		Environment:    getStringField(rv, "Environment", "development"),

		TracingEnabled:    getBoolField(rv, "TracingEnabled", false),
		TracingSampleRate: getFloat64Field(rv, "TracingSampleRate", 1.0),
		TracingEndpoint:   getStringField(rv, "TracingEndpoint", "http://localhost:4318"),
		TracingHeaders:    getStringMapField(rv, "TracingHeaders"),

		MetricsEnabled:  getBoolField(rv, "MetricsEnabled", false),
		MetricsInterval: getDurationField(rv, "MetricsInterval", 30*time.Second),
		MetricsEndpoint: getStringField(rv, "MetricsEndpoint", "http://localhost:4318"),
		MetricsHeaders:  getStringMapField(rv, "MetricsHeaders"),

		LoggingEnabled:        getBoolField(rv, "LoggingEnabled", false),
		LoggingEndpoint:       getStringField(rv, "LoggingEndpoint", "http://localhost:4318"),
		LogCorrelationEnabled: getBoolField(rv, "LogCorrelationEnabled", true),
		LoggingHeaders:        getStringMapField(rv, "LoggingHeaders"),

		ResourceAttributes: getStringMapField(rv, "ResourceAttributes"),

		IsDevelopment:   getBoolField(rv, "IsDevelopment", true),
		ConsoleExporter: getBoolField(rv, "ConsoleExporter", false),
		DebugMode:       getBoolField(rv, "DebugMode", false),
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid telemetry configuration: %w", err)
	}

	return config, nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.ServiceName == "" {
		return fmt.Errorf("service name cannot be empty")
	}

	if c.ServiceVersion == "" {
		return fmt.Errorf("service version cannot be empty")
	}

	if c.TracingSampleRate < 0.0 || c.TracingSampleRate > 1.0 {
		return fmt.Errorf("tracing sample rate must be between 0.0 and 1.0, got %f", c.TracingSampleRate)
	}

	if c.MetricsInterval <= 0 {
		return fmt.Errorf("metrics interval must be positive, got %v", c.MetricsInterval)
	}

	return nil
}

// IsProduction returns true if this is a production environment
func (c *Config) IsProduction() bool {
	return strings.ToLower(c.Environment) == "production"
}

// IsStaging returns true if this is a staging environment
func (c *Config) IsStaging() bool {
	return strings.ToLower(c.Environment) == "staging"
}

// GetResourceAttributes returns resource attributes for OpenTelemetry
func (c *Config) GetResourceAttributes() map[string]string {
	attrs := make(map[string]string)

	// Add custom resource attributes first
	for key, value := range c.ResourceAttributes {
		attrs[key] = value
	}

	// Add default attributes (these override custom ones if keys conflict)
	attrs["service.name"] = c.ServiceName
	attrs["service.version"] = c.ServiceVersion
	attrs["deployment.environment"] = c.Environment

	return attrs
}

// GetSamplingConfig returns sampling configuration based on environment
func (c *Config) GetSamplingConfig() SamplingConfig {
	if c.IsProduction() {
		return SamplingConfig{
			TraceSampleRate:   0.1, // 10% sampling in production
			MetricsSampleRate: 1.0, // All metrics
			LogSampleRate:     0.5, // 50% of debug logs
		}
	} else if c.IsStaging() {
		return SamplingConfig{
			TraceSampleRate:   0.5, // 50% sampling in staging
			MetricsSampleRate: 1.0, // All metrics
			LogSampleRate:     1.0, // All logs
		}
	} else {
		return SamplingConfig{
			TraceSampleRate:   c.TracingSampleRate, // Use configured rate for development
			MetricsSampleRate: 1.0,                 // All metrics
			LogSampleRate:     1.0,                 // All logs
		}
	}
}

// SamplingConfig holds sampling rates for different telemetry types
type SamplingConfig struct {
	TraceSampleRate   float64
	MetricsSampleRate float64
	LogSampleRate     float64
}

// Helper functions for environment variable parsing

func getEnv(key, defaultValue string) string {
	value, exists := os.LookupEnv(key)
	if exists {
		return strings.TrimSpace(value)
	}
	return defaultValue
}

func getBoolEnv(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func getFloatEnv(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func parseHeaders(headerStr string) map[string]string {
	headers := make(map[string]string)
	if headerStr == "" {
		return headers
	}

	pairs := strings.Split(headerStr, ",")
	for _, pair := range pairs {
		kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(kv) == 2 {
			headers[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	return headers
}

func parseResourceAttributes() map[string]string {
	attributes := make(map[string]string)

	// Parse OTEL_RESOURCE_ATTRIBUTES
	if attrs := os.Getenv("OTEL_RESOURCE_ATTRIBUTES"); attrs != "" {
		pairs := strings.Split(attrs, ",")
		for _, pair := range pairs {
			kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
			if len(kv) == 2 {
				attributes[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
			}
		}
	}

	// Add some default attributes based on environment
	if hostname, err := os.Hostname(); err == nil {
		attributes["host.name"] = hostname
	}

	if pid := os.Getpid(); pid > 0 {
		attributes["process.pid"] = strconv.Itoa(pid)
	}

	return attributes
}

// Reflection helper functions for runtime config extraction

func getStringField(rv reflect.Value, fieldName, defaultValue string) string {
	field := rv.FieldByName(fieldName)
	if !field.IsValid() || field.Kind() != reflect.String {
		return defaultValue
	}
	value := field.String()
	if value == "" {
		return defaultValue
	}
	return value
}

func getBoolField(rv reflect.Value, fieldName string, defaultValue bool) bool {
	field := rv.FieldByName(fieldName)
	if !field.IsValid() || field.Kind() != reflect.Bool {
		return defaultValue
	}
	return field.Bool()
}

func getFloat64Field(rv reflect.Value, fieldName string, defaultValue float64) float64 {
	field := rv.FieldByName(fieldName)
	if !field.IsValid() || field.Kind() != reflect.Float64 {
		return defaultValue
	}
	return field.Float()
}

func getDurationField(rv reflect.Value, fieldName string, defaultValue time.Duration) time.Duration {
	field := rv.FieldByName(fieldName)
	if !field.IsValid() {
		return defaultValue
	}

	switch field.Kind() {
	case reflect.Int64:
		// time.Duration is int64 under the hood
		return time.Duration(field.Int())
	default:
		return defaultValue
	}
}

func getStringMapField(rv reflect.Value, fieldName string) map[string]string {
	field := rv.FieldByName(fieldName)
	if !field.IsValid() || field.Kind() != reflect.Map {
		return make(map[string]string)
	}

	result := make(map[string]string)
	for _, key := range field.MapKeys() {
		if key.Kind() == reflect.String {
			value := field.MapIndex(key)
			if value.IsValid() && value.Kind() == reflect.String {
				result[key.String()] = value.String()
			}
		}
	}
	return result
}
