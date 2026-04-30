package adapters

import (
	"context"
)

// Transaction represents the unified canonical data model for a payment
type Transaction struct {
	ID          string                 `json:"transaction_id"`
	AgentID     string                 `json:"agent_id"`
	Amount      float64                `json:"amount"`
	Currency    string                 `json:"currency"`
	Context     map[string]interface{} `json:"context"`
	Constraints []interface{}          `json:"constraints"`
}

// ExecutionResult represents the normalized response from any rail adapter
type ExecutionResult struct {
	TransactionID string                 `json:"transaction_id"`
	ProviderID    string                 `json:"provider_id"`
	Status        string                 `json:"status"` // pending, completed, failed
	Metadata      map[string]interface{} `json:"metadata"`
	Error         error                  `json:"error,omitempty"`
}

// RailCapabilities defines the operational profile of a payment rail
type RailCapabilities struct {
	SupportedCurrencies []string `json:"supported_currencies"`
	TypicalLatency      int      `json:"typical_latency_ms"`
	CostProfile         string   `json:"cost_profile"` // e.g., "flat", "percentage", "hybrid"
	ReliabilityScore    float64  `json:"reliability_score"` // 0.0 to 1.0
}

// RailAdapter defines the interface for different payment rails
type RailAdapter interface {
	// Execute performs the actual payment transaction
	Execute(ctx context.Context, tx Transaction) (*ExecutionResult, error)

	// GetCostEstimate returns the estimated fee for the transaction
	GetCostEstimate(amount float64, context map[string]interface{}) (float64, error)

	// GetCapabilities returns the static and dynamic capabilities of the rail
	GetCapabilities() RailCapabilities

	// HealthCheck returns true if the rail is operational
	HealthCheck(ctx context.Context) bool

	// GetID returns the unique identifier for this adapter
	GetID() string
}
