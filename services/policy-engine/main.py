import json
import logging
import os
import uuid
from typing import List, Dict, Any, Optional
from dataclasses import dataclass
import structlog
from opentelemetry import trace, metrics
from opentelemetry.sdk.resources import RESOURCE_ATTRIBUTES, Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.exporter.otlp.proto.grpc.metric_exporter import OTLPMetricExporter

import zen
from kafka import KafkaConsumer, KafkaProducer
from sqlalchemy import create_all_all, create_engine, Column, Integer, String, JSON, Boolean, DateTime, select
from sqlalchemy.orm import declarative_base, sessionmaker
from sqlalchemy.sql import func
from pydantic import BaseModel

import requests
import re
from datetime import datetime

# Configuration
KAFKA_BOOTSTRAP_SERVERS = os.getenv("KAFKA_BOOTSTRAP_SERVERS", "localhost:9092").split(",")
DATABASE_URL = os.getenv("DATABASE_URL", "postgresql://postgres:postgres@localhost:5432/agent_money")
AUTH_SERVICE_URL = os.getenv("AUTH_SERVICE_URL", "http://localhost:8081")
TOPIC_TRANSACTIONS = "transaction-events"
CONSUMER_GROUP = "policy-engine-group"

# Configure Structlog
structlog.configure(
    processors=[
        structlog.processors.add_log_level,
        structlog.processors.TimeStamper(fmt="iso"),
        structlog.processors.JSONRenderer(),
    ],
    logger_factory=structlog.PrintLoggerFactory(),
)
logger = structlog.get_logger("policy-engine")

# Configure OpenTelemetry
resource = Resource(attributes={
    RESOURCE_ATTRIBUTES["service.name"]: "policy-engine"
})

trace_provider = TracerProvider(resource=resource)
otlp_exporter = OTLPSpanExporter(endpoint=os.getenv("OTEL_COLLECTOR_URL", "localhost:4317"), insecure=True)
trace_provider.add_span_processor(BatchSpanProcessor(otlp_exporter))
trace.set_tracer_provider(trace_provider)
tracer = trace.get_tracer(__name__)

meter_provider = MeterProvider(
    resource=resource,
    metric_readers=[PeriodicExportingMetricReader(OTLPMetricExporter(endpoint=os.getenv("OTEL_COLLECTOR_URL", "localhost:4317"), insecure=True))]
)
metrics.set_meter_provider(meter_provider)
meter = metrics.get_meter(__name__)

policy_latency = meter.create_histogram(
    "policy_evaluation_latency",
    unit="ms",
    description="Latency of policy evaluation"
)
risk_score_gauge = meter.create_gauge(
    "external_risk_score",
    unit="score",
    description="Latest external risk score retrieved"
)

Base = declarative_base()

class RuleModel(Base):
    __tablename__ = 'rules'
    rule_id = Column(String, primary_key=True, default=lambda: str(uuid.uuid4()))
    priority = Column(Integer, nullable=False)
    conditions = Column(JSON, nullable=False) # List of expressions
    actions = Column(JSON, nullable=False)    # {type: approve/reject/route, rails: []}
    parameters = Column(JSON)                 # {budget_remaining: 100, ...}
    active = Column(Boolean, default=True)
    created_at = Column(DateTime, default=func.now())

class PolicyDecision(BaseModel):
    decision: str  # APPROVE, REJECT, ROUTE
    selected_rails: List[str] = []
    modifications: Dict[str, Any] = {}
    rule_id: Optional[str] = None
    reason: Optional[str] = None

class PolicyEngine:
    def __init__(self, db_url: str):
        self.db_engine = create_engine(db_url)
        self.Session = sessionmaker(bind=self.db_engine)
        self.zen_engine = zen.ZenEngine()
        logger.info("Policy Engine initialized with GoRules ZEN core")

    def get_active_rules(self) -> List[RuleModel]:
        with self.Session() as session:
            try:
                stmt = select(RuleModel).where(RuleModel.active == True).order_by(RuleModel.priority.asc())
                return session.execute(stmt).scalars().all()
            except Exception as e:
                logger.error(f"Failed to fetch rules from DB: {e}")
                return []

    def _get_external_risk_score(self, agent_id: str) -> float:
        """Fetches external risk scores for KYC/AML compliance."""
        try:
            # Simulate call to external risk provider (e.g., Chainalysis, Elliptic, or internal KYC)
            risk_service_url = os.getenv("RISK_SERVICE_URL", "http://risk-service:8082")
            response = requests.get(f"{risk_service_url}/score/{agent_id}", timeout=2)
            if response.status_code == 200:
                score = response.json().get("risk_score", 0.1)
                risk_score_gauge.set(score, {"agent_id": agent_id})
                return score
        except Exception as e:
            logger.error("failed_to_fetch_risk_score", agent_id=agent_id, error=str(e))
        return 0.1 # Default low risk if service unavailable

    def _get_auth_policy(self, agent_id: str, workflow_id: Optional[str] = None) -> Optional[Dict[str, Any]]:
        """Queries the auth-service for the effective policy for this agent/workflow."""
        try:
            params = {"agent_id": agent_id}
            if workflow_id:
                params["workflow_id"] = workflow_id
            
            response = requests.get(f"{AUTH_SERVICE_URL}/internal/effective-policy", params=params, timeout=2)
            if response.status_code == 200:
                return response.json()
        except Exception as e:
            logger.error("auth_policy_fetch_failed", error=str(e))
        return None

    def evaluate(self, transaction: Dict[str, Any]) -> PolicyDecision:
        with tracer.start_as_current_span("policy_evaluation") as span:
            start_time = datetime.now()
            agent_id = transaction.get("agent_id", "default")
            span.set_attribute("agent_id", agent_id)
            
            workflow_id = transaction.get("metadata", {}).get("workflow_id")
            
            # 1. Fetch RBAC/ABAC Policy from Auth System
            auth_policy = self._get_auth_policy(agent_id, workflow_id)
            if not auth_policy:
                return PolicyDecision(
                    decision="REJECT",
                    reason="Identity/Permission Error: No active policy found for this agent"
                )

            # 2. Risk Score Hook (KYC/AML)
            risk_score = self._get_external_risk_score(agent_id)
            span.set_attribute("risk_score", risk_score)
            
            # Check risk threshold from policy attributes
            attrs = auth_policy.get("attributes", {})
            risk_threshold = float(attrs.get("risk_threshold", 0.5))
            if risk_score > risk_threshold:
                return PolicyDecision(
                    decision="REJECT",
                    reason=f"Compliance Violation: Risk score {risk_score} exceeds threshold {risk_threshold}"
                )

            # 3. Perform ABAC Checks
            # Check Task Cost Ceiling
            amount = float(transaction.get("amount", 0))
            ceiling = float(attrs.get("task_cost_ceiling", 0))
            if ceiling > 0 and amount > ceiling:
                return PolicyDecision(
                    decision="REJECT",
                    reason=f"ABAC Violation: Transaction amount {amount} exceeds task cost ceiling of {ceiling}"
                )

            # Check Context Patterns (Regex)
            allowed_patterns = attrs.get("allowed_context_patterns", [])
            if allowed_patterns:
                context_str = json.dumps(transaction.get("context", {}))
                matched = False
                for pattern in allowed_patterns:
                    if re.search(pattern, context_str):
                        matched = True
                        break
                if not matched:
                    return PolicyDecision(
                        decision="REJECT",
                        reason="ABAC Violation: Transaction context does not match allowed patterns"
                    )

            # Check Daily Budget
            daily_limit = float(attrs.get("daily_budget", 0))
            if daily_limit > 0:
                metering = self._get_metering_data(agent_id)
                current_spent = metering.get("daily_budget_used", 0)
                if current_spent + amount > daily_limit:
                    return PolicyDecision(
                        decision="REJECT",
                        reason=f"ABAC Violation: Daily budget exceeded ({current_spent + amount} > {daily_limit})"
                    )

            # 4. Evaluate Rule-based Logic
            rules = self.get_active_rules()
            context = {
                "tx": transaction,
                "amount": amount,
                "currency": transaction.get("currency", "USD"),
                "policy": auth_policy,
                "env": os.getenv("APP_ENV", "production"),
                "risk_score": risk_score
            }

            if not rules:
                if "executor" in auth_policy.get("roles", []):
                    return PolicyDecision(decision="APPROVE", reason="ABAC passed and agent has executor role")
                return PolicyDecision(decision="REJECT", reason="ABAC passed but no explicit rules or roles for execution")

            for rule in rules:
                try:
                    is_match = True
                    for condition in rule.conditions:
                        if not self._evaluate_condition(condition, context):
                            is_match = False
                            break
                    
                    if is_match:
                        action_type = rule.actions.get("type", "approve").upper()
                        decision = PolicyDecision(
                            decision="APPROVE" if action_type != "REJECT" else "REJECT",
                            selected_rails=rule.actions.get("rails", []),
                            modifications=rule.parameters or {},
                            rule_id=str(rule.rule_id),
                            reason=rule.actions.get("reason", f"Matched priority {rule.priority}")
                        )
                        # Record latency metric
                        latency = (datetime.now() - start_time).total_seconds() * 1000
                        policy_latency.record(latency, {"decision": decision.decision})
                        return decision
                except Exception as e:
                    logger.error("rule_evaluation_error", error=str(e))
                    continue

            return PolicyDecision(decision="REJECT", reason="No specific rules matched")

    def _get_metering_data(self, agent_id: str) -> Dict[str, Any]:
        """Fetches real-time budget usage from Redis (simulated or via metering-service)."""
        # In a real system, this would call the metering-service or query Redis directly.
        return {"daily_budget_used": 0.0}

    def _evaluate_condition(self, expression: str, context: Dict[str, Any]) -> bool:
        """
        Evaluates a single condition using the ZEN expression engine.
        ZEN expressions are compiled for high performance.
        """
        try:
            return self.zen_engine.evaluate_expression(expression, context)
        except Exception as e:
            logger.debug("condition_check_failed", expression=expression, error=str(e))
            return False


def main():
    policy_engine = PolicyEngine(DATABASE_URL)
    
    consumer = KafkaConsumer(
        TOPIC_TRANSACTIONS,
        bootstrap_servers=KAFKA_BOOTSTRAP_SERVERS,
        auto_offset_reset='earliest',
        group_id=CONSUMER_GROUP,
        value_deserializer=lambda x: json.loads(x.decode('utf-8'))
    )
    
    producer = KafkaProducer(
        bootstrap_servers=KAFKA_BOOTSTRAP_SERVERS,
        value_serializer=lambda x: json.dumps(x).encode('utf-8')
    )
 
    logger.info("policy_engine_started", kafka_topics=[TOPIC_TRANSACTIONS])
 
    for message in consumer:
        with tracer.start_as_current_span("process_transaction_event") as span:
            tx = message.value
            tx_id = tx.get("transaction_id", "unknown")
            span.set_attribute("transaction_id", tx_id)

            # Only process transactions in REQUESTED or PENDING state
            if tx.get("status") not in ["PENDING", "REQUESTED"]:
                continue
 
            decision = policy_engine.evaluate(tx)
            
            # Update transaction state
            tx["status"] = "APPROVED" if decision.decision in ["APPROVE", "ROUTE"] else "REJECTED"
            tx["metadata"] = tx.get("metadata", {})
            tx["metadata"]["policy_decision"] = decision.model_dump()
            
            if decision.selected_rails:
                tx["rail_type"] = decision.selected_rails[0]
 
            logger.info("policy_decision_made", 
                        transaction_id=tx_id, 
                        status=tx["status"], 
                        rule_id=decision.rule_id,
                        reason=decision.reason)
            
            producer.send(TOPIC_TRANSACTIONS, value=tx)

if __name__ == "__main__":
    main()
