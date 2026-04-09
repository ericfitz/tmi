package api

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/ericfitz/tmi/auth/db"
	tmiotel "github.com/ericfitz/tmi/internal/otel"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// setupOtelMetricsProvider creates an in-memory meter provider and registers TMIMetrics as GlobalMetrics.
// Returns the reader and a cleanup function that resets GlobalMetrics.
func setupOtelMetricsProvider(t *testing.T) (*sdkmetric.ManualReader, func()) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)

	metrics, err := tmiotel.NewTMIMetrics()
	require.NoError(t, err)
	tmiotel.GlobalMetrics = metrics

	cleanup := func() {
		tmiotel.GlobalMetrics = nil
		_ = mp.Shutdown(context.Background())
	}
	return reader, cleanup
}

// newMiniredisRedisDB creates a RedisDB backed by a miniredis instance for unit tests.
func newMiniredisRedisDB(t *testing.T) (*db.RedisDB, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)

	// Construct RedisDB by directly creating the client (bypasses OTel instrumentation that needs a real connection)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	// Wrap the client in a RedisDB using the exported constructor
	redisDB, err := db.NewRedisDB(db.RedisConfig{Host: mr.Host(), Port: mr.Port()})
	if err != nil {
		// If NewRedisDB fails (shouldn't with miniredis), fall back to direct construction
		t.Logf("NewRedisDB via config failed (%v), using direct client", err)
		_ = client.Close()
		t.FailNow()
	}
	_ = client.Close() // Close the probe client; redisDB has its own

	return redisDB, mr
}

// findCounterSum scans collected ResourceMetrics and sums all data points for a named counter.
func findCounterSum(t *testing.T, rm metricdata.ResourceMetrics, metricName string) int64 {
	t.Helper()

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == metricName {
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

// TestCacheOtel_ThreatMissAndHit verifies tmi.cache.miss and tmi.cache.hit are recorded
// when calling GetCachedThreat on a CacheService backed by miniredis.
func TestCacheOtel_ThreatMissAndHit(t *testing.T) {
	reader, cleanup := setupOtelMetricsProvider(t)
	defer cleanup()

	redisDB, _ := newMiniredisRedisDB(t)
	defer func() { _ = redisDB.Close() }()

	cs := NewCacheService(redisDB)
	ctx := context.Background()

	threatID := uuid.New().String()

	// --- Cache miss ---
	result, err := cs.GetCachedThreat(ctx, threatID)
	require.NoError(t, err)
	assert.Nil(t, result, "expected cache miss to return nil")

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))
	assert.Equal(t, int64(1), findCounterSum(t, rm, "tmi.cache.miss"), "tmi.cache.miss should be 1 after a miss")
	assert.Equal(t, int64(0), findCounterSum(t, rm, "tmi.cache.hit"), "tmi.cache.hit should be 0 before any hit")

	// --- Cache the item ---
	id := uuid.MustParse(threatID)
	threat := &Threat{
		Id:   &id,
		Name: "SQL Injection",
	}
	require.NoError(t, cs.CacheThreat(ctx, threat))

	// --- Cache hit ---
	hit, err := cs.GetCachedThreat(ctx, threatID)
	require.NoError(t, err)
	require.NotNil(t, hit, "expected a cache hit to return the threat")
	assert.Equal(t, "SQL Injection", hit.Name)

	var rm2 metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm2))
	assert.Equal(t, int64(1), findCounterSum(t, rm2, "tmi.cache.miss"), "tmi.cache.miss should still be 1")
	assert.Equal(t, int64(1), findCounterSum(t, rm2, "tmi.cache.hit"), "tmi.cache.hit should be 1 after a hit")
}

// TestCacheOtel_MultipleMisses verifies that each miss increments tmi.cache.miss.
func TestCacheOtel_MultipleMisses(t *testing.T) {
	reader, cleanup := setupOtelMetricsProvider(t)
	defer cleanup()

	redisDB, _ := newMiniredisRedisDB(t)
	defer func() { _ = redisDB.Close() }()

	cs := NewCacheService(redisDB)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		result, err := cs.GetCachedThreat(ctx, uuid.New().String())
		require.NoError(t, err)
		assert.Nil(t, result)
	}

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))
	assert.Equal(t, int64(3), findCounterSum(t, rm, "tmi.cache.miss"), "tmi.cache.miss should be 3 after three misses")
	assert.Equal(t, int64(0), findCounterSum(t, rm, "tmi.cache.hit"), "tmi.cache.hit should remain 0")
}

// TestCacheOtel_NoMetricsWhenGlobalNil verifies that GetCachedThreat is safe when GlobalMetrics is nil.
func TestCacheOtel_NoMetricsWhenGlobalNil(t *testing.T) {
	// Ensure GlobalMetrics is nil for this test.
	prev := tmiotel.GlobalMetrics
	tmiotel.GlobalMetrics = nil
	defer func() { tmiotel.GlobalMetrics = prev }()

	redisDB, _ := newMiniredisRedisDB(t)
	defer func() { _ = redisDB.Close() }()

	cs := NewCacheService(redisDB)
	ctx := context.Background()

	// Should not panic
	result, err := cs.GetCachedThreat(ctx, uuid.New().String())
	require.NoError(t, err)
	assert.Nil(t, result)
}
