import pytest
from unittest.mock import MagicMock, patch
from main import PolicyEngine, RuleModel, PolicyDecision

@pytest.fixture
def policy_engine():
    with patch('main.create_engine'):
        with patch('main.sessionmaker'):
            engine = PolicyEngine("sqlite:///:memory:")
            return engine

def test_evaluate_approve_simple(policy_engine):
    # Setup mock rules
    mock_rule = RuleModel(
        priority=1,
        conditions=["tx.amount < 100"],
        actions={"type": "approve", "reason": "Small transaction"},
        active=True
    )
    policy_engine.get_active_rules = MagicMock(return_value=[mock_rule])
    policy_engine._get_auth_policy = MagicMock(return_value={
        "agent_id": "agent_1",
        "attributes": {"risk_threshold": 0.5, "task_cost_ceiling": 1000},
        "roles": ["executor"]
    })
    policy_engine._get_external_risk_score = MagicMock(return_value=0.1)
    policy_engine.zen_engine.evaluate_expression = MagicMock(return_value=True)

    transaction = {
        "agent_id": "agent_1",
        "amount": 50,
        "currency": "USD"
    }

    decision = policy_engine.evaluate(transaction)
    assert decision.decision == "APPROVE"
    assert "Small transaction" in decision.reason

def test_evaluate_reject_risk(policy_engine):
    policy_engine._get_auth_policy = MagicMock(return_value={
        "agent_id": "agent_1",
        "attributes": {"risk_threshold": 0.2},
        "roles": ["executor"]
    })
    policy_engine._get_external_risk_score = MagicMock(return_value=0.5)

    transaction = {
        "agent_id": "agent_1",
        "amount": 50
    }

    decision = policy_engine.evaluate(transaction)
    assert decision.decision == "REJECT"
    assert "Risk score" in decision.reason

def test_evaluate_reject_ceiling(policy_engine):
    policy_engine._get_auth_policy = MagicMock(return_value={
        "agent_id": "agent_1",
        "attributes": {"task_cost_ceiling": 10},
        "roles": ["executor"]
    })
    policy_engine._get_external_risk_score = MagicMock(return_value=0.1)

    transaction = {
        "agent_id": "agent_1",
        "amount": 50
    }

    decision = policy_engine.evaluate(transaction)
    assert decision.decision == "REJECT"
    assert "exceeds task cost ceiling" in decision.reason
