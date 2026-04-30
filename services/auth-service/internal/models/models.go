package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Organization struct {
	OrgID      uuid.UUID       `json:"org_id"`
	Name       string          `json:"name"`
	ExternalID string          `json:"external_id"`
	Metadata   json.RawMessage `json:"metadata"`
	CreatedAt  time.Time       `json:"created_at"`
}

type Agent struct {
	AgentID    uuid.UUID       `json:"agent_id"`
	OrgID      uuid.UUID       `json:"org_id"`
	Name       string          `json:"name"`
	PublicKey  string          `json:"public_key"`
	APIKeyHash string          `json:"api_key_hash"`
	Status     string          `json:"status"`
	Metadata   json.RawMessage `json:"metadata"`
	CreatedAt  time.Time       `json:"created_at"`
}

type Workflow struct {
	WorkflowID uuid.UUID `json:"workflow_id"`
	OrgID      uuid.UUID `json:"org_id"`
	Name       string    `json:"name"`
	Description string   `json:"description"`
	CreatedAt  time.Time `json:"created_at"`
}

type PolicyAttributes struct {
	DailyBudget           decimal.Decimal `json:"daily_budget"`
	TaskCostCeiling       decimal.Decimal `json:"task_cost_ceiling"`
	AllowedContextPatterns []string        `json:"allowed_context_patterns"`
	TimeWindows           []string        `json:"time_windows"`
}

type Policy struct {
	PolicyID   uuid.UUID        `json:"policy_id"`
	OrgID      uuid.UUID        `json:"org_id"`
	Name       string           `json:"name"`
	Version    int              `json:"version"`
	Status     string           `json:"status"`
	Roles      []string         `json:"roles"`
	Attributes PolicyAttributes `json:"attributes"`
	AgentID    *uuid.UUID       `json:"agent_id,omitempty"`
	WorkflowID *uuid.UUID       `json:"workflow_id,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

type PolicyApproval struct {
	ApprovalID      uuid.UUID       `json:"approval_id"`
	PolicyID        uuid.UUID       `json:"policy_id"`
	ProposedBy      uuid.UUID       `json:"proposed_by"`
	ApproverID      *uuid.UUID      `json:"approver_id,omitempty"`
	Status          string          `json:"status"`
	ProposedChanges json.RawMessage `json:"proposed_changes"`
	Metadata        json.RawMessage `json:"metadata"`
	CreatedAt       time.Time       `json:"created_at"`
	DecidedAt       *time.Time      `json:"decided_at,omitempty"`
}
