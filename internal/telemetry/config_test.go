package telemetry

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected func(*Config)
		wantErr  bool
	}{
		{
			name:    "default configuration",
			envVars: map[string]string{},
			expected: func(c *Config) {
				assert.Equal(t, "tmi-api", c.ServiceName)
				assert.Equal(t, "1.0.0", c.ServiceVersion)
				assert.Equal(t, "development", c.Environment)
				assert.True(t, c.TracingEnabled)
				assert.Equal(t, 1.0, c.TracingSampleRate)
				assert.True(t, c.MetricsEnabled)
				assert.Equal(t, 30*time.Second, c.MetricsInterval)
				assert.True(t, c.LoggingEnabled)
				assert.True(t, c.IsDevelopment)
			},
		},
		{
			name: "custom configuration",
			envVars: map[string]string{
				"OTEL_SERVICE_NAME":                  "custom-service",
				"OTEL_SERVICE_VERSION":               "2.0.0",
				"OTEL_ENVIRONMENT":                   "production",
				"OTEL_TRACING_SAMPLE_RATE":           "0.1",
				"OTEL_METRICS_INTERVAL":              "60s",
				"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://custom:4318/v1/traces",
				"OTEL_EXPORTER_OTLP_TRACES_HEADERS":  "api-key=secret,x-tenant=test",
				"OTEL_RESOURCE_ATTRIBUTES":           "service.namespace=tmi,service.instance.id=1",
			},
			expected: func(c *Config) {
				assert.Equal(t, "custom-service", c.ServiceName)
				assert.Equal(t, "2.0.0", c.ServiceVersion)
				assert.Equal(t, "production", c.Environment)
				assert.Equal(t, 0.1, c.TracingSampleRate)
				assert.Equal(t, 60*time.Second, c.MetricsInterval)
				assert.Equal(t, "http://custom:4318/v1/traces", c.TracingEndpoint)
				assert.Equal(t, "secret", c.TracingHeaders["api-key"])
				assert.Equal(t, "test", c.TracingHeaders["x-tenant"])
				assert.Equal(t, "tmi", c.ResourceAttributes["service.namespace"])
				assert.Equal(t, "1", c.ResourceAttributes["service.instance.id"])
				assert.False(t, c.IsDevelopment)
			},
		},
		{
			name: "invalid sample rate",
			envVars: map[string]string{
				"OTEL_TRACING_SAMPLE_RATE": "1.5",
			},
			wantErr: true,
		},
		{
			name: "empty service name",
			envVars: map[string]string{
				"OTEL_SERVICE_NAME": "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tt.envVars {
				_ = os.Setenv(k, v)
			}
			defer func() {
				for k := range tt.envVars {
					_ = os.Unsetenv(k)
				}
			}()

			config, err := LoadConfig()

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, config)
				tt.expected(config)
			}
		})
	}
}

func TestConfig_GetSamplingConfig(t *testing.T) {
	tests := []struct {
		name        string
		environment string
		expected    SamplingConfig
	}{
		{
			name:        "production sampling",
			environment: "production",
			expected: SamplingConfig{
				TraceSampleRate:   0.1,
				MetricsSampleRate: 1.0,
				LogSampleRate:     0.5,
			},
		},
		{
			name:        "staging sampling",
			environment: "staging",
			expected: SamplingConfig{
				TraceSampleRate:   0.5,
				MetricsSampleRate: 1.0,
				LogSampleRate:     1.0,
			},
		},
		{
			name:        "development sampling",
			environment: "development",
			expected: SamplingConfig{
				TraceSampleRate:   1.0,
				MetricsSampleRate: 1.0,
				LogSampleRate:     1.0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				Environment:       tt.environment,
				TracingSampleRate: 1.0,
			}

			sampling := config.GetSamplingConfig()
			assert.Equal(t, tt.expected, sampling)
		})
	}
}

func TestConfig_GetResourceAttributes(t *testing.T) {
	config := &Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		ResourceAttributes: map[string]string{
			"custom.attribute": "value",
			"service.name":     "should-not-override", // This should be overridden
		},
	}

	attrs := config.GetResourceAttributes()

	assert.Equal(t, "test-service", attrs["service.name"])
	assert.Equal(t, "1.0.0", attrs["service.version"])
	assert.Equal(t, "test", attrs["deployment.environment"])
	assert.Equal(t, "value", attrs["custom.attribute"])
}

func TestParseHeaders(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: map[string]string{},
		},
		{
			name:  "single header",
			input: "api-key=secret",
			expected: map[string]string{
				"api-key": "secret",
			},
		},
		{
			name:  "multiple headers",
			input: "api-key=secret,x-tenant=test,authorization=bearer token",
			expected: map[string]string{
				"api-key":       "secret",
				"x-tenant":      "test",
				"authorization": "bearer token",
			},
		},
		{
			name:  "headers with spaces",
			input: " api-key = secret , x-tenant = test ",
			expected: map[string]string{
				"api-key":  "secret",
				"x-tenant": "test",
			},
		},
		{
			name:     "invalid format",
			input:    "invalid-header",
			expected: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseHeaders(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
