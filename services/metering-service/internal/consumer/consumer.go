package consumer

import (
	"context"
	"encoding/json"
	"log"

	"github.com/galan/agent_money/services/metering-service/internal/db"
	"github.com/segmentio/kafka-go"
)

type FinancialEvent struct {
	EventID       string            `json:"event_id"`
	TransactionID string            `json:"transaction_id"`
	AgentID       string            `json:"agent_id"`
	RailUsed      string            `json:"rail_used"`
	BilledAmount  float64           `json:"billed_amount"`
	Currency      string            `json:"currency"`
	Status        string            `json:"status"`
	ActionDetails map[string]string `json:"action_details"`
}

type MeteringConsumer struct {
	reader   *kafka.Reader
	postgres *db.PostgresRepo
	redis    *db.RedisRepo
}

func NewMeteringConsumer(brokerURL string, topic string, pg *db.PostgresRepo, rd *db.RedisRepo) *MeteringConsumer {
	return &MeteringConsumer{
		reader: kafka.NewReader(kafka.ReaderConfig{
			Brokers:  []string{brokerURL},
			Topic:    topic,
			GroupID:  "metering-service-group",
			MinBytes: 10e3, // 10KB
			MaxBytes: 10e6, // 10MB
		}),
		postgres: pg,
		redis:    rd,
	}
}

func (c *MeteringConsumer) Start(ctx context.Context) {
	log.Println("[Metering Consumer] Starting ingestion loop...")
	for {
		m, err := c.reader.ReadMessage(ctx)
		if err != nil {
			log.Printf("[Metering Consumer] Error reading message: %v", err)
			break
		}

		var event FinancialEvent
		if err := json.Unmarshal(m.Value, &event); err != nil {
			log.Printf("[Metering Consumer] Failed to unmarshal event: %v", err)
			continue
		}

		log.Printf("[Metering Consumer] Ingested event: TX %s, Amount %f %s", event.TransactionID, event.BilledAmount, event.Currency)

		// 1. Update Redis (Real-time checks)
		if err := c.redis.UpdateRealtimeBudget(ctx, event.AgentID, event.BilledAmount, event.Currency); err != nil {
			log.Printf("[Metering Consumer] Redis update failed for TX %s: %v", event.TransactionID, err)
		}

		// 2. Update PostgreSQL (Historical & Ledger)
		if err := c.postgres.UpdateBudgetAndLedger(ctx, event.AgentID, event.TransactionID, event.BilledAmount, event.Currency, event.RailUsed); err != nil {
			log.Printf("[Metering Consumer] Postgres update failed for TX %s: %v", event.TransactionID, err)
		}

		log.Printf("[Metering Consumer] Successfully processed TX %s", event.TransactionID)
	}
}

func (c *MeteringConsumer) Close() error {
	return c.reader.Close()
}
