package metering

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

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
	Timestamp     time.Time         `json:"timestamp"`
}

type EventPublisher struct {
	writer *kafka.Writer
}

func NewEventPublisher(brokerURL string, topic string) *EventPublisher {
	writer := &kafka.Writer{
		Addr:     kafka.TCP(brokerURL),
		Topic:    topic,
		Balancer: &kafka.LeastBytes{},
		// Ensure at-least-once delivery
		MaxAttempts: 5,
		RequiredAcks: kafka.RequireAll,
		Async:        true, // Fire-and-forget for the main flow
	}

	return &EventPublisher{
		writer: writer,
	}
}

func (p *EventPublisher) PublishFinancialEvent(ctx context.Context, event FinancialEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal financial event: %w", err)
	}

	err = p.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(event.TransactionID),
		Value: payload,
	})

	if err != nil {
		log.Printf("[Metering] Fire-and-forget publish failed for TX %s: %v", event.TransactionID, err)
		return err
	}

	return nil
}

func (p *EventPublisher) Close() error {
	return p.writer.Close()
}
