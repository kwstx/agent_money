package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/galan/agent_money/services/auth-service/internal/models"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// Organization operations
func (r *Repository) CreateOrganization(org *models.Organization) error {
	query := `INSERT INTO organizations (org_id, name, external_id, metadata) VALUES ($1, $2, $3, $4) RETURNING created_at`
	if org.OrgID == uuid.Nil {
		org.OrgID = uuid.New()
	}
	return r.db.QueryRow(query, org.OrgID, org.Name, org.ExternalID, org.Metadata).Scan(&org.CreatedAt)
}

// Agent operations
func (r *Repository) CreateAgent(agent *models.Agent) error {
	query := `INSERT INTO agents (agent_id, org_id, name, public_key, api_key_hash, metadata) VALUES ($1, $2, $3, $4, $5, $6) RETURNING created_at`
	if agent.AgentID == uuid.Nil {
		agent.AgentID = uuid.New()
	}
	return r.db.QueryRow(query, agent.AgentID, agent.OrgID, agent.Name, agent.PublicKey, agent.APIKeyHash, agent.Metadata).Scan(&agent.CreatedAt)
}

// Policy operations with versioning
func (r *Repository) CreatePolicy(policy *models.Policy) error {
	attrJSON, err := json.Marshal(policy.Attributes)
	if err != nil {
		return err
	}

	query := `INSERT INTO policies (policy_id, org_id, name, version, status, roles, attributes, agent_id, workflow_id) 
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING created_at, updated_at`
	
	if policy.PolicyID == uuid.Nil {
		policy.PolicyID = uuid.New()
	}

	return r.db.QueryRow(query, 
		policy.PolicyID, policy.OrgID, policy.Name, policy.Version, policy.Status, 
		pq.Array(policy.Roles), attrJSON, policy.AgentID, policy.WorkflowID,
	).Scan(&policy.CreatedAt, &policy.UpdatedAt)
}

func (r *Repository) GetPolicy(id uuid.UUID) (*models.Policy, error) {
	query := `SELECT policy_id, org_id, name, version, status, roles, attributes, agent_id, workflow_id, created_at, updated_at FROM policies WHERE policy_id = $1`
	
	var p models.Policy
	var attrJSON []byte
	var roles []string
	
	err := r.db.QueryRow(query, id).Scan(
		&p.PolicyID, &p.OrgID, &p.Name, &p.Version, &p.Status, pq.Array(&roles), &attrJSON, &p.AgentID, &p.WorkflowID, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	
	p.Roles = roles
	err = json.Unmarshal(attrJSON, &p.Attributes)
	return &p, err
}

// Approval Workflow
func (r *Repository) ProposePolicyChange(approval *models.PolicyApproval) error {
	query := `INSERT INTO policy_approvals (approval_id, policy_id, proposed_by, proposed_changes, metadata) 
	          VALUES ($1, $2, $3, $4, $5) RETURNING created_at`
	
	if approval.ApprovalID == uuid.Nil {
		approval.ApprovalID = uuid.New()
	}

	return r.db.QueryRow(query, 
		approval.ApprovalID, approval.PolicyID, approval.ProposedBy, approval.ProposedChanges, approval.Metadata,
	).Scan(&approval.CreatedAt)
}

func (r *Repository) DecideApproval(id uuid.UUID, approverID uuid.UUID, status string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Update approval status
	var policyID uuid.UUID
	var proposedChanges []byte
	query := `UPDATE policy_approvals SET status = $1, approver_id = $2, decided_at = CURRENT_TIMESTAMP 
	          WHERE approval_id = $3 AND status = 'pending' RETURNING policy_id, proposed_changes`
	err = tx.QueryRow(query, status, approverID, id).Scan(&policyID, &proposedChanges)
	if err != nil {
		return fmt.Errorf("failed to update approval or not found: %w", err)
	}

	if status == "approved" {
		// 2. Apply changes and increment version
		// This is a simplified version: overwriting current policy or creating a new version record
		// Here we update the existing policy record and increment version
		var currentVersion int
		err = tx.QueryRow(`SELECT version FROM policies WHERE policy_id = $1`, policyID).Scan(&currentVersion)
		if err != nil {
			return err
		}

		var changes map[string]interface{}
		if err := json.Unmarshal(proposedChanges, &changes); err != nil {
			return err
		}

		// Update logic... (simplified for brevity, usually you'd iterate over changes)
		// For now, let's assume 'changes' contains the full attributes or specific fields
		if attrs, ok := changes["attributes"]; ok {
			attrJSON, _ := json.Marshal(attrs)
			_, err = tx.Exec(`UPDATE policies SET attributes = $1, version = $2, updated_at = CURRENT_TIMESTAMP WHERE policy_id = $3`, 
				attrJSON, currentVersion+1, policyID)
			if err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// Querying for Policy Engine (ABAC context)
func (r *Repository) GetEffectivePolicy(agentID uuid.UUID, workflowID *uuid.UUID) (*models.Policy, error) {
	// Priority: Agent-specific policy > Workflow-specific policy > Org-level policy
	// For simplicity, we find the first matching active policy
	query := `
		SELECT policy_id, org_id, name, version, status, roles, attributes, agent_id, workflow_id, created_at, updated_at 
		FROM policies 
		WHERE status = 'active' AND (agent_id = $1 OR workflow_id = $2)
		ORDER BY agent_id NULLS LAST, workflow_id NULLS LAST
		LIMIT 1`
	
	var p models.Policy
	var attrJSON []byte
	var roles []string
	
	err := r.db.QueryRow(query, agentID, workflowID).Scan(
		&p.PolicyID, &p.OrgID, &p.Name, &p.Version, &p.Status, pq.Array(&roles), &attrJSON, &p.AgentID, &p.WorkflowID, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	
	p.Roles = roles
	err = json.Unmarshal(attrJSON, &p.Attributes)
	return &p, err
}
