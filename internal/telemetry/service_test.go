package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewService(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid configuration",
			config: &Config{
				ServiceName:       "test-service",
				ServiceVersion:    "1.0.0",
				Environment:       "test",
				TracingEnabled:    true,
				TracingSampleRate: 0.1,
				MetricsEnabled:    true,
				MetricsInterval:   30 * time.Second,
				TracingEndpoint:   "", // Disable OTLP export for tests
				MetricsEndpoint:   "", // Disable OTLP export for tests
				ConsoleExporter:   true,
				IsDevelopment:     true,
				ResourceAttributes: map[string]string{
					"test.attribute": "value",
				},
			},
			wantErr: false,
		},
		{
			name: "tracing disabled",
			config: &Config{
				ServiceName:     "test-service",
				ServiceVersion:  "1.0.0",
				Environment:     "test",
				TracingEnabled:  false,
				MetricsEnabled:  true,
				MetricsInterval: 30 * time.Second,
				IsDevelopment:   true,
			},
			wantErr: false,
		},
		{
			name: "metrics disabled",
			config: &Config{
				ServiceName:       "test-service",
				ServiceVersion:    "1.0.0",
				Environment:       "test",
				TracingEnabled:    true,
				TracingSampleRate: 0.1,
				MetricsEnabled:    false,
				ConsoleExporter:   true,
				IsDevelopment:     true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := NewService(tt.config)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, service)
			} else {
				require.NoError(t, err)
				require.NotNil(t, service)

				// Verify service components
				assert.NotNil(t, service.resource)
				assert.Equal(t, tt.config, service.config)

				if tt.config.TracingEnabled {
					assert.NotNil(t, service.tracerProvider)
					assert.NotNil(t, service.tracer)
				}

				if tt.config.MetricsEnabled {
					assert.NotNil(t, service.meterProvider)
					assert.NotNil(t, service.meter)
				}

				// Test shutdown
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				err = service.Shutdown(ctx)
				assert.NoError(t, err)
			}
		})
	}
}

func TestService_Health(t *testing.T) {
	config := &Config{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TracingEnabled:  true,
		MetricsEnabled:  true,
		MetricsInterval: 30 * time.Second,
		ConsoleExporter: true,
		IsDevelopment:   true,
	}

	service, err := NewService(config)
	require.NoError(t, err)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = service.Shutdown(ctx)
	}()

	health := service.Health()

	assert.True(t, health.Healthy)
	assert.Equal(t, "test-service", health.Details["service_name"])
	assert.Equal(t, "1.0.0", health.Details["service_version"])
	assert.Equal(t, "test", health.Details["environment"])
	assert.True(t, health.Details["tracing_enabled"].(bool))
	assert.True(t, health.Details["metrics_enabled"].(bool))
}

func TestService_ForceFlush(t *testing.T) {
	config := &Config{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TracingEnabled:  true,
		MetricsEnabled:  true,
		MetricsInterval: 30 * time.Second,
		ConsoleExporter: true,
		IsDevelopment:   true,
	}

	service, err := NewService(config)
	require.NoError(t, err)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = service.Shutdown(ctx)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = service.ForceFlush(ctx)
	assert.NoError(t, err)
}

func TestGlobalFunctions(t *testing.T) {
	// Test with no global service
	tracer := GetTracer()
	assert.NotNil(t, tracer)

	meter := GetMeter()
	assert.NotNil(t, meter)

	service := GetService()
	assert.Nil(t, service)

	// Test initialization
	config := &Config{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TracingEnabled:  true,
		MetricsEnabled:  true,
		MetricsInterval: 30 * time.Second,
		ConsoleExporter: true,
		IsDevelopment:   true,
	}

	err := Initialize(config)
	require.NoError(t, err)

	// Test with global service
	tracer = GetTracer()
	assert.NotNil(t, tracer)

	meter = GetMeter()
	assert.NotNil(t, meter)

	service = GetService()
	assert.NotNil(t, service)

	// Test shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = Shutdown(ctx)
	assert.NoError(t, err)

	// Reset global service for other tests
	globalService = nil
}
