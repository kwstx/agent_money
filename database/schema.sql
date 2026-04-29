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
CREATE INDEX idx_telemetry_transaction_id ON transaction_telemetry (transaction_id, time DESC);
