package adapters

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/sony/gobreaker"
)

// ResilienceWrapper wraps a RailAdapter with retries and circuit breaking
type ResilienceWrapper struct {
	adapter RailAdapter
	cb      *gobreaker.CircuitBreaker
}

func NewResilienceWrapper(adapter RailAdapter) *ResilienceWrapper {
	st := gobreaker.Settings{
		Name:        adapter.GetID(),
		MaxRequests: 3,
		Interval:    5 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 5 && failureRatio >= 0.6
		},
	}

	return &ResilienceWrapper{
		adapter: adapter,
		cb:      gobreaker.NewCircuitBreaker(st),
	}
}

func (w *ResilienceWrapper) Execute(ctx context.Context, tx Transaction) (*ExecutionResult, error) {
	// Execute through circuit breaker
	res, err := w.cb.Execute(func() (interface{}, error) {
		return w.adapter.Execute(ctx, tx)
	})

	if err != nil {
		return nil, err
	}

	return res.(*ExecutionResult), nil
}

// Delegate other methods to the underlying adapter
func (w *ResilienceWrapper) GetCostEstimate(amount float64, context map[string]interface{}) (float64, error) {
	return w.adapter.GetCostEstimate(amount, context)
}

func (w *ResilienceWrapper) GetLatencyEstimate() int {
	return w.adapter.GetLatencyEstimate()
}

func (w *ResilienceWrapper) HealthCheck() bool {
	return w.adapter.HealthCheck()
}

func (w *ResilienceWrapper) GetID() string {
	return w.adapter.GetID()
}
