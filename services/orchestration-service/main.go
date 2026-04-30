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
	"github.com/galan/agent_money/pkg/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
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

type WebhookRequest struct {
	TransactionID string                 `json:"transaction_id"`
	ExternalID    string                 `json:"external_id"`
	RailType      string                 `json:"rail_type"`
	Amount        float64                `json:"amount"`
	Currency      string                 `json:"currency"`
	Status        string                 `json:"status"`
	RawData       map[string]interface{} `json:"raw_data"`
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

	// Initialize Telemetry
	telemetry.InitLogger("orchestration-service")
	logger := telemetry.GetLogger()
	defer logger.Sync()

	collectorURL := os.Getenv("OTEL_COLLECTOR_URL")
	if collectorURL == "" {
		collectorURL = "localhost:4317"
	}
	shutdown, err := telemetry.InitTracer(ctx, "orchestration-service", collectorURL)
	if err != nil {
		logger.Fatal("Failed to initialize tracer", zap.Error(err))
	}
	defer shutdown(ctx)

	promExporter, err := telemetry.InitMetrics(ctx, "orchestration-service")
	if err != nil {
		logger.Fatal("Failed to initialize metrics", zap.Error(err))
	}
	if err := telemetry.RegisterMetrics(); err != nil {
		logger.Fatal("Failed to register metrics", zap.Error(err))
	}

	// Initialize Metering Publisher
	kafkaURL := os.Getenv("KAFKA_URL")
	if kafkaURL == "" {
		kafkaURL = "localhost:9092"
	}
	publisher := metering.NewEventPublisher(kafkaURL, "financial-events")

	// Initialize Rail Registry and Discovery
	registry := adapters.InitializeDefaultRegistry(ctx)
	registry.StartDiscovery(ctx, 1*time.Minute)

	// Initialize Routing Engine and Saga Coordinator
	routingEngine = routing.NewRoutingEngine(registry)
	sagaCoordinator = routing.NewSagaCoordinator(routingEngine, repo, publisher)

	// Start HTTP server
	http.HandleFunc("/spend", spendHandler)
	http.HandleFunc("/webhook", webhookHandler)
	http.Handle("/metrics", promExporter)
	
	logger.Info("Orchestration Service (v3 with Saga & Resilience) listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func spendHandler(w http.ResponseWriter, r *http.Request) {
	ctx, span := telemetry.Tracer.Start(r.Context(), "spendHandler")
	defer span.End()

	logger := telemetry.GetLogger()

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	agentID := r.Header.Get("X-Consumer-Username")
	span.SetAttributes(attribute.String("agent_id", agentID))

	var req SpendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Error("Failed to decode request body", zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Metrics: Increment spend requests
	telemetry.SpendRequestsCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("agent_id", agentID)))

	// Idempotency check
	if req.RequestID != "" {
		span.SetAttributes(attribute.String("request_id", req.RequestID))
		val, err := redisClient.Get(ctx, req.RequestID).Result()
		if err == nil {
			logger.Info("Returning idempotent response", zap.String("request_id", req.RequestID))
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(val))
			return
		}
	} else {
		req.RequestID = uuid.New().String()
	}

	// 1. Convert to Internal Transaction
	txID := uuid.New().String()
	span.SetAttributes(attribute.String("transaction_id", txID))
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
	startTime := time.Now()
	result, err := sagaCoordinator.Orchestrate(ctx, tx, mockPolicyCall)
	duration := time.Since(startTime)

	// Metrics: Record policy latency (though Orchestrate does more, it includes policy)
	telemetry.PolicyLatencyHistogram.Record(ctx, duration.Seconds())

	if err != nil {
		logger.Error("Orchestration failed", zap.String("tx_id", txID), zap.Error(err))
		http.Error(w, fmt.Sprintf("Orchestration failed: %v", err), http.StatusServiceUnavailable)
		return
	}

	// 3. Success - Return response
	resp := SpendResponse{
		TransactionID: txID,
		Status:        result.Status,
		Rail:          result.ProviderID,
		Cost:          "estimated",
	}
	
	respJSON, _ := json.Marshal(resp)
	redisClient.Set(ctx, req.RequestID, respJSON, 24*time.Hour)

	logger.Info("Transaction completed successfully", 
		zap.String("tx_id", txID), 
		zap.String("provider_id", result.ProviderID),
		zap.String("status", result.Status),
	)

	// Metrics: Success
	telemetry.RailSuccessCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("rail", result.ProviderID),
		attribute.String("status", result.Status),
	))

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

func webhookHandler(w http.ResponseWriter, r *http.Request) {
	ctx, span := telemetry.Tracer.Start(r.Context(), "webhookHandler")
	defer span.End()

	logger := telemetry.GetLogger()

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req WebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Error("Failed to decode webhook body", zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	err := repo.CreateExternalConfirmation(ctx, req.TransactionID, req.ExternalID, req.RailType, req.Amount, req.Currency, req.Status, req.RawData)
	if err != nil {
		logger.Error("Failed to store external confirmation", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	logger.Info("Received external confirmation", zap.String("external_id", req.ExternalID), zap.String("tx_id", req.TransactionID))
	w.WriteHeader(http.StatusAccepted)
}
