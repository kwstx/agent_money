module github.com/galan/agent_money/services/transaction-handler

go 1.21

require (
	github.com/google/uuid v1.4.0
	github.com/segmentio/kafka-go v0.4.47
	google.golang.org/protobuf v1.31.0
	github.com/galan/agent_money/pkg/telemetry v0.0.0
)

replace github.com/galan/agent_money/pkg/telemetry => ../../pkg/telemetry
