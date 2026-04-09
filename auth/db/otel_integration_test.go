package db

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/opentelemetry-go-extra/otelgorm"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestOTelGORM_ProducesSpansOnQuery verifies that otelgorm emits at least one DB
// span when a GORM query is executed inside a parent span context.
func TestOTelGORM_ProducesSpansOnQuery(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	err = gormDB.Use(otelgorm.NewPlugin(otelgorm.WithDBName("test")))
	require.NoError(t, err)

	type TestModel struct {
		ID   uint   `gorm:"primarykey"`
		Name string `gorm:"size:255"`
	}
	require.NoError(t, gormDB.AutoMigrate(&TestModel{}))

	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-parent")
	result := gormDB.WithContext(ctx).Create(&TestModel{Name: "otel-test"})
	span.End()

	require.NoError(t, result.Error)

	spans := exporter.GetSpans()
	require.GreaterOrEqual(t, len(spans), 2, "should have parent span + at least one DB query span")

	// Find any span that is not our parent.
	var dbSpan *tracetest.SpanStub
	for i := range spans {
		if spans[i].Name != "test-parent" {
			dbSpan = &spans[i]
			break
		}
	}
	require.NotNil(t, dbSpan, "should have a DB span produced by otelgorm")
}

// TestOTelRedis_ProducesSpansOnCommand verifies that redisotel emits spans for
// Redis SET and GET commands executed inside a parent span context.
func TestOTelRedis_ProducesSpansOnCommand(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	mr := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer func() { _ = client.Close() }()

	err := redisotel.InstrumentTracing(client)
	require.NoError(t, err)

	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-parent")
	client.Set(ctx, "otel-key", "otel-value", 0)
	client.Get(ctx, "otel-key")
	span.End()

	spans := exporter.GetSpans()
	// Expect: parent + SET span + GET span = at least 3.
	require.GreaterOrEqual(t, len(spans), 3, "should have parent + redis SET + redis GET spans")

	// Verify at least two non-parent spans exist.
	nonParent := 0
	for _, s := range spans {
		if s.Name != "test-parent" {
			nonParent++
		}
	}
	require.GreaterOrEqual(t, nonParent, 2, "should have at least 2 Redis command spans")
}
