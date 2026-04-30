package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"github.com/galan/agent_money/pkg/telemetry"
	"go.uber.org/zap"
)

// Simplified struct for demonstration since I haven't run protoc yet
type Transaction struct {
	ID          string                 `json:"transaction_id"`
	Amount      string                 `json:"amount"`
	Currency    string                 `json:"currency"`
	RailType    string                 `json:"rail_type"`
	Context     map[string]interface{} `json:"context"`
	Constraints []interface{}          `json:"constraints"`
	Status      string                 `json:"status"`
	Metadata    map[string]interface{} `json:"metadata"`
	CreatedAt   int64                  `json:"created_at"`
}

func main() {
	ctx := context.Background()
	telemetry.InitLogger("transaction-handler")
	logger := telemetry.GetLogger()
	defer logger.Sync()

	collectorURL := os.Getenv("OTEL_COLLECTOR_URL")
	if collectorURL == "" {
		collectorURL = "localhost:4317"
	}
	shutdown, err := telemetry.InitTracer(ctx, "transaction-handler", collectorURL)
	if err != nil {
		logger.Fatal("Failed to initialize tracer", zap.Error(err))
	}
	defer shutdown(ctx)

	kafkaURL := os.Getenv("KAFKA_URL")
	if kafkaURL == "" {
		kafkaURL = "localhost:9092"
	}

	writer := &kafka.Writer{
		Addr:     kafka.TCP(kafkaURL),
		Topic:    "transaction-events",
		Balancer: &kafka.LeastBytes{},
	}
	defer writer.Close()

	http.HandleFunc("/transactions", func(w http.ResponseWriter, r *http.Request) {
		ctx, span := telemetry.Tracer.Start(r.Context(), "handleTransaction")
		defer span.End()

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var tx Transaction
		if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
			logger.Error("Failed to decode transaction", zap.Error(err))
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		tx.ID = uuid.New().String()
		tx.Status = "PENDING"
		tx.CreatedAt = time.Now().Unix()

		logger.Info("Received transaction", zap.String("tx_id", tx.ID), zap.String("rail", tx.RailType))

		payload, _ := json.Marshal(tx)
		err := writer.WriteMessages(ctx,
			kafka.Message{
				Key:   []byte(tx.ID),
				Value: payload,
			},
		)

		if err != nil {
			logger.Error("Failed to write to Kafka", zap.Error(err), zap.String("tx_id", tx.ID))
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tx)
	})

	fmt.Println("Transaction Handler listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
