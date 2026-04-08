package otel

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

func TestSetup_Disabled(t *testing.T) {
	cfg := Config{
		Enabled:      false,
		SamplingRate: 1.0,
	}

	shutdown, err := Setup(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// Tracer should be a no-op
	tracer := otel.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")
	assert.False(t, span.SpanContext().IsValid(), "disabled OTel should produce invalid span contexts")
	span.End()

	// Shutdown should succeed
	err = shutdown(context.Background())
	assert.NoError(t, err)
}

func TestSetup_Enabled_ProducesSpans(t *testing.T) {
	cfg := Config{
		Enabled:      true,
		SamplingRate: 1.0,
	}

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4317")

	shutdown, err := Setup(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	defer func() {
		_ = shutdown(context.Background())
	}()

	tracer := otel.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")
	assert.True(t, span.SpanContext().IsValid(), "enabled OTel should produce valid span contexts")
	span.End()
}

var _ = trace.SpanContext{} // ensure trace import is used
