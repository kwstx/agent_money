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
	Adapters map[string]adapters.RailAdapter
}

func NewRoutingEngine() *RoutingEngine {
	engine := &RoutingEngine{
		Adapters: make(map[string]adapters.RailAdapter),
	}
	// Register default adapters with resilience wrappers
	engine.RegisterAdapter(adapters.NewResilienceWrapper(adapters.NewLightningAdapter()))
	engine.RegisterAdapter(adapters.NewResilienceWrapper(adapters.NewStripeAdapter()))
	engine.RegisterAdapter(adapters.NewResilienceWrapper(adapters.NewStablecoinAdapter("solana")))
	engine.RegisterAdapter(adapters.NewResilienceWrapper(adapters.NewStablecoinAdapter("ethereum")))
	engine.RegisterAdapter(adapters.NewResilienceWrapper(adapters.NewX402Adapter()))
	
	return engine
}

func (e *RoutingEngine) RegisterAdapter(a adapters.RailAdapter) {
	e.Adapters[a.GetID()] = a
}

// ComputePlan selects the best adapter based on policy outputs and metrics
func (e *RoutingEngine) ComputePlan(ctx context.Context, tx adapters.Transaction, policyResults map[string]interface{}) (*ExecutionPlan, error) {
	type scoredAdapter struct {
		id    string
		score float64
	}
	
	var candidates []scoredAdapter
	
	for id, adapter := range e.Adapters {
		if !adapter.HealthCheck() {
			continue
		}

		// Basic scoring logic:
		// Higher score is better.
		// Policy engine might provide multipliers or block certain rails.
		
		cost, _ := adapter.GetCostEstimate(tx.Amount, tx.Context)
		latency := float64(adapter.GetLatencyEstimate())
		
		// Normalized Cost (lower is better, so we use inverse)
		// Normalized Latency (lower is better)
		
		// Simple weights: 60% cost, 40% latency
		// In a real system, these would come from the agent's policy metadata
		costScore := 1.0 / (1.0 + cost)
		latencyScore := 1.0 / (1.0 + (latency / 1000.0))
		
		baseScore := (costScore * 0.6) + (latencyScore * 0.4)
		
		// Apply policy modifiers (e.g., if policy says "avoid-stripe", multiplier = 0.1)
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
		return nil, fmt.Errorf("no healthy adapters available for transaction")
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

	bestAdapter := e.Adapters[best.id]
	cost, _ := bestAdapter.GetCostEstimate(tx.Amount, tx.Context)

	return &ExecutionPlan{
		AdapterID:     best.id,
		Score:         best.score,
		EstimatedCost: cost,
		Latency:       bestAdapter.GetLatencyEstimate(),
		FallbackChain: fallbackChain,
	}, nil
}

func (e *RoutingEngine) Dispatch(ctx context.Context, tx adapters.Transaction, plan *ExecutionPlan) (string, error) {
	adapter, exists := e.Adapters[plan.AdapterID]
	if !exists {
		return "", fmt.Errorf("adapter %s not found", plan.AdapterID)
	}

	log.Printf("[Router] Dispatching tx %s via %s", tx.ID, plan.AdapterID)
	
	result, err := adapter.Execute(ctx, tx)
	if err != nil {
		log.Printf("[Router] Execution failed for %s: %v. Attempting fallbacks...", plan.AdapterID, err)
		
		for _, fallbackID := range plan.FallbackChain {
			fallbackAdapter := e.Adapters[fallbackID]
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
