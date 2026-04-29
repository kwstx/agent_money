# Agent Money Public API

This layer provides a unified REST and gRPC interface for agent expenditures.

## Architecture

1.  **Kong API Gateway**: Handles JWT authentication, rate limiting, and routing.
2.  **Orchestration Service**: Handles idempotency (via Redis), validation, and forwards requests to the transaction handler.
3.  **Redis**: Stores idempotency keys (`request_id`) for 24-48 hours.

## Endpoints

### REST API
- **Endpoint**: `POST /spend`
- **Authentication**: JWT (Bearer Token)
- **Rate Limit**: 100 requests per minute (per agent)

**Request Body:**
```json
{
  "request_id": "unique-id-123",
  "amount": 10.50,
  "currency": "USD",
  "context": {
    "action": "web_search",
    "agent_id": "agent-001"
  },
  "constraints": [
    {
      "type": "budget",
      "limit": 100.0
    }
  ]
}
```

**Response:**
```json
{
  "transaction_id": "uuid-...",
  "status": "ACCEPTED",
  "estimated_cost": "0.01"
}
```

### gRPC API
- **Service**: `agent_money.v1.TransactionService`
- **Method**: `Spend`
- **Port**: `9090` (internal), exposed via Kong on `8000` (grpc) or `8443` (grpcs).

## Authentication

The API is secured with JWT. To generate a test token:
- **Issuer (`iss`)**: `agent-money-issuer`
- **Secret**: `a36c30a0-d461-4b74-9d4a-384357a7d4c1` (configured in `kong/kong.yml`)

## Idempotency

Provide a unique `request_id` in the JSON body. The orchestration service will cache the response in Redis for 24 hours. Subsequent requests with the same `request_id` will return the cached response.
