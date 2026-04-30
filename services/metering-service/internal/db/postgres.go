package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

type PostgresRepo struct {
	db *sql.DB
}

func NewPostgresRepo(connStr string) (*PostgresRepo, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}
	return &PostgresRepo{db: db}, nil
}

func (r *PostgresRepo) UpdateBudgetAndLedger(ctx context.Context, agentID string, txID string, amount float64, currency string, rail string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Update Aggregated Budget
	upsertBudget := `
		INSERT INTO budgets (agent_id, currency, total_spent, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (agent_id, currency)
		DO UPDATE SET 
			total_spent = budgets.total_spent + EXCLUDED.total_spent,
			updated_at = EXCLUDED.updated_at
	`
	_, err = tx.ExecContext(ctx, upsertBudget, agentID, currency, amount, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update budget: %w", err)
	}

	// 2. Double-Entry Ledger: Debit Agent Wallet
	insertDebit := `
		INSERT INTO ledger_entries (transaction_id, account_id, amount, entry_type)
		VALUES ($1, $2, $3, $4)
	`
	_, err = tx.ExecContext(ctx, insertDebit, txID, fmt.Sprintf("agent:%s", agentID), -amount, "settlement")
	if err != nil {
		return fmt.Errorf("failed to insert debit entry: %w", err)
	}

	// 3. Double-Entry Ledger: Credit Rail Liquidity
	insertCredit := `
		INSERT INTO ledger_entries (transaction_id, account_id, amount, entry_type)
		VALUES ($1, $2, $3, $4)
	`
	_, err = tx.ExecContext(ctx, insertCredit, txID, fmt.Sprintf("rail:%s", rail), amount, "settlement")
	if err != nil {
		return fmt.Errorf("failed to insert credit entry: %w", err)
	}

	return tx.Commit()
}

func (r *PostgresRepo) AdjustLedger(ctx context.Context, txID string, accountID string, amount float64, entryType string) error {
	query := `
		INSERT INTO ledger_entries (transaction_id, account_id, amount, entry_type)
		VALUES ($1, $2, $3, $4)
	`
	_, err := r.db.ExecContext(ctx, query, txID, accountID, amount, entryType)
	return err
}
