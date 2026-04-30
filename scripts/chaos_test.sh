#!/bin/bash
# Simple Chaos Test Script for Agent Money

NAMESPACE="agent-money-system"
SERVICE="orchestration-service"

echo "Starting Chaos Test: Simulating Latency on Lightning Rail..."

# Use kubectl to inject environment variables or labels that trigger failure modes in the adapters
# In a real K8s environment, you might use Chaos Mesh or Toxiproxy.

# Example: Force Lightning rail to return 500s or high latency via a config map update
# kubectl patch configmap orchestration-config -n $NAMESPACE --type merge -p '{"data":{"LIGHTNING_LATENCY":"5s"}}'

echo "Monitoring error rates..."
# Check metrics from Prometheus
# curl -s "http://prometheus:9090/api/v1/query?query=rate(orchestration_errors_total[1m])"

echo "Verifying fallback to Stripe rail..."
# Check logs for "switching to fallback"
# kubectl logs -l app=$SERVICE -n $NAMESPACE | grep "fallback"

echo "Chaos Test Complete."
