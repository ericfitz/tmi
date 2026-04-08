package otel

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const meterName = "tmi"

// TMIMetrics holds all custom OTel metric instruments for TMI.
type TMIMetrics struct {
	CacheHits                   metric.Int64Counter
	CacheMisses                 metric.Int64Counter
	WebSocketActiveSessions     metric.Int64UpDownCounter
	WebSocketActiveParticipants metric.Int64UpDownCounter
	WebSocketMessages           metric.Int64Counter
	WebhookDeliveries           metric.Int64Counter
	TimmyActiveSessions         metric.Int64UpDownCounter
	TimmyLLMDuration            metric.Float64Histogram
	TimmyLLMTokens              metric.Int64Counter
	TimmyEmbedDuration          metric.Float64Histogram
	TimmySSEDuration            metric.Float64Histogram
	TimmySSEEvents              metric.Int64Counter
}

// GlobalMetrics holds the TMI metrics instance for package-level access.
var GlobalMetrics *TMIMetrics

// NewTMIMetrics creates and registers all TMI metric instruments.
func NewTMIMetrics() (*TMIMetrics, error) {
	meter := otel.Meter(meterName)
	m := &TMIMetrics{}
	var err error

	if m.CacheHits, err = meter.Int64Counter("tmi.cache.hit",
		metric.WithDescription("Cache hits")); err != nil {
		return nil, err
	}
	if m.CacheMisses, err = meter.Int64Counter("tmi.cache.miss",
		metric.WithDescription("Cache misses")); err != nil {
		return nil, err
	}
	if m.WebSocketActiveSessions, err = meter.Int64UpDownCounter("tmi.websocket.sessions.active",
		metric.WithDescription("Active WebSocket sessions")); err != nil {
		return nil, err
	}
	if m.WebSocketActiveParticipants, err = meter.Int64UpDownCounter("tmi.websocket.participants.active",
		metric.WithDescription("Active WebSocket participants")); err != nil {
		return nil, err
	}
	if m.WebSocketMessages, err = meter.Int64Counter("tmi.websocket.messages",
		metric.WithDescription("WebSocket messages")); err != nil {
		return nil, err
	}
	if m.WebhookDeliveries, err = meter.Int64Counter("tmi.webhook.deliveries",
		metric.WithDescription("Webhook delivery attempts")); err != nil {
		return nil, err
	}
	if m.TimmyActiveSessions, err = meter.Int64UpDownCounter("tmi.timmy.session.active",
		metric.WithDescription("Active Timmy chat sessions")); err != nil {
		return nil, err
	}
	if m.TimmyLLMDuration, err = meter.Float64Histogram("tmi.timmy.llm.duration",
		metric.WithDescription("LLM call latency in seconds"),
		metric.WithUnit("s")); err != nil {
		return nil, err
	}
	if m.TimmyLLMTokens, err = meter.Int64Counter("tmi.timmy.llm.tokens",
		metric.WithDescription("LLM tokens consumed")); err != nil {
		return nil, err
	}
	if m.TimmyEmbedDuration, err = meter.Float64Histogram("tmi.timmy.embedding.duration",
		metric.WithDescription("Embedding call latency in seconds"),
		metric.WithUnit("s")); err != nil {
		return nil, err
	}
	if m.TimmySSEDuration, err = meter.Float64Histogram("tmi.timmy.sse.duration",
		metric.WithDescription("SSE stream total duration in seconds"),
		metric.WithUnit("s")); err != nil {
		return nil, err
	}
	if m.TimmySSEEvents, err = meter.Int64Counter("tmi.timmy.sse.events",
		metric.WithDescription("SSE events sent")); err != nil {
		return nil, err
	}

	return m, nil
}

// DBPoolStats holds snapshot statistics for a database connection pool.
type DBPoolStats struct {
	OpenConnections int
	Idle            int
	InUse           int
	WaitCount       int64
	WaitDuration    time.Duration
}

// RedisPoolStats holds snapshot statistics for a Redis connection pool.
type RedisPoolStats struct {
	ActiveCount int
	IdleCount   int
}

// RegisterPoolMetrics registers observable gauges for DB and Redis pool monitoring.
// Either dbStatsFn or redisStatsFn may be nil to skip registration for that pool.
func RegisterPoolMetrics(dbStatsFn func() DBPoolStats, redisStatsFn func() RedisPoolStats) error {
	meter := otel.Meter(meterName)

	if dbStatsFn != nil {
		if _, err := meter.Int64ObservableGauge("tmi.db.pool.open",
			metric.WithDescription("Open database connections"),
			metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
				o.Observe(int64(dbStatsFn().OpenConnections))
				return nil
			}),
		); err != nil {
			return err
		}

		if _, err := meter.Int64ObservableGauge("tmi.db.pool.idle",
			metric.WithDescription("Idle database connections"),
			metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
				o.Observe(int64(dbStatsFn().Idle))
				return nil
			}),
		); err != nil {
			return err
		}

		if _, err := meter.Int64ObservableGauge("tmi.db.pool.in_use",
			metric.WithDescription("In-use database connections"),
			metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
				o.Observe(int64(dbStatsFn().InUse))
				return nil
			}),
		); err != nil {
			return err
		}
	}

	if redisStatsFn != nil {
		if _, err := meter.Int64ObservableGauge("tmi.redis.pool.active",
			metric.WithDescription("Active Redis connections"),
			metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
				o.Observe(int64(redisStatsFn().ActiveCount))
				return nil
			}),
		); err != nil {
			return err
		}

		if _, err := meter.Int64ObservableGauge("tmi.redis.pool.idle",
			metric.WithDescription("Idle Redis connections"),
			metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
				o.Observe(int64(redisStatsFn().IdleCount))
				return nil
			}),
		); err != nil {
			return err
		}
	}

	return nil
}
