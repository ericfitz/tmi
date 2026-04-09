package api

import (
	"context"
	"net/http/httptest"
	"testing"

	tmiotel "github.com/ericfitz/tmi/internal/otel"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// findCounterValue scans collected ResourceMetrics and sums all data points for a named counter.
func findCounterValue(rm metricdata.ResourceMetrics, name string) int64 {
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

// TestSSEWriter_SendEvent_IncrementsMetric verifies that SendEvent increments
// the tmi.timmy.sse.events counter once per call.
func TestSSEWriter_SendEvent_IncrementsMetric(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	defer func() { _ = mp.Shutdown(context.Background()) }()

	metrics, err := tmiotel.NewTMIMetrics()
	require.NoError(t, err)
	tmiotel.GlobalMetrics = metrics
	defer func() { tmiotel.GlobalMetrics = nil }()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// Provide a minimal request so IsClientGone() works without panic.
	c.Request = httptest.NewRequest("GET", "/stream", nil)

	sse := NewSSEWriter(c)

	// Send 3 events of different types; ignore write errors (test recorder may not
	// fully satisfy http.Flusher, but metrics are recorded before the write).
	_ = sse.SendEvent("token", map[string]string{"content": "hello"})
	_ = sse.SendEvent("error", map[string]string{"code": "E1", "message": "oops"})
	_ = sse.SendEvent("done", map[string]string{})

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	assert.Equal(t, int64(3), findCounterValue(rm, "tmi.timmy.sse.events"),
		"tmi.timmy.sse.events should be 3 after three SendEvent calls")
}

// TestSSEWriter_SendEvent_NoMetrics_WhenGlobalNil verifies that SendEvent does not
// panic when tmiotel.GlobalMetrics is nil.
func TestSSEWriter_SendEvent_NoMetrics_WhenGlobalNil(t *testing.T) {
	prev := tmiotel.GlobalMetrics
	tmiotel.GlobalMetrics = nil
	defer func() { tmiotel.GlobalMetrics = prev }()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/stream", nil)

	sse := NewSSEWriter(c)

	assert.NotPanics(t, func() {
		_ = sse.SendEvent("token", map[string]string{"content": "safe"})
	}, "SendEvent should not panic when GlobalMetrics is nil")
}
