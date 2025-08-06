package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// metricBuilder provides a fluent interface for creating metrics with centralized error handling
type metricBuilder struct {
	meter metric.Meter
	err   error
}

// newMetricBuilder creates a new metric builder
func newMetricBuilder(meter metric.Meter) *metricBuilder {
	return &metricBuilder{meter: meter}
}

// Error returns any accumulated error
func (mb *metricBuilder) Error() error {
	return mb.err
}

// Int64Counter creates an Int64Counter metric
func (mb *metricBuilder) Int64Counter(name, desc, unit string) metric.Int64Counter {
	if mb.err != nil {
		return nil
	}
	counter, err := mb.meter.Int64Counter(
		name,
		metric.WithDescription(desc),
		metric.WithUnit(unit),
	)
	if err != nil {
		mb.err = fmt.Errorf("failed to create counter %s: %w", name, err)
		return nil
	}
	return counter
}

// Int64UpDownCounter creates an Int64UpDownCounter metric
func (mb *metricBuilder) Int64UpDownCounter(name, desc, unit string) metric.Int64UpDownCounter {
	if mb.err != nil {
		return nil
	}
	counter, err := mb.meter.Int64UpDownCounter(
		name,
		metric.WithDescription(desc),
		metric.WithUnit(unit),
	)
	if err != nil {
		mb.err = fmt.Errorf("failed to create updowncounter %s: %w", name, err)
		return nil
	}
	return counter
}

// Float64Counter creates a Float64Counter metric
func (mb *metricBuilder) Float64Counter(name, desc, unit string) metric.Float64Counter {
	if mb.err != nil {
		return nil
	}
	counter, err := mb.meter.Float64Counter(
		name,
		metric.WithDescription(desc),
		metric.WithUnit(unit),
	)
	if err != nil {
		mb.err = fmt.Errorf("failed to create float counter %s: %w", name, err)
		return nil
	}
	return counter
}

// Float64UpDownCounter creates a Float64UpDownCounter metric
func (mb *metricBuilder) Float64UpDownCounter(name, desc, unit string) metric.Float64UpDownCounter {
	if mb.err != nil {
		return nil
	}
	counter, err := mb.meter.Float64UpDownCounter(
		name,
		metric.WithDescription(desc),
		metric.WithUnit(unit),
	)
	if err != nil {
		mb.err = fmt.Errorf("failed to create float updowncounter %s: %w", name, err)
		return nil
	}
	return counter
}

// Float64Histogram creates a Float64Histogram metric with explicit bucket boundaries
func (mb *metricBuilder) Float64Histogram(name, desc, unit string, buckets []float64) metric.Float64Histogram {
	if mb.err != nil {
		return nil
	}
	histogram, err := mb.meter.Float64Histogram(
		name,
		metric.WithDescription(desc),
		metric.WithUnit(unit),
		metric.WithExplicitBucketBoundaries(buckets...),
	)
	if err != nil {
		mb.err = fmt.Errorf("failed to create histogram %s: %w", name, err)
		return nil
	}
	return histogram
}

// Int64Histogram creates an Int64Histogram metric with explicit bucket boundaries
func (mb *metricBuilder) Int64Histogram(name, desc, unit string, buckets []float64) metric.Int64Histogram {
	if mb.err != nil {
		return nil
	}
	histogram, err := mb.meter.Int64Histogram(
		name,
		metric.WithDescription(desc),
		metric.WithUnit(unit),
		metric.WithExplicitBucketBoundaries(buckets...),
	)
	if err != nil {
		mb.err = fmt.Errorf("failed to create int histogram %s: %w", name, err)
		return nil
	}
	return histogram
}
