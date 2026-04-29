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
	// "github.com/galan/agent_money/proto/v1" // Assuming generated code
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
	writer := &kafka.Writer{
		Addr:     kafka.TCP("localhost:9092"),
		Topic:    "transaction-events",
		Balancer: &kafka.LeastBytes{},
	}
	defer writer.Close()

	http.HandleFunc("/transactions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var tx Transaction
		if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		tx.ID = uuid.New().String()
		tx.Status = "PENDING"
		tx.CreatedAt = time.Now().Unix()

		payload, _ := json.Marshal(tx)
		err := writer.WriteMessages(context.Background(),
			kafka.Message{
				Key:   []byte(tx.ID),
				Value: payload,
			},
		)

		if err != nil {
			log.Printf("failed to write messages: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tx)
	})

	fmt.Println("Transaction Handler listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
