package telemetry

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"
)

// ProductionConfig provides production-ready OpenTelemetry configuration
type ProductionConfig struct {
	// Service identification
	ServiceName    string
	ServiceVersion string
	Environment    string
	DeploymentID   string
	InstanceID     string

	// Resource attributes
	ResourceAttributes map[string]string

	// Tracing configuration
	TracingEnabled      bool
	TracingSampleRate   float64
	TracingBatchTimeout time.Duration
	TracingBatchSize    int
	TracingQueueSize    int

	// Metrics configuration
	MetricsEnabled      bool
	MetricsInterval     time.Duration
	MetricsBatchTimeout time.Duration
	MetricsBatchSize    int
	MetricsQueueSize    int

	// Logging configuration
	LoggingEnabled        bool
	LogLevel              string
	LogFormat             string // "json" or "text"
	LogFile               string
	LogRotationMaxSize    int
	LogRotationMaxBackups int
	LogRotationMaxAge     int
	LogRotationCompress   bool

	// Exporter configuration
	ExporterType         string // "otlp", "jaeger", "prometheus", "console"
	OTLPEndpoint         string
	OTLPHeaders          map[string]string
	OTLPTimeout          time.Duration
	OTLPRetryEnabled     bool
	OTLPRetryMaxAttempts int
	OTLPRetryBackoff     time.Duration

	// Jaeger configuration
	JaegerEndpoint string
	JaegerUser     string
	JaegerPassword string

	// Prometheus configuration
	PrometheusEndpoint  string
	PrometheusNamespace string
	PrometheusJobName   string

	// Security configuration
	TLSEnabled            bool
	TLSCertFile           string
	TLSKeyFile            string
	TLSCAFile             string
	TLSInsecureSkipVerify bool

	// Performance configuration
	MaxCPUs            int
	MaxMemoryMB        int
	PerformanceProfile string // "low", "medium", "high", "custom"
	SamplingStrategy   string // "probabilistic", "rate_limiting", "adaptive"

	// Feature flags
	EnableHTTPInstrumentation     bool
	EnableDatabaseInstrumentation bool
	EnableRedisInstrumentation    bool
	EnableGRPCInstrumentation     bool
	EnableRuntimeMetrics          bool
	EnableCustomMetrics           bool
	EnableSecurityFiltering       bool
	EnableLogSampling             bool

	// Development/Debug settings
	DevelopmentMode bool
	DebugLogging    bool
	ConsoleExporter bool
	VerboseLogging  bool
}

// ProductionEnvironment represents different production environments
type ProductionEnvironment string

const (
	EnvDevelopment ProductionEnvironment = "development"
	EnvStaging     ProductionEnvironment = "staging"
	EnvProduction  ProductionEnvironment = "production"
	EnvTesting     ProductionEnvironment = "testing"
)

// PerformanceProfile represents different performance optimization profiles
type PerformanceProfile string

const (
	ProfileLow    PerformanceProfile = "low"
	ProfileMedium PerformanceProfile = "medium"
	ProfileHigh   PerformanceProfile = "high"
	ProfileCustom PerformanceProfile = "custom"
)

// NewProductionConfig creates a production-ready configuration from environment variables
func NewProductionConfig() (*ProductionConfig, error) {
	config := &ProductionConfig{
		// Default service identification
		ServiceName:    getEnvString("OTEL_SERVICE_NAME", "tmi-api"),
		ServiceVersion: getEnvString("OTEL_SERVICE_VERSION", "1.0.0"),
		Environment:    getEnvString("OTEL_ENVIRONMENT", "production"),
		DeploymentID:   getEnvString("DEPLOYMENT_ID", ""),
		InstanceID:     getEnvString("INSTANCE_ID", generateInstanceID()),

		// Resource attributes from environment
		ResourceAttributes: parseProductionResourceAttributes(),

		// Tracing defaults for production
		TracingEnabled:      getEnvBool("OTEL_TRACING_ENABLED", true),
		TracingSampleRate:   getEnvFloat("OTEL_TRACING_SAMPLE_RATE", 0.1), // 10% sampling in production
		TracingBatchTimeout: getEnvDuration("OTEL_TRACING_BATCH_TIMEOUT", 5*time.Second),
		TracingBatchSize:    getEnvInt("OTEL_TRACING_BATCH_SIZE", 512),
		TracingQueueSize:    getEnvInt("OTEL_TRACING_QUEUE_SIZE", 2048),

		// Metrics defaults for production
		MetricsEnabled:      getEnvBool("OTEL_METRICS_ENABLED", true),
		MetricsInterval:     getEnvDuration("OTEL_METRICS_INTERVAL", 30*time.Second),
		MetricsBatchTimeout: getEnvDuration("OTEL_METRICS_BATCH_TIMEOUT", 10*time.Second),
		MetricsBatchSize:    getEnvInt("OTEL_METRICS_BATCH_SIZE", 1024),
		MetricsQueueSize:    getEnvInt("OTEL_METRICS_QUEUE_SIZE", 4096),

		// Logging defaults for production
		LoggingEnabled:        getEnvBool("OTEL_LOGGING_ENABLED", true),
		LogLevel:              getEnvString("LOG_LEVEL", "info"),
		LogFormat:             getEnvString("LOG_FORMAT", "json"),
		LogFile:               getEnvString("LOG_FILE", "/var/log/tmi/app.log"),
		LogRotationMaxSize:    getEnvInt("LOG_ROTATION_MAX_SIZE", 100),
		LogRotationMaxBackups: getEnvInt("LOG_ROTATION_MAX_BACKUPS", 10),
		LogRotationMaxAge:     getEnvInt("LOG_ROTATION_MAX_AGE", 30),
		LogRotationCompress:   getEnvBool("LOG_ROTATION_COMPRESS", true),

		// Exporter configuration
		ExporterType:         getEnvString("OTEL_EXPORTER_TYPE", "otlp"),
		OTLPEndpoint:         getEnvString("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		OTLPHeaders:          parseOTLPHeaders(),
		OTLPTimeout:          getEnvDuration("OTEL_EXPORTER_OTLP_TIMEOUT", 30*time.Second),
		OTLPRetryEnabled:     getEnvBool("OTEL_EXPORTER_OTLP_RETRY_ENABLED", true),
		OTLPRetryMaxAttempts: getEnvInt("OTEL_EXPORTER_OTLP_RETRY_MAX_ATTEMPTS", 3),
		OTLPRetryBackoff:     getEnvDuration("OTEL_EXPORTER_OTLP_RETRY_BACKOFF", 1*time.Second),

		// Jaeger configuration
		JaegerEndpoint: getEnvString("JAEGER_ENDPOINT", ""),
		JaegerUser:     getEnvString("JAEGER_USER", ""),
		JaegerPassword: getEnvString("JAEGER_PASSWORD", ""),

		// Prometheus configuration
		PrometheusEndpoint:  getEnvString("PROMETHEUS_ENDPOINT", ""),
		PrometheusNamespace: getEnvString("PROMETHEUS_NAMESPACE", "tmi"),
		PrometheusJobName:   getEnvString("PROMETHEUS_JOB_NAME", "tmi-api"),

		// Security configuration
		TLSEnabled:            getEnvBool("OTEL_TLS_ENABLED", false),
		TLSCertFile:           getEnvString("OTEL_TLS_CERT_FILE", ""),
		TLSKeyFile:            getEnvString("OTEL_TLS_KEY_FILE", ""),
		TLSCAFile:             getEnvString("OTEL_TLS_CA_FILE", ""),
		TLSInsecureSkipVerify: getEnvBool("OTEL_TLS_INSECURE_SKIP_VERIFY", false),

		// Performance configuration
		MaxCPUs:            getEnvInt("OTEL_MAX_CPUS", 0), // 0 = use all available
		MaxMemoryMB:        getEnvInt("OTEL_MAX_MEMORY_MB", 512),
		PerformanceProfile: getEnvString("OTEL_PERFORMANCE_PROFILE", "medium"),
		SamplingStrategy:   getEnvString("OTEL_SAMPLING_STRATEGY", "probabilistic"),

		// Feature flags
		EnableHTTPInstrumentation:     getEnvBool("OTEL_ENABLE_HTTP_INSTRUMENTATION", true),
		EnableDatabaseInstrumentation: getEnvBool("OTEL_ENABLE_DATABASE_INSTRUMENTATION", true),
		EnableRedisInstrumentation:    getEnvBool("OTEL_ENABLE_REDIS_INSTRUMENTATION", true),
		EnableGRPCInstrumentation:     getEnvBool("OTEL_ENABLE_GRPC_INSTRUMENTATION", false),
		EnableRuntimeMetrics:          getEnvBool("OTEL_ENABLE_RUNTIME_METRICS", true),
		EnableCustomMetrics:           getEnvBool("OTEL_ENABLE_CUSTOM_METRICS", true),
		EnableSecurityFiltering:       getEnvBool("OTEL_ENABLE_SECURITY_FILTERING", true),
		EnableLogSampling:             getEnvBool("OTEL_ENABLE_LOG_SAMPLING", true),

		// Development/Debug settings
		DevelopmentMode: getEnvBool("OTEL_DEVELOPMENT_MODE", false),
		DebugLogging:    getEnvBool("OTEL_DEBUG_LOGGING", false),
		ConsoleExporter: getEnvBool("OTEL_CONSOLE_EXPORTER", false),
		VerboseLogging:  getEnvBool("OTEL_VERBOSE_LOGGING", false),
	}

	// Apply environment-specific configurations
	if err := config.applyEnvironmentDefaults(); err != nil {
		return nil, fmt.Errorf("failed to apply environment defaults: %w", err)
	}

	// Apply performance profile
	if err := config.applyPerformanceProfile(); err != nil {
		return nil, fmt.Errorf("failed to apply performance profile: %w", err)
	}

	// Validate configuration
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return config, nil
}

// applyEnvironmentDefaults applies environment-specific configuration defaults
func (c *ProductionConfig) applyEnvironmentDefaults() error {
	env := ProductionEnvironment(c.Environment)

	switch env {
	case EnvDevelopment:
		c.TracingSampleRate = 1.0 // Sample everything in development
		c.MetricsInterval = 5 * time.Second
		c.LogLevel = "debug"
		c.DebugLogging = true
		c.ConsoleExporter = true
		c.DevelopmentMode = true
		c.VerboseLogging = true

	case EnvTesting:
		c.TracingSampleRate = 1.0 // Sample everything in testing
		c.MetricsInterval = 1 * time.Second
		c.LogLevel = "debug"
		c.ConsoleExporter = true
		c.EnableLogSampling = false // No sampling in tests

	case EnvStaging:
		c.TracingSampleRate = 0.5 // 50% sampling in staging
		c.MetricsInterval = 15 * time.Second
		c.LogLevel = "info"
		c.DebugLogging = false
		c.ConsoleExporter = false
		c.DevelopmentMode = false

	case EnvProduction:
		c.TracingSampleRate = 0.1 // 10% sampling in production
		c.MetricsInterval = 30 * time.Second
		c.LogLevel = "warn"
		c.DebugLogging = false
		c.ConsoleExporter = false
		c.DevelopmentMode = false
		c.VerboseLogging = false

	default:
		return fmt.Errorf("unknown environment: %s", c.Environment)
	}

	return nil
}

// applyPerformanceProfile applies performance-specific configuration
func (c *ProductionConfig) applyPerformanceProfile() error {
	profile := PerformanceProfile(c.PerformanceProfile)

	switch profile {
	case ProfileLow:
		// Low resource usage, higher latency
		c.TracingBatchSize = 128
		c.TracingQueueSize = 512
		c.TracingBatchTimeout = 10 * time.Second
		c.MetricsBatchSize = 256
		c.MetricsQueueSize = 1024
		c.MetricsBatchTimeout = 30 * time.Second
		c.MaxMemoryMB = 128

	case ProfileMedium:
		// Balanced performance and resource usage
		c.TracingBatchSize = 512
		c.TracingQueueSize = 2048
		c.TracingBatchTimeout = 5 * time.Second
		c.MetricsBatchSize = 1024
		c.MetricsQueueSize = 4096
		c.MetricsBatchTimeout = 10 * time.Second
		c.MaxMemoryMB = 512

	case ProfileHigh:
		// High performance, higher resource usage
		c.TracingBatchSize = 2048
		c.TracingQueueSize = 8192
		c.TracingBatchTimeout = 1 * time.Second
		c.MetricsBatchSize = 4096
		c.MetricsQueueSize = 16384
		c.MetricsBatchTimeout = 5 * time.Second
		c.MaxMemoryMB = 2048

	case ProfileCustom:
		// Custom profile - use values from environment variables
		// No changes, keep existing values

	default:
		return fmt.Errorf("unknown performance profile: %s", c.PerformanceProfile)
	}

	return nil
}

// validate validates the configuration for consistency and correctness
func (c *ProductionConfig) validate() error {
	var errors []string

	// Validate service identification
	if c.ServiceName == "" {
		errors = append(errors, "service name is required")
	}
	if c.ServiceVersion == "" {
		errors = append(errors, "service version is required")
	}
	if c.Environment == "" {
		errors = append(errors, "environment is required")
	}

	// Validate sampling rates
	if c.TracingSampleRate < 0 || c.TracingSampleRate > 1 {
		errors = append(errors, "tracing sample rate must be between 0 and 1")
	}

	// Validate batch sizes and timeouts
	if c.TracingBatchSize <= 0 {
		errors = append(errors, "tracing batch size must be positive")
	}
	if c.TracingQueueSize <= 0 {
		errors = append(errors, "tracing queue size must be positive")
	}
	if c.TracingBatchTimeout <= 0 {
		errors = append(errors, "tracing batch timeout must be positive")
	}

	if c.MetricsBatchSize <= 0 {
		errors = append(errors, "metrics batch size must be positive")
	}
	if c.MetricsQueueSize <= 0 {
		errors = append(errors, "metrics queue size must be positive")
	}
	if c.MetricsBatchTimeout <= 0 {
		errors = append(errors, "metrics batch timeout must be positive")
	}

	// Validate log level
	validLogLevels := []string{"debug", "info", "warn", "error", "fatal"}
	if !contains(validLogLevels, strings.ToLower(c.LogLevel)) {
		errors = append(errors, fmt.Sprintf("invalid log level: %s (must be one of: %v)", c.LogLevel, validLogLevels))
	}

	// Validate log format
	validLogFormats := []string{"json", "text"}
	if !contains(validLogFormats, strings.ToLower(c.LogFormat)) {
		errors = append(errors, fmt.Sprintf("invalid log format: %s (must be one of: %v)", c.LogFormat, validLogFormats))
	}

	// Validate exporter type
	validExporterTypes := []string{"otlp", "jaeger", "prometheus", "console"}
	if !contains(validExporterTypes, strings.ToLower(c.ExporterType)) {
		errors = append(errors, fmt.Sprintf("invalid exporter type: %s (must be one of: %v)", c.ExporterType, validExporterTypes))
	}

	// Validate exporter-specific configuration
	switch strings.ToLower(c.ExporterType) {
	case "otlp":
		if c.OTLPEndpoint == "" {
			errors = append(errors, "OTLP endpoint is required when using OTLP exporter")
		}
	case "jaeger":
		if c.JaegerEndpoint == "" {
			errors = append(errors, "Jaeger endpoint is required when using Jaeger exporter")
		}
	case "prometheus":
		if c.PrometheusEndpoint == "" {
			errors = append(errors, "Prometheus endpoint is required when using Prometheus exporter")
		}
	}

	// Validate TLS configuration
	if c.TLSEnabled {
		if c.TLSCertFile == "" || c.TLSKeyFile == "" {
			errors = append(errors, "TLS cert file and key file are required when TLS is enabled")
		}
	}

	// Validate performance configuration
	if c.MaxMemoryMB <= 0 {
		errors = append(errors, "max memory must be positive")
	}

	if len(errors) > 0 {
		return fmt.Errorf("configuration validation errors: %s", strings.Join(errors, "; "))
	}

	return nil
}

// ToConfig converts ProductionConfig to the standard Config format
func (c *ProductionConfig) ToConfig() *Config {
	return &Config{
		ServiceName:       c.ServiceName,
		ServiceVersion:    c.ServiceVersion,
		Environment:       c.Environment,
		TracingEnabled:    c.TracingEnabled,
		TracingSampleRate: c.TracingSampleRate,
		MetricsEnabled:    c.MetricsEnabled,
		MetricsInterval:   c.MetricsInterval,
		ConsoleExporter:   c.ConsoleExporter,
		IsDevelopment:     c.DevelopmentMode,
	}
}

// GetResourceAttributes returns OpenTelemetry resource attributes
func (c *ProductionConfig) GetResourceAttributes() *resource.Resource {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(c.ServiceName),
		semconv.ServiceVersion(c.ServiceVersion),
		semconv.DeploymentEnvironment(c.Environment),
	}

	// Add deployment and instance information
	if c.DeploymentID != "" {
		attrs = append(attrs, attribute.String("deployment.id", c.DeploymentID))
	}
	if c.InstanceID != "" {
		attrs = append(attrs, attribute.String("service.instance.id", c.InstanceID))
	}

	// Add custom resource attributes
	for key, value := range c.ResourceAttributes {
		attrs = append(attrs, attribute.String(key, value))
	}

	// Add runtime information
	attrs = append(attrs,
		attribute.String("telemetry.sdk.name", "opentelemetry"),
		attribute.String("telemetry.sdk.language", "go"),
		attribute.String("telemetry.sdk.version", "1.21.0"),
	)

	resource, _ := resource.New(
		context.Background(),
		resource.WithAttributes(attrs...),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithProcess(),
		resource.WithContainer(),
		resource.WithOS(),
	)

	return resource
}

// GetLoggingConfig returns logging configuration
func (c *ProductionConfig) GetLoggingConfig() *LoggingConfig {
	var level LogLevel
	switch strings.ToLower(c.LogLevel) {
	case "debug":
		level = LevelDebug
	case "info":
		level = LevelInfo
	case "warn":
		level = LevelWarn
	case "error":
		level = LevelError
	case "fatal":
		level = LevelFatal
	default:
		level = LevelInfo
	}

	return &LoggingConfig{
		Level:             level,
		OutputFile:        c.LogFile,
		MaxSize:           c.LogRotationMaxSize,
		MaxBackups:        c.LogRotationMaxBackups,
		MaxAge:            c.LogRotationMaxAge,
		Compress:          c.LogRotationCompress,
		EnableConsole:     c.ConsoleExporter || c.DevelopmentMode,
		EnableTraceID:     true,
		EnableSpanID:      true,
		EnableStructured:  c.LogFormat == "json",
		SamplingEnabled:   c.EnableLogSampling,
		SamplingRate:      c.TracingSampleRate,
		SecurityFiltering: c.EnableSecurityFiltering,
	}
}

// Helper functions for environment variable parsing

func getEnvString(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if floatValue, err := strconv.ParseFloat(value, 64); err == nil {
			return floatValue
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

func parseProductionResourceAttributes() map[string]string {
	attrs := make(map[string]string)

	// Parse OTEL_RESOURCE_ATTRIBUTES format: key1=value1,key2=value2
	if value := os.Getenv("OTEL_RESOURCE_ATTRIBUTES"); value != "" {
		pairs := strings.Split(value, ",")
		for _, pair := range pairs {
			kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
			if len(kv) == 2 {
				attrs[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
			}
		}
	}

	return attrs
}

func parseOTLPHeaders() map[string]string {
	headers := make(map[string]string)

	// Parse OTEL_EXPORTER_OTLP_HEADERS format: key1=value1,key2=value2
	if value := os.Getenv("OTEL_EXPORTER_OTLP_HEADERS"); value != "" {
		pairs := strings.Split(value, ",")
		for _, pair := range pairs {
			kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
			if len(kv) == 2 {
				headers[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
			}
		}
	}

	return headers
}

func generateInstanceID() string {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}
	return fmt.Sprintf("%s-%d", hostname, os.Getpid())
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// Production configuration presets

// GetDevelopmentConfig returns a configuration optimized for development
func GetDevelopmentConfig() *ProductionConfig {
	config, _ := NewProductionConfig()
	config.Environment = "development"
	config.DevelopmentMode = true
	config.DebugLogging = true
	config.ConsoleExporter = true
	config.TracingSampleRate = 1.0
	config.MetricsInterval = 5 * time.Second
	config.LogLevel = "debug"
	config.VerboseLogging = true
	return config
}

// GetStagingConfig returns a configuration optimized for staging
func GetStagingConfig() *ProductionConfig {
	config, _ := NewProductionConfig()
	config.Environment = "staging"
	config.DevelopmentMode = false
	config.DebugLogging = false
	config.ConsoleExporter = false
	config.TracingSampleRate = 0.5
	config.MetricsInterval = 15 * time.Second
	config.LogLevel = "info"
	return config
}

// GetProductionConfig returns a configuration optimized for production
func GetProductionConfig() *ProductionConfig {
	config, _ := NewProductionConfig()
	config.Environment = "production"
	config.DevelopmentMode = false
	config.DebugLogging = false
	config.ConsoleExporter = false
	config.TracingSampleRate = 0.1
	config.MetricsInterval = 30 * time.Second
	config.LogLevel = "warn"
	config.VerboseLogging = false
	return config
}

// ConfigurationTemplate provides template methods for generating configuration files
type ConfigurationTemplate struct {
	Environment string
	Config      *ProductionConfig
}

// GenerateDockerCompose generates a docker-compose.yml configuration
func (ct *ConfigurationTemplate) GenerateDockerCompose() string {
	return fmt.Sprintf(`version: '3.8'

services:
  tmi-api:
    image: tmi-api:latest
    ports:
      - "8080:8080"
    environment:
      # Service Configuration
      - OTEL_SERVICE_NAME=%s
      - OTEL_SERVICE_VERSION=%s
      - OTEL_ENVIRONMENT=%s
      
      # Tracing Configuration
      - OTEL_TRACING_ENABLED=%t
      - OTEL_TRACING_SAMPLE_RATE=%.2f
      
      # Metrics Configuration
      - OTEL_METRICS_ENABLED=%t
      - OTEL_METRICS_INTERVAL=%s
      
      # Logging Configuration
      - LOG_LEVEL=%s
      - LOG_FORMAT=%s
      
      # Exporter Configuration
      - OTEL_EXPORTER_TYPE=%s
      - OTEL_EXPORTER_OTLP_ENDPOINT=%s
      
      # Performance Configuration
      - OTEL_PERFORMANCE_PROFILE=%s
      - OTEL_MAX_MEMORY_MB=%d
      
    volumes:
      - ./logs:/var/log/tmi
    depends_on:
      - postgres
      - redis
      - jaeger
      
  postgres:
    image: postgres:15
    environment:
      - POSTGRES_DB=tmi
      - POSTGRES_USER=tmi
      - POSTGRES_PASSWORD=tmi_password
    volumes:
      - postgres_data:/var/lib/postgresql/data
      
  redis:
    image: redis:7-alpine
    volumes:
      - redis_data:/data
      
  jaeger:
    image: jaegertracing/all-in-one:latest
    ports:
      - "16686:16686"
      - "14268:14268"
    environment:
      - COLLECTOR_OTLP_ENABLED=true

volumes:
  postgres_data:
  redis_data:
`,
		ct.Config.ServiceName,
		ct.Config.ServiceVersion,
		ct.Config.Environment,
		ct.Config.TracingEnabled,
		ct.Config.TracingSampleRate,
		ct.Config.MetricsEnabled,
		ct.Config.MetricsInterval.String(),
		ct.Config.LogLevel,
		ct.Config.LogFormat,
		ct.Config.ExporterType,
		ct.Config.OTLPEndpoint,
		ct.Config.PerformanceProfile,
		ct.Config.MaxMemoryMB,
	)
}

// GenerateKubernetesManifest generates Kubernetes deployment manifests
func (ct *ConfigurationTemplate) GenerateKubernetesManifest() string {
	return fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: tmi-api
  namespace: tmi
  labels:
    app: tmi-api
    environment: %s
spec:
  replicas: 3
  selector:
    matchLabels:
      app: tmi-api
  template:
    metadata:
      labels:
        app: tmi-api
        environment: %s
    spec:
      containers:
      - name: tmi-api
        image: tmi-api:latest
        ports:
        - containerPort: 8080
        env:
        # Service Configuration
        - name: OTEL_SERVICE_NAME
          value: "%s"
        - name: OTEL_SERVICE_VERSION
          value: "%s"
        - name: OTEL_ENVIRONMENT
          value: "%s"
        - name: INSTANCE_ID
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        
        # Tracing Configuration
        - name: OTEL_TRACING_ENABLED
          value: "%t"
        - name: OTEL_TRACING_SAMPLE_RATE
          value: "%.2f"
        
        # Metrics Configuration
        - name: OTEL_METRICS_ENABLED
          value: "%t"
        - name: OTEL_METRICS_INTERVAL
          value: "%s"
        
        # Logging Configuration
        - name: LOG_LEVEL
          value: "%s"
        - name: LOG_FORMAT
          value: "%s"
        
        # Exporter Configuration
        - name: OTEL_EXPORTER_TYPE
          value: "%s"
        - name: OTEL_EXPORTER_OTLP_ENDPOINT
          value: "%s"
        
        resources:
          requests:
            memory: "%dMi"
            cpu: "100m"
          limits:
            memory: "%dMi"
            cpu: "500m"
        
        livenessProbe:
          httpGet:
            path: /
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
        
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
          
        volumeMounts:
        - name: log-volume
          mountPath: /var/log/tmi
          
      volumes:
      - name: log-volume
        emptyDir: {}

---
apiVersion: v1
kind: Service
metadata:
  name: tmi-api-service
  namespace: tmi
spec:
  selector:
    app: tmi-api
  ports:
  - protocol: TCP
    port: 80
    targetPort: 8080
  type: ClusterIP

---
apiVersion: v1
kind: ConfigMap
metadata:
  name: tmi-config
  namespace: tmi
data:
  performance-profile: "%s"
  max-memory-mb: "%d"
`,
		ct.Config.Environment,
		ct.Config.Environment,
		ct.Config.ServiceName,
		ct.Config.ServiceVersion,
		ct.Config.Environment,
		ct.Config.TracingEnabled,
		ct.Config.TracingSampleRate,
		ct.Config.MetricsEnabled,
		ct.Config.MetricsInterval.String(),
		ct.Config.LogLevel,
		ct.Config.LogFormat,
		ct.Config.ExporterType,
		ct.Config.OTLPEndpoint,
		ct.Config.MaxMemoryMB/2, // Request half of limit
		ct.Config.MaxMemoryMB,
		ct.Config.PerformanceProfile,
		ct.Config.MaxMemoryMB,
	)
}

// GenerateEnvironmentFile generates a .env file for configuration
func (ct *ConfigurationTemplate) GenerateEnvironmentFile() string {
	return fmt.Sprintf(`# TMI OpenTelemetry Configuration - %s Environment

# Service Configuration
OTEL_SERVICE_NAME=%s
OTEL_SERVICE_VERSION=%s
OTEL_ENVIRONMENT=%s
DEPLOYMENT_ID=
INSTANCE_ID=

# Tracing Configuration
OTEL_TRACING_ENABLED=%t
OTEL_TRACING_SAMPLE_RATE=%.2f
OTEL_TRACING_BATCH_TIMEOUT=%s
OTEL_TRACING_BATCH_SIZE=%d
OTEL_TRACING_QUEUE_SIZE=%d

# Metrics Configuration
OTEL_METRICS_ENABLED=%t
OTEL_METRICS_INTERVAL=%s
OTEL_METRICS_BATCH_TIMEOUT=%s
OTEL_METRICS_BATCH_SIZE=%d
OTEL_METRICS_QUEUE_SIZE=%d

# Logging Configuration
OTEL_LOGGING_ENABLED=%t
LOG_LEVEL=%s
LOG_FORMAT=%s
LOG_FILE=%s
LOG_ROTATION_MAX_SIZE=%d
LOG_ROTATION_MAX_BACKUPS=%d
LOG_ROTATION_MAX_AGE=%d
LOG_ROTATION_COMPRESS=%t

# Exporter Configuration
OTEL_EXPORTER_TYPE=%s
OTEL_EXPORTER_OTLP_ENDPOINT=%s
OTEL_EXPORTER_OTLP_TIMEOUT=%s
OTEL_EXPORTER_OTLP_RETRY_ENABLED=%t
OTEL_EXPORTER_OTLP_RETRY_MAX_ATTEMPTS=%d
OTEL_EXPORTER_OTLP_RETRY_BACKOFF=%s

# Jaeger Configuration (if using Jaeger exporter)
JAEGER_ENDPOINT=%s
JAEGER_USER=%s
JAEGER_PASSWORD=%s

# Prometheus Configuration (if using Prometheus exporter)
PROMETHEUS_ENDPOINT=%s
PROMETHEUS_NAMESPACE=%s
PROMETHEUS_JOB_NAME=%s

# Security Configuration
OTEL_TLS_ENABLED=%t
OTEL_TLS_CERT_FILE=%s
OTEL_TLS_KEY_FILE=%s
OTEL_TLS_CA_FILE=%s
OTEL_TLS_INSECURE_SKIP_VERIFY=%t

# Performance Configuration
OTEL_MAX_CPUS=%d
OTEL_MAX_MEMORY_MB=%d
OTEL_PERFORMANCE_PROFILE=%s
OTEL_SAMPLING_STRATEGY=%s

# Feature Flags
OTEL_ENABLE_HTTP_INSTRUMENTATION=%t
OTEL_ENABLE_DATABASE_INSTRUMENTATION=%t
OTEL_ENABLE_REDIS_INSTRUMENTATION=%t
OTEL_ENABLE_GRPC_INSTRUMENTATION=%t
OTEL_ENABLE_RUNTIME_METRICS=%t
OTEL_ENABLE_CUSTOM_METRICS=%t
OTEL_ENABLE_SECURITY_FILTERING=%t
OTEL_ENABLE_LOG_SAMPLING=%t

# Development/Debug Settings
OTEL_DEVELOPMENT_MODE=%t
OTEL_DEBUG_LOGGING=%t
OTEL_CONSOLE_EXPORTER=%t
OTEL_VERBOSE_LOGGING=%t
`,
		ct.Config.Environment,
		ct.Config.ServiceName,
		ct.Config.ServiceVersion,
		ct.Config.Environment,
		ct.Config.TracingEnabled,
		ct.Config.TracingSampleRate,
		ct.Config.TracingBatchTimeout.String(),
		ct.Config.TracingBatchSize,
		ct.Config.TracingQueueSize,
		ct.Config.MetricsEnabled,
		ct.Config.MetricsInterval.String(),
		ct.Config.MetricsBatchTimeout.String(),
		ct.Config.MetricsBatchSize,
		ct.Config.MetricsQueueSize,
		ct.Config.LoggingEnabled,
		ct.Config.LogLevel,
		ct.Config.LogFormat,
		ct.Config.LogFile,
		ct.Config.LogRotationMaxSize,
		ct.Config.LogRotationMaxBackups,
		ct.Config.LogRotationMaxAge,
		ct.Config.LogRotationCompress,
		ct.Config.ExporterType,
		ct.Config.OTLPEndpoint,
		ct.Config.OTLPTimeout.String(),
		ct.Config.OTLPRetryEnabled,
		ct.Config.OTLPRetryMaxAttempts,
		ct.Config.OTLPRetryBackoff.String(),
		ct.Config.JaegerEndpoint,
		ct.Config.JaegerUser,
		ct.Config.JaegerPassword,
		ct.Config.PrometheusEndpoint,
		ct.Config.PrometheusNamespace,
		ct.Config.PrometheusJobName,
		ct.Config.TLSEnabled,
		ct.Config.TLSCertFile,
		ct.Config.TLSKeyFile,
		ct.Config.TLSCAFile,
		ct.Config.TLSInsecureSkipVerify,
		ct.Config.MaxCPUs,
		ct.Config.MaxMemoryMB,
		ct.Config.PerformanceProfile,
		ct.Config.SamplingStrategy,
		ct.Config.EnableHTTPInstrumentation,
		ct.Config.EnableDatabaseInstrumentation,
		ct.Config.EnableRedisInstrumentation,
		ct.Config.EnableGRPCInstrumentation,
		ct.Config.EnableRuntimeMetrics,
		ct.Config.EnableCustomMetrics,
		ct.Config.EnableSecurityFiltering,
		ct.Config.EnableLogSampling,
		ct.Config.DevelopmentMode,
		ct.Config.DebugLogging,
		ct.Config.ConsoleExporter,
		ct.Config.VerboseLogging,
	)
}
