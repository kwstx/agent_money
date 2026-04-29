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

# Configuration
KAFKA_BOOTSTRAP_SERVERS = os.getenv("KAFKA_BOOTSTRAP_SERVERS", "localhost:9092").split(",")
DATABASE_URL = os.getenv("DATABASE_URL", "postgresql://postgres:postgres@localhost:5432/agent_money")
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

    def _get_metering_data(self, agent_id: str) -> Dict[str, Any]:
        """
        Computed derived values from the metering layer.
        In a production system, this would involve a sub-5ms lookup 
        from Redis or a specialized metering service.
        """
        # Simulated derived values
        return {
            "current_spend_rate": 0.045, # USD/sec
            "daily_budget_used": 142.50,
            "daily_budget_limit": 500.00,
            "risk_score": 0.02,
            "agent_reputation": "HIGH"
        }

    def evaluate(self, transaction: Dict[str, Any]) -> PolicyDecision:
        rules = self.get_active_rules()
        agent_id = transaction.get("agent_id", "default")
        
        # Prepare evaluation context (Transaction + Telemetry/Metering)
        context = {
            "tx": transaction,
            "amount": float(transaction.get("amount", 0)),
            "currency": transaction.get("currency", "USD"),
            "metering": self._get_metering_data(agent_id),
            "env": os.getenv("APP_ENV", "production")
        }

        logger.info(f"Evaluating {len(rules)} rules for transaction {transaction.get('transaction_id')}")

        for rule in rules:
            try:
                # Short-circuiting evaluation logic
                is_match = True
                for condition in rule.conditions:
                    if not self._evaluate_condition(condition, context):
                        is_match = False
                        break
                
                if is_match:
                    logger.info(f"Rule {rule.rule_id} (Priority {rule.priority}) MATCHED")
                    
                    action_type = rule.actions.get("type", "approve").upper()
                    decision_str = "APPROVE"
                    if action_type == "REJECT":
                        decision_str = "REJECT"
                    elif action_type == "ROUTE_TO_RAIL":
                        decision_str = "ROUTE"

                    return PolicyDecision(
                        decision=decision_str,
                        selected_rails=rule.actions.get("rails", []),
                        modifications=rule.parameters or {},
                        rule_id=str(rule.rule_id),
                        reason=rule.actions.get("reason", f"Matched priority {rule.priority}")
                    )
            except Exception as e:
                logger.error(f"Rule evaluation error [Rule: {rule.rule_id}]: {e}")
                continue

        # Default fallback: Reject if no rules match (Zero Trust)
        return PolicyDecision(
            decision="REJECT",
            reason="Security Default: No policy rules matched this request"
        )

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
