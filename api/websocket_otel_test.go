package api

import (
	"context"
	"testing"

	tmiotel "github.com/ericfitz/tmi/internal/otel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// findUpDownCounterValue scans collected ResourceMetrics and sums all data points
// for a named UpDownCounter (reported as a Sum metric in the SDK).
func findUpDownCounterValue(rm metricdata.ResourceMetrics, name string) int64 {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
					var total int64
					for _, dp := range sum.DataPoints {
						total += dp.Value
					}
					return total
				}
			}
		}
	}
	return 0
}

// TestWebSocketOTelMetrics_SessionCounter verifies that adding and removing from
// WebSocketActiveSessions produces the correct net value in collected metrics.
func TestWebSocketOTelMetrics_SessionCounter(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	defer func() { _ = mp.Shutdown(context.Background()) }()

	metrics, err := tmiotel.NewTMIMetrics()
	require.NoError(t, err)
	tmiotel.GlobalMetrics = metrics
	defer func() { tmiotel.GlobalMetrics = nil }()

	ctx := context.Background()

	// Simulate session start/end pattern from websocket.go:
	// two sessions opened, one closed → net 1 active session.
	metrics.WebSocketActiveSessions.Add(ctx, 1)
	metrics.WebSocketActiveSessions.Add(ctx, 1)
	metrics.WebSocketActiveSessions.Add(ctx, -1)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	value := findUpDownCounterValue(rm, "tmi.websocket.sessions.active")
	assert.Equal(t, int64(1), value,
		"tmi.websocket.sessions.active should be 1 after +1, +1, -1")
}

// TestWebSocketOTelMetrics_AllSessionsClosed verifies the counter reaches zero
// when every opened session is also closed.
func TestWebSocketOTelMetrics_AllSessionsClosed(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	defer func() { _ = mp.Shutdown(context.Background()) }()

	metrics, err := tmiotel.NewTMIMetrics()
	require.NoError(t, err)
	tmiotel.GlobalMetrics = metrics
	defer func() { tmiotel.GlobalMetrics = nil }()

	ctx := context.Background()

	metrics.WebSocketActiveSessions.Add(ctx, 1)
	metrics.WebSocketActiveSessions.Add(ctx, 1)
	metrics.WebSocketActiveSessions.Add(ctx, 1)
	metrics.WebSocketActiveSessions.Add(ctx, -1)
	metrics.WebSocketActiveSessions.Add(ctx, -1)
	metrics.WebSocketActiveSessions.Add(ctx, -1)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	value := findUpDownCounterValue(rm, "tmi.websocket.sessions.active")
	assert.Equal(t, int64(0), value,
		"tmi.websocket.sessions.active should be 0 when all sessions are closed")
}
