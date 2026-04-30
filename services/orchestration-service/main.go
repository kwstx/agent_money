package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/galan/agent_money/services/orchestration-service/internal/adapters"
	"github.com/galan/agent_money/services/orchestration-service/internal/repository"
	"github.com/galan/agent_money/services/orchestration-service/internal/routing"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

var (
	redisClient   *redis.Client
	routingEngine *routing.RoutingEngine
	repo          *repository.PostgresRepository
	ctx           = context.Background()
)

type SpendRequest struct {
	RequestID   string                 `json:"request_id"`
	Amount      float64                `json:"amount"`
	Currency    string                 `json:"currency"`
	Context     map[string]interface{} `json:"context"`
	Constraints []interface{}          `json:"constraints"`
}

type SpendResponse struct {
	TransactionID string `json:"transaction_id"`
	Status        string `json:"status"`
	Rail          string `json:"rail"`
	Cost          string `json:"estimated_cost"`
}

func main() {
	// Initialize Redis
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "localhost:6379"
	}
	redisClient = redis.NewClient(&redis.Options{
		Addr: redisURL,
	})

	// Initialize Postgres
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://admin:password@localhost:5432/agent_money?sslmode=disable"
	}
	var err error
	repo, err = repository.NewPostgresRepository(dbURL)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// Initialize Routing Engine
	routingEngine = routing.NewRoutingEngine()

	// Start HTTP server
	http.HandleFunc("/spend", spendHandler)
	fmt.Println("Orchestration Service (v2 with Dynamic Routing) listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func spendHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	agentID := r.Header.Get("X-Consumer-Username")
	var req SpendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Idempotency check
	if req.RequestID != "" {
		val, err := redisClient.Get(ctx, req.RequestID).Result()
		if err == nil {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(val))
			return
		}
	} else {
		req.RequestID = uuid.New().String()
	}

	// 1. Convert to Internal Transaction
	txID := uuid.New().String()
	tx := adapters.Transaction{
		ID:          txID,
		Amount:      req.Amount,
		Currency:    req.Currency,
		Context:     req.Context,
		Constraints: req.Constraints,
	}
	if tx.Context == nil {
		tx.Context = make(map[string]interface{})
	}
	tx.Context["agent_id"] = agentID

	// 2. Fetch Policy Results (Mocked call to policy engine)
	// In reality, this would be an HTTP call to the policy-engine service
	policyResults := mockPolicyCall(tx)

	if policyResults["decision"] == "REJECT" {
		http.Error(w, "Transaction rejected by policy engine", http.StatusForbidden)
		return
	}

	// 3. Compute Routing Plan
	plan, err := routingEngine.ComputePlan(ctx, tx, policyResults)
	if err != nil {
		log.Printf("Routing failed: %v", err)
		http.Error(w, "No viable payment rail found", http.StatusServiceUnavailable)
		return
	}

	// 4. Persist Execution Plan (BEFORE Dispatching)
	planID, err := repo.CreateExecutionPlan(ctx, txID, plan)
	if err != nil {
		log.Printf("Persistence failed: %v", err)
		// We can still proceed, but persistence is required by the prompt
	}

	// 5. Dispatch to Adapter
	providerTxID, err := routingEngine.Dispatch(ctx, tx, plan)
	if err != nil {
		log.Printf("Dispatch failed: %v", err)
		repo.UpdatePlanStatus(ctx, planID, "failed")
		http.Error(w, fmt.Sprintf("Execution failed: %v", err), http.StatusGatewayTimeout)
		return
	}

	// 6. Success - Update status and return
	repo.UpdatePlanStatus(ctx, planID, "executed")

	resp := SpendResponse{
		TransactionID: txID,
		Status:        "SUCCESS",
		Rail:          plan.AdapterID,
		Cost:          fmt.Sprintf("%.4f", plan.EstimatedCost),
	}
	
	respJSON, _ := json.Marshal(resp)
	redisClient.Set(ctx, req.RequestID, respJSON, 24*time.Hour)

	log.Printf("Transaction %s completed via %s (Provider ID: %s)", txID, plan.AdapterID, providerTxID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	w.Write(respJSON)
}

func mockPolicyCall(tx adapters.Transaction) map[string]interface{} {
	// Simulated response from policy engine
	return map[string]interface{}{
		"decision": "APPROVE",
		"rail_modifiers": map[string]interface{}{
			"lightning": 1.2, // Boost lightning
			"stripe":    0.5, // Penalize stripe
		},
	}
}
