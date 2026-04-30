-- Transactions table (PostgreSQL)
CREATE TABLE transactions (
    transaction_id UUID PRIMARY KEY,
    agent_id UUID NOT NULL,
    amount DECIMAL NOT NULL,
    currency VARCHAR(3) NOT NULL,
    rail_type VARCHAR(20) NOT NULL,
    context JSONB,
    constraints JSONB[],
    status VARCHAR(20) NOT NULL,
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Financial Telemetry table (TimescaleDB)
CREATE TABLE transaction_telemetry (
    time TIMESTAMP WITH TIME ZONE NOT NULL,
    transaction_id UUID NOT NULL,
    rail_type VARCHAR(20),
    amount DECIMAL,
    currency VARCHAR(3),
    status VARCHAR(20),
    latency_ms INTEGER
);

-- Convert to hypertable for TimescaleDB
SELECT create_hypertable('transaction_telemetry', 'time');

-- Index for transaction_id on telemetry
-- Rules table for Policy Engine
CREATE TABLE rules (
    rule_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    priority INTEGER NOT NULL,
    conditions JSONB NOT NULL, -- JSON array of expressions
    actions JSONB NOT NULL,    -- e.g., {"type": "approve", "rails": ["lightning", "stripe"]}
    parameters JSONB,          -- e.g., {"budget_remaining": 100.0, "risk_score_threshold": 0.1}
    active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Index for priority evaluation
CREATE INDEX idx_rules_priority ON rules (priority ASC) WHERE active = TRUE;

-- Execution Plans table for Dynamic Routing
CREATE TABLE execution_plans (
    plan_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id UUID NOT NULL REFERENCES transactions(transaction_id),
    adapter_id VARCHAR(50) NOT NULL,
    score FLOAT NOT NULL,
    estimated_cost DECIMAL,
    estimated_latency INTEGER, -- in ms
    fallback_chain JSONB,       -- Ordered list of adapter_ids to try if this one fails
    status VARCHAR(20) NOT NULL DEFAULT 'pending', -- 'pending', 'executed', 'failed'
    policy_metadata JSONB,      -- Snapshots of policy engine output that led to this decision
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_execution_plans_transaction_id ON execution_plans(transaction_id);

-- Immutable Audit Trail for Resilience and Compliance
CREATE TABLE audit_trail (
    audit_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id UUID NOT NULL REFERENCES transactions(transaction_id),
    step VARCHAR(50) NOT NULL, -- e.g., 'policy_check', 'routing', 'execution_attempt', 'metering_update'
    adapter_id VARCHAR(50),    -- optional, for execution steps
    status VARCHAR(20) NOT NULL, -- 'success', 'failure', 'retry'
    error_message TEXT,
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_audit_trail_transaction_id ON audit_trail(transaction_id);

-- Enforce Immutability on Audit Trail
CREATE OR REPLACE FUNCTION block_immutable_changes()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'Updates and Deletes are not allowed on this immutable table.';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_audit_trail_immutable_update
BEFORE UPDATE ON audit_trail
FOR EACH ROW EXECUTE FUNCTION block_immutable_changes();

CREATE TRIGGER trg_audit_trail_immutable_delete
BEFORE DELETE ON audit_trail
FOR EACH ROW EXECUTE FUNCTION block_immutable_changes();

-- Budgets table for historical tracking and enforcement
CREATE TABLE budgets (
    agent_id UUID NOT NULL,
    currency VARCHAR(3) NOT NULL,
    total_spent DECIMAL DEFAULT 0,
    daily_limit DECIMAL,
    monthly_limit DECIMAL,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (agent_id, currency)
);

-- Ledger entries for double-entry accounting
-- Every transaction should have at least two entries: a debit and a credit.
CREATE TABLE ledger_entries (
    entry_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id UUID NOT NULL REFERENCES transactions(transaction_id),
    account_id VARCHAR(100) NOT NULL, -- e.g., 'agent_wallet', 'platform_revenue', 'rail_liquidity'
    amount DECIMAL NOT NULL, -- Positive for credit, negative for debit
    entry_type VARCHAR(20) NOT NULL, -- 'settlement', 'fee', 'refund'
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_ledger_transaction_id ON ledger_entries(transaction_id);
CREATE INDEX idx_ledger_account_id ON ledger_entries(account_id);

-- Authorization and Permissions System

-- Organizations table for multi-tenancy and external IDP integration
CREATE TABLE organizations (
    org_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    external_id VARCHAR(255) UNIQUE, -- For OIDC/OAuth provider link
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Agents table with public key and API key authentication
CREATE TABLE agents (
    agent_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(org_id),
    name VARCHAR(255) NOT NULL,
    public_key TEXT,
    api_key_hash TEXT,
    status VARCHAR(20) DEFAULT 'active', -- 'active', 'suspended', 'deactivated'
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Workflows represent specific operational sequences that can have specific policies
CREATE TABLE workflows (
    workflow_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(org_id),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Permissions policies combining RBAC and ABAC
CREATE TABLE policies (
    policy_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(org_id),
    name VARCHAR(255) NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    status VARCHAR(20) NOT NULL DEFAULT 'active', -- 'active', 'pending_approval', 'deprecated'
    
    -- RBAC: Roles associated with this policy
    roles TEXT[] DEFAULT '{}', -- e.g., ['manager', 'executor']
    
    -- ABAC: Attribute-based constraints
    attributes JSONB NOT NULL DEFAULT '{
        "daily_budget": 0.0,
        "task_cost_ceiling": 0.0,
        "allowed_context_patterns": [],
        "time_windows": []
    }',
    
    -- Links: Policy can be applied to an agent or a specific workflow
    agent_id UUID REFERENCES agents(agent_id),
    workflow_id UUID REFERENCES workflows(workflow_id),
    
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_policies_org_id ON policies(org_id);
CREATE INDEX idx_policies_agent_id ON policies(agent_id);
CREATE INDEX idx_policies_workflow_id ON policies(workflow_id);

-- Policy Approval Workflows for versioning and high-privilege changes
CREATE TABLE policy_approvals (
    approval_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_id UUID REFERENCES policies(policy_id),
    proposed_by UUID NOT NULL, -- User/Admin ID
    approver_id UUID,
    status VARCHAR(20) DEFAULT 'pending', -- 'pending', 'approved', 'rejected'
    proposed_changes JSONB NOT NULL,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    decided_at TIMESTAMP WITH TIME ZONE
);
