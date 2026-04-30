package routing

import (
	"context"
	"fmt"
	"log"
	"sort"

	"github.com/galan/agent_money/services/orchestration-service/internal/adapters"
)

type ExecutionPlan struct {
	AdapterID      string
	Score          float64
	EstimatedCost  float64
	Latency        int
	FallbackChain  []string
}

type RoutingEngine struct {
	Registry *adapters.RailRegistry
}

func NewRoutingEngine(registry *adapters.RailRegistry) *RoutingEngine {
	return &RoutingEngine{
		Registry: registry,
	}
}

// ComputePlan selects the best adapter based on policy outputs, static capabilities, and dynamic metrics
func (e *RoutingEngine) ComputePlan(ctx context.Context, tx adapters.Transaction, policyResults map[string]interface{}) (*ExecutionPlan, error) {
	type scoredAdapter struct {
		id    string
		score float64
	}
	
	var candidates []scoredAdapter
	
	allAdapters := e.Registry.ListAdapters()
	
	for _, adapter := range allAdapters {
		id := adapter.GetID()
		if !adapter.HealthCheck(ctx) {
			continue
		}

		caps := adapter.GetCapabilities()
		availabilityScore := e.Registry.GetAvailabilityScore(id)
		
		// Check if currency is supported
		supported := false
		for _, c := range caps.SupportedCurrencies {
			if c == tx.Currency {
				supported = true
				break
			}
		}
		if !supported {
			continue
		}

		cost, _ := adapter.GetCostEstimate(tx.Amount, tx.Context)
		latency := float64(caps.TypicalLatency)
		
		// Normalized Cost (lower is better)
		// Normalized Latency (lower is better)
		
		// Scoring weights: 40% cost, 30% latency, 30% reliability/availability
		costScore := 1.0 / (1.0 + cost)
		latencyScore := 1.0 / (1.0 + (latency / 1000.0))
		
		// Combine static reliability and dynamic availability
		reliabilityScore := (caps.ReliabilityScore * 0.5) + (availabilityScore * 0.5)
		
		baseScore := (costScore * 0.4) + (latencyScore * 0.3) + (reliabilityScore * 0.3)
		
		// Apply policy modifiers
		multiplier := 1.0
		if mod, ok := policyResults["rail_modifiers"].(map[string]interface{}); ok {
			if m, exists := mod[id].(float64); exists {
				multiplier = m
			}
		}
		
		candidates = append(candidates, scoredAdapter{
			id:    id,
			score: baseScore * multiplier,
		})
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no healthy/supported adapters available for transaction")
	}

	// Sort candidates by score descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	best := candidates[0]
	var fallbackChain []string
	for i := 1; i < len(candidates); i++ {
		fallbackChain = append(fallbackChain, candidates[i].id)
	}

	bestAdapter, _ := e.Registry.GetAdapter(best.id)
	cost, _ := bestAdapter.GetCostEstimate(tx.Amount, tx.Context)
	caps := bestAdapter.GetCapabilities()

	return &ExecutionPlan{
		AdapterID:     best.id,
		Score:         best.score,
		EstimatedCost: cost,
		Latency:       caps.TypicalLatency,
		FallbackChain: fallbackChain,
	}, nil
}

func (e *RoutingEngine) Dispatch(ctx context.Context, tx adapters.Transaction, plan *ExecutionPlan) (string, error) {
	adapter, exists := e.Registry.GetAdapter(plan.AdapterID)
	if !exists {
		return "", fmt.Errorf("adapter %s not found", plan.AdapterID)
	}

	log.Printf("[Router] Dispatching tx %s via %s", tx.ID, plan.AdapterID)
	
	result, err := adapter.Execute(ctx, tx)
	if err != nil {
		log.Printf("[Router] Execution failed for %s: %v. Attempting fallbacks...", plan.AdapterID, err)
		
		for _, fallbackID := range plan.FallbackChain {
			fallbackAdapter, ok := e.Registry.GetAdapter(fallbackID)
			if !ok {
				continue
			}
			log.Printf("[Router] Trying fallback: %s", fallbackID)
			res, fErr := fallbackAdapter.Execute(ctx, tx)
			if fErr == nil {
				return res.ProviderID, nil
			}
			log.Printf("[Router] Fallback %s failed: %v", fallbackID, fErr)
		}
		return "", fmt.Errorf("all adapters in execution plan failed: last error: %v", err)
	}

	return result.ProviderID, nil
}
