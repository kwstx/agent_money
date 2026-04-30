from typing import Any, Dict, Optional
from decimal import Decimal
from llama_index.core.tools import FunctionTool
from ..client import AgentClient

def create_spend_tool(client: AgentClient) -> FunctionTool:
    """
    Creates a LlamaIndex FunctionTool for spending money.
    """
    def spend_money(amount: str, reason: str) -> str:
        try:
            result = client.spend(
                amount=Decimal(amount),
                context={"reason": reason, "integration": "llamaindex"}
            )
            return f"Spend successful: {result.transaction_id}"
        except Exception as e:
            return f"Spend failed: {str(e)}"

    return FunctionTool.from_defaults(
        fn=spend_money,
        name="agent_money_spend",
        description="Allows the agent to spend money for specific tasks or services."
    )
