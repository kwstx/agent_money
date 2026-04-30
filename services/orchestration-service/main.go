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
	"github.com/galan/agent_money/services/orchestration-service/internal/metering"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

var (
	redisClient      *redis.Client
	routingEngine    *routing.RoutingEngine
	sagaCoordinator  *routing.SagaCoordinator
	repo             *repository.PostgresRepository
	ctx              = context.Background()
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
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "localhost:6379"
	}
	redisClient = redis.NewClient(&redis.Options{
		Addr: redisURL,
	})

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://admin:password@localhost:5432/agent_money?sslmode=disable"
	}
	var err error
	repo, err = repository.NewPostgresRepository(dbURL)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// Initialize Metering Publisher
	kafkaURL := os.Getenv("KAFKA_URL")
	if kafkaURL == "" {
		kafkaURL = "localhost:9092"
	}
	publisher := metering.NewEventPublisher(kafkaURL, "financial-events")

	// Initialize Routing Engine and Saga Coordinator
	routingEngine = routing.NewRoutingEngine()
	sagaCoordinator = routing.NewSagaCoordinator(routingEngine, repo, publisher)

	// Start HTTP server
	http.HandleFunc("/spend", spendHandler)
	fmt.Println("Orchestration Service (v3 with Saga & Resilience) listening on :8080")
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
		AgentID:     agentID,
		Amount:      req.Amount,
		Currency:    req.Currency,
		Context:     req.Context,
		Constraints: req.Constraints,
	}
	if tx.Context == nil {
		tx.Context = make(map[string]interface{})
	}
	tx.Context["agent_id"] = agentID

	// 2. Orchestrate via Saga Coordinator
	// This handles Policy -> Routing -> Execution (with Fallback & Retries) -> Metering
	result, err := sagaCoordinator.Orchestrate(ctx, tx, mockPolicyCall)
	if err != nil {
		log.Printf("Orchestration failed for tx %s: %v", txID, err)
		http.Error(w, fmt.Sprintf("Orchestration failed: %v", err), http.StatusServiceUnavailable)
		return
	}

	// 3. Success - Return response
	resp := SpendResponse{
		TransactionID: txID,
		Status:        result.Status,
		Rail:          result.ProviderID, // Or the adapter ID if preferred
		Cost:          "estimated",       // In a real system, get actual cost from result
	}
	
	respJSON, _ := json.Marshal(resp)
	redisClient.Set(ctx, req.RequestID, respJSON, 24*time.Hour)

	log.Printf("Transaction %s completed successfully (Provider ID: %s)", txID, result.ProviderID)

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
