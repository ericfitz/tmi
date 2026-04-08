package otel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestRegisterPoolMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	defer func() { _ = mp.Shutdown(context.Background()) }()

	dbStats := func() DBPoolStats {
		return DBPoolStats{
			OpenConnections: 5,
			Idle:            3,
			InUse:           2,
			WaitCount:       10,
			WaitDuration:    100 * time.Millisecond,
		}
	}

	err := RegisterPoolMetrics(dbStats, nil)
	require.NoError(t, err)

	var rm metricdata.ResourceMetrics
	err = reader.Collect(context.Background(), &rm)
	require.NoError(t, err)
	assert.Greater(t, len(rm.ScopeMetrics), 0)
}

func TestNewTMIMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	defer func() { _ = mp.Shutdown(context.Background()) }()

	metrics, err := NewTMIMetrics()
	require.NoError(t, err)
	require.NotNil(t, metrics)
	require.NotNil(t, metrics.CacheHits)
	require.NotNil(t, metrics.CacheMisses)
	require.NotNil(t, metrics.TimmyActiveSessions)
	require.NotNil(t, metrics.TimmyLLMDuration)
	require.NotNil(t, metrics.TimmyLLMTokens)

	// Record a metric and verify it's collected
	metrics.CacheHits.Add(context.Background(), 1)

	var rm metricdata.ResourceMetrics
	err = reader.Collect(context.Background(), &rm)
	require.NoError(t, err)
	assert.Greater(t, len(rm.ScopeMetrics), 0, "should have collected metrics")
}
