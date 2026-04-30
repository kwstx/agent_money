package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/galan/agent_money/services/metering-service/internal/consumer"
	"github.com/galan/agent_money/services/metering-service/internal/db"
)

func main() {
	log.Println("Starting Metering Service...")

	kafkaURL := getEnv("KAFKA_URL", "localhost:9092")
	topic := getEnv("METERING_TOPIC", "financial-events")
	postgresURL := getEnv("DATABASE_URL", "postgres://admin:password@localhost:5432/agent_money?sslmode=disable")
	redisURL := getEnv("REDIS_URL", "localhost:6379")

	pg, err := db.NewPostgresRepo(postgresURL)
	if err != nil {
		log.Fatalf("Failed to connect to Postgres: %v", err)
	}

	rd := db.NewRedisRepo(redisURL)

	cons := consumer.NewMeteringConsumer(kafkaURL, topic, pg, rd)
	defer cons.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Shutting down...")
		cancel()
	}()

	// Start Webhook/Reconciliation Server
	http.HandleFunc("/webhooks/reconciliation", func(w http.ResponseWriter, r *http.Request) {
		// Example: Handle rail confirmations (e.g., Stripe, Lightning)
		// Matching execution confirmations against expected amounts
		log.Println("[Reconciliation] Received rail webhook")
		w.WriteHeader(http.StatusOK)
	})
	
	go func() {
		log.Println("Metering Reconciliation API listening on :8081")
		if err := http.ListenAndServe(":8081", nil); err != nil {
			log.Printf("Reconciliation API failed: %v", err)
		}
	}()

	cons.Start(ctx)
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
