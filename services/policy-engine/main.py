import json
import logging
import os
import uuid
from typing import List, Dict, Any, Optional
from dataclasses import dataclass

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

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger("policy-engine")

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
            logger.error(f"Failed to fetch policy from auth-service: {e}")
        return None

    def evaluate(self, transaction: Dict[str, Any]) -> PolicyDecision:
        agent_id = transaction.get("agent_id", "default")
        workflow_id = transaction.get("metadata", {}).get("workflow_id")
        
        # 1. Fetch RBAC/ABAC Policy from Auth System
        auth_policy = self._get_auth_policy(agent_id, workflow_id)
        if not auth_policy:
            return PolicyDecision(
                decision="REJECT",
                reason="Identity/Permission Error: No active policy found for this agent"
            )

        # 2. Perform ABAC Checks
        attrs = auth_policy.get("attributes", {})
        
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

        # 3. Evaluate Rule-based Logic (if any additional rules are active)
        rules = self.get_active_rules()
        context = {
            "tx": transaction,
            "amount": amount,
            "currency": transaction.get("currency", "USD"),
            "policy": auth_policy,
            "env": os.getenv("APP_ENV", "production")
        }

        if not rules:
            # If no specific rules, but ABAC passed, we can default to Approve if the policy says so
            # or if 'executor' role is present.
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
                    return PolicyDecision(
                        decision="APPROVE" if action_type != "REJECT" else "REJECT",
                        selected_rails=rule.actions.get("rails", []),
                        modifications=rule.parameters or {},
                        rule_id=str(rule.rule_id),
                        reason=rule.actions.get("reason", f"Matched priority {rule.priority}")
                    )
            except Exception as e:
                logger.error(f"Rule evaluation error: {e}")
                continue

        return PolicyDecision(decision="REJECT", reason="No specific rules matched")

    def _evaluate_condition(self, expression: str, context: Dict[str, Any]) -> bool:
        """
        Evaluates a single condition using the ZEN expression engine.
        ZEN expressions are compiled for high performance.
        """
        try:
            # ZEN evaluate_expression takes an expression string and a context
            # It returns the result of the expression (usually bool for conditions)
            return self.zen_engine.evaluate_expression(expression, context)
        except Exception as e:
            logger.debug(f"Condition check failed: '{expression}' | Error: {e}")
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

    logger.info("Policy Engine Service listening on Kafka...")

    for message in consumer:
        tx = message.value
        # Only process transactions in REQUESTED or PENDING state
        if tx.get("status") not in ["PENDING", "REQUESTED"]:
            continue

        start_time = func.now() # Simplified latency tracking
        decision = policy_engine.evaluate(tx)
        
        # Update transaction state
        tx["status"] = "APPROVED" if decision.decision in ["APPROVE", "ROUTE"] else "REJECTED"
        tx["metadata"] = tx.get("metadata", {})
        tx["metadata"]["policy_decision"] = decision.model_dump()
        
        if decision.selected_rails:
            tx["rail_type"] = decision.selected_rails[0] # Take primary rail

        logger.info(f"Decision for {tx['transaction_id']}: {tx['status']} (Rule: {decision.rule_id})")
        
        producer.send(TOPIC_TRANSACTIONS, value=tx)

if __name__ == "__main__":
    main()
