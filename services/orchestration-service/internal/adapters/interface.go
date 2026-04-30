package adapters

import (
	"context"
)

// Transaction represents the unified canonical data model for a payment
type Transaction struct {
	ID          string                 `json:"transaction_id"`
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

// RailAdapter defines the interface for different payment rails
type RailAdapter interface {
	// Execute performs the actual payment transaction
	Execute(ctx context.Context, tx Transaction) (*ExecutionResult, error)

	// GetCostEstimate returns the estimated fee for the transaction
	GetCostEstimate(amount float64, context map[string]interface{}) (float64, error)

	// GetLatencyEstimate returns the expected time for completion in milliseconds
	GetLatencyEstimate() int

	// HealthCheck returns the current status of the rail provider
	HealthCheck() bool

	// GetID returns the unique identifier for this adapter
	GetID() string
}
