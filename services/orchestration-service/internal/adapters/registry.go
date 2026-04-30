package adapters

import (
	"context"
	"log"
	"sync"
	"time"
)

// RailRegistry manages the dynamic collection of payment rail adapters
type RailRegistry struct {
	mu       sync.RWMutex
	adapters map[string]RailAdapter
	scores   map[string]float64
}

func NewRailRegistry() *RailRegistry {
	r := &RailRegistry{
		adapters: make(map[string]RailAdapter),
		scores:   make(map[string]float64),
	}
	return r
}

// Register adds a new adapter to the registry
func (r *RailRegistry) Register(adapter RailAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[adapter.GetID()] = adapter
	r.scores[adapter.GetID()] = 1.0 // Initial score
	log.Printf("[Registry] Registered adapter: %s", adapter.GetID())
}

// GetAdapter returns an adapter by ID
func (r *RailRegistry) GetAdapter(id string) (RailAdapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[id]
	return a, ok
}

// ListAdapters returns all registered adapters
func (r *RailRegistry) ListAdapters() []RailAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var list []RailAdapter
	for _, a := range r.adapters {
		list = append(list, a)
	}
	return list
}

// StartDiscovery launches a background process to test adapter health and update scores
func (r *RailRegistry) StartDiscovery(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.discover()
			}
		}
	}()
}

func (r *RailRegistry) discover() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for id, adapter := range r.adapters {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		healthy := adapter.HealthCheck(ctx)
		cancel()

		// Update reliability score based on health
		// Simple logic: if healthy, slightly increase score; if unhealthy, drop it significantly
		if healthy {
			r.scores[id] = r.scores[id]*0.9 + 0.1 // Decay towards 1.0
		} else {
			r.scores[id] = r.scores[id] * 0.5 // Penalty
			log.Printf("[Discovery] Adapter %s health check failed", id)
		}
	}
}

// GetAvailabilityScore returns the dynamic health-adjusted score for an adapter
func (r *RailRegistry) GetAvailabilityScore(id string) float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.scores[id]
}
