-- Transactions table (PostgreSQL)
CREATE TABLE transactions (
    transaction_id UUID PRIMARY KEY,
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

