package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/galan/agent_money/services/orchestration-service/internal/routing"
	_ "github.com/lib/pq"
)

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(connStr string) (*PostgresRepository, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}

	// Basic connection pooling
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	return &PostgresRepository{db: db}, nil
}

func (r *PostgresRepository) CreateExecutionPlan(ctx context.Context, txID string, plan *routing.ExecutionPlan) (string, error) {
	fallbackJSON, _ := json.Marshal(plan.FallbackChain)
	
	var planID string
	query := `
		INSERT INTO execution_plans (
			transaction_id, adapter_id, score, estimated_cost, estimated_latency, fallback_chain, status
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING plan_id
	`
	
	err := r.db.QueryRowContext(ctx, query,
		txID,
		plan.AdapterID,
		plan.Score,
		plan.EstimatedCost,
		plan.Latency,
		fallbackJSON,
		"pending",
	).Scan(&planID)
	
	if err != nil {
		return "", fmt.Errorf("failed to persist execution plan: %v", err)
	}
	
	return planID, nil
}

func (r *PostgresRepository) UpdatePlanStatus(ctx context.Context, planID string, status string) error {
	query := `UPDATE execution_plans SET status = $1 WHERE plan_id = $2`
	_, err := r.db.ExecContext(ctx, query, status, planID)
	return err
}
