package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	// "github.com/galan/agent_money/proto/v1" // Assuming generated code
)

var (
	redisClient *redis.Client
	ctx         = context.Background()
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
	EstimatedCost string `json:"estimated_cost"`
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

	// Start gRPC server
	go startGRPC()

	// Start HTTP server
	http.HandleFunc("/spend", spendHandler)
	fmt.Println("Orchestration Service listening on :8080 (HTTP) and :9090 (gRPC)")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func spendHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract agent identity from Kong headers (optional but good for context)
	agentID := r.Header.Get("X-Consumer-Username")
	log.Printf("Processing spend request for agent: %s", agentID)

	var req SpendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Idempotency check
	if req.RequestID != "" {
		val, err := redisClient.Get(ctx, req.RequestID).Result()
		if err == nil {
			// Found in cache, return previous response
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(val))
			return
		}
	} else {
		// Generate one if missing (though client should provide it)
		req.RequestID = uuid.New().String()
	}

	// Validation
	if req.Amount <= 0 {
		http.Error(w, "Amount must be positive", http.StatusBadRequest)
		return
	}

	// Forward to transaction handler
	txHandlerURL := os.Getenv("TRANSACTION_HANDLER_URL")
	if txHandlerURL == "" {
		txHandlerURL = "http://localhost:8081"
	}

	resp, err := forwardToTransactionHandler(txHandlerURL, req)
	if err != nil {
		log.Printf("Error forwarding to transaction handler: %v", err)
		http.Error(w, "Failed to process transaction", http.StatusInternalServerError)
		return
	}

	// Store in Redis for idempotency (24 hours)
	respJSON, _ := json.Marshal(resp)
	redisClient.Set(ctx, req.RequestID, respJSON, 24*time.Hour)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	w.Write(respJSON)
}

func forwardToTransactionHandler(url string, req SpendRequest) (*SpendResponse, error) {
	// Map SpendRequest to Transaction Handler format
	payload, _ := json.Marshal(map[string]interface{}{
		"amount":      fmt.Sprintf("%.2f", req.Amount),
		"currency":    req.Currency,
		"context":     req.Context,
		"constraints": req.Constraints,
	})

	resp, err := http.Post(url+"/transactions", "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("transaction handler returned status %d", resp.StatusCode)
	}

	var result struct {
		ID     string `json:"transaction_id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &SpendResponse{
		TransactionID: result.ID,
		Status:        result.Status,
		EstimatedCost: "0.01", // Mocked for now
	}, nil
}

// Server is used to implement TransactionService
type server struct {
	// pb.UnimplementedTransactionServiceServer
}

func (s *server) Spend(ctx context.Context, in *SpendRequest) (*SpendResponse, error) {
	// Idempotency check (simplified for gRPC)
	// In a real app, we'd extract a RequestID from metadata or the request itself
	
	// Forward to transaction handler
	txHandlerURL := os.Getenv("TRANSACTION_HANDLER_URL")
	if txHandlerURL == "" {
		txHandlerURL = "http://localhost:8081"
	}

	resp, err := forwardToTransactionHandler(txHandlerURL, *in)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func startGRPC() {
	lis, err := net.Listen("tcp", ":9090")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	// pb.RegisterTransactionServiceServer(s, &server{}) 
	fmt.Println("gRPC server listening on :9090")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
