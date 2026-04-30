package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

var Meter metric.Meter

func InitMetrics(ctx context.Context, serviceName string) (*prometheus.Exporter, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	exporter, err := prometheus.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus exporter: %w", err)
	}

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(exporter),
	)
	
	Meter = provider.Meter(serviceName)

	return exporter, nil
}

// Key metrics
var (
	SpendRequestsCounter   metric.Int64Counter
	PolicyLatencyHistogram metric.Float64Histogram
	RailSuccessCounter     metric.Int64Counter
	BudgetUtilizationGauge metric.Float64ObservableGauge
)

func RegisterMetrics() error {
	var err error
	SpendRequestsCounter, err = Meter.Int64Counter("spend_requests_total",
		metric.WithDescription("Total number of spend requests"),
	)
	if err != nil {
		return err
	}

	PolicyLatencyHistogram, err = Meter.Float64Histogram("policy_evaluation_latency_seconds",
		metric.WithDescription("Latency of policy evaluation"),
	)
	if err != nil {
		return err
	}

	RailSuccessCounter, err = Meter.Int64Counter("rail_execution_total",
		metric.WithDescription("Total number of rail executions"),
	)
	if err != nil {
		return err
	}

	return nil
}
