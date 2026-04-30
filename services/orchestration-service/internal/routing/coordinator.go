package routing

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/galan/agent_money/services/orchestration-service/internal/adapters"
	"github.com/galan/agent_money/services/orchestration-service/internal/metering"
	"github.com/avast/retry-go/v4"
	"github.com/google/uuid"
)

type AuditLogger interface {
	LogAuditStep(ctx context.Context, txID string, step string, adapterID string, status string, errMsg string, metadata map[string]interface{}) error
}

type SagaCoordinator struct {
	Engine         *RoutingEngine
	AuditLogger    AuditLogger
	Publisher      *metering.EventPublisher
	MaxRetryCounts map[string]int
	GlobalTimeout  time.Duration
}

func NewSagaCoordinator(engine *RoutingEngine, logger AuditLogger, publisher *metering.EventPublisher) *SagaCoordinator {
	return &SagaCoordinator{
		Engine:      engine,
		AuditLogger: logger,
		Publisher:   publisher,
		MaxRetryCounts: map[string]int{
			"lightning":  3,
			"stripe":     1,
			"stablecoin": 2,
			"x402":       2,
		},
		GlobalTimeout: 30 * time.Second,
	}
}

// Orchestrate manages the multi-step saga of a transaction: Policy -> Routing -> Execution -> Metering
func (s *SagaCoordinator) Orchestrate(ctx context.Context, tx adapters.Transaction, mockPolicyFn func(adapters.Transaction) map[string]interface{}) (*adapters.ExecutionResult, error) {
	// Apply Global Timeout
	ctx, cancel := context.WithTimeout(ctx, s.GlobalTimeout)
	defer cancel()

	s.AuditLogger.LogAuditStep(ctx, tx.ID, "start", "", "success", "", nil)

	// 1. Policy Check
	s.AuditLogger.LogAuditStep(ctx, tx.ID, "policy_check", "", "pending", "", nil)
	policyResults := mockPolicyFn(tx)
	if policyResults["decision"] == "REJECT" {
		s.AuditLogger.LogAuditStep(ctx, tx.ID, "policy_check", "", "failure", "rejected by policy", nil)
		return nil, fmt.Errorf("transaction rejected by policy engine")
	}
	s.AuditLogger.LogAuditStep(ctx, tx.ID, "policy_check", "", "success", "", policyResults)

	// 2. Routing
	s.AuditLogger.LogAuditStep(ctx, tx.ID, "routing", "", "pending", "", nil)
	plan, err := s.Engine.ComputePlan(ctx, tx, policyResults)
	if err != nil {
		s.AuditLogger.LogAuditStep(ctx, tx.ID, "routing", "", "failure", err.Error(), nil)
		return nil, err
	}
	s.AuditLogger.LogAuditStep(ctx, tx.ID, "routing", plan.AdapterID, "success", "", map[string]interface{}{
		"fallback_chain": plan.FallbackChain,
		"score":          plan.Score,
	})

	// 3. Execution (with saga fallback and per-rail resilience)
	result, err := s.executeWithFallbacks(ctx, tx, plan)
	if err != nil {
		s.AuditLogger.LogAuditStep(ctx, tx.ID, "final_status", "", "failure", err.Error(), nil)
		return nil, err
	}

	// 4. Metering Update (Asynchronous Event-Driven)
	s.AuditLogger.LogAuditStep(ctx, tx.ID, "metering_update", result.ProviderID, "pending", "", nil)
	
	event := metering.FinancialEvent{
		EventID:       uuid.New().String(),
		TransactionID: tx.ID,
		AgentID:       tx.AgentID, // Assuming Transaction has AgentID
		RailUsed:      result.ProviderID,
		BilledAmount:  tx.Amount,
		Currency:      tx.Currency,
		Status:        "executed",
		Timestamp:     time.Now(),
		ActionDetails: map[string]string{
			"orchestration_status": "success",
		},
	}
	
	// Fire-and-forget publishing
	go func() {
		pubCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Publisher.PublishFinancialEvent(pubCtx, event); err != nil {
			log.Printf("[Saga] Failed to publish metering event for TX %s: %v", tx.ID, err)
		}
	}()

	s.AuditLogger.LogAuditStep(ctx, tx.ID, "metering_update", result.ProviderID, "success", "", nil)
	s.AuditLogger.LogAuditStep(ctx, tx.ID, "final_status", result.ProviderID, "success", "", nil)

	return result, nil
}

func (s *SagaCoordinator) executeWithFallbacks(ctx context.Context, tx adapters.Transaction, plan *ExecutionPlan) (*adapters.ExecutionResult, error) {
	// Try primary rail
	result, err := s.executeWithRetry(ctx, tx, plan.AdapterID)
	if err == nil {
		return result, nil
	}

	log.Printf("[Saga] Primary adapter %s failed: %v. Starting fallback chain...", plan.AdapterID, err)

	// Fallback logic
	for _, fallbackID := range plan.FallbackChain {
		s.AuditLogger.LogAuditStep(ctx, tx.ID, "fallback_attempt", fallbackID, "pending", "", nil)
		
		res, fErr := s.executeWithRetry(ctx, tx, fallbackID)
		if fErr == nil {
			s.AuditLogger.LogAuditStep(ctx, tx.ID, "fallback_attempt", fallbackID, "success", "", nil)
			return res, nil
		}
		
		log.Printf("[Saga] Fallback adapter %s failed: %v", fallbackID, fErr)
		s.AuditLogger.LogAuditStep(ctx, tx.ID, "fallback_attempt", fallbackID, "failure", fErr.Error(), nil)
	}

	return nil, fmt.Errorf("all adapters in execution plan failed")
}

func (s *SagaCoordinator) executeWithRetry(ctx context.Context, tx adapters.Transaction, adapterID string) (*adapters.ExecutionResult, error) {
	adapter, exists := s.Engine.Adapters[adapterID]
	if !exists {
		return nil, fmt.Errorf("adapter %s not found", adapterID)
	}

	maxAttempts := s.MaxRetryCounts[adapterID]
	if maxAttempts == 0 {
		maxAttempts = 1 // Default
	}

	var finalResult *adapters.ExecutionResult
	err := retry.Do(
		func() error {
			s.AuditLogger.LogAuditStep(ctx, tx.ID, "execution_attempt", adapterID, "pending", "", nil)
			
			res, err := adapter.Execute(ctx, tx)
			if err != nil {
				s.AuditLogger.LogAuditStep(ctx, tx.ID, "execution_attempt", adapterID, "retry", err.Error(), nil)
				return err
			}
			
			finalResult = res
			s.AuditLogger.LogAuditStep(ctx, tx.ID, "execution_attempt", adapterID, "success", "", nil)
			return nil
		},
		retry.Context(ctx),
		retry.Attempts(uint(maxAttempts)),
		retry.Delay(1*time.Second),
		retry.OnRetry(func(n uint, err error) {
			log.Printf("[Saga] Retry %d for %s: %v", n, adapterID, err)
		}),
	)

	return finalResult, err
}
