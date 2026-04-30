from typing import Any, Dict, Optional, Type
from decimal import Decimal
from pydantic import BaseModel, Field
from langchain.tools import BaseTool
from ..client import AgentClient

class SpendToolInput(BaseModel):
    amount: str = Field(description="The amount to spend, e.g., '0.50'")
    reason: str = Field(description="The reason for the expenditure")

class AgentMoneySpendTool(BaseTool):
    name: str = "agent_money_spend"
    description: str = "Use this tool to pay for actions or services. Input should be the amount and a reason."
    args_schema: Type[BaseModel] = SpendToolInput
    client: AgentClient

    def _run(self, amount: str, reason: str) -> str:
        try:
            result = self.client.spend(
                amount=Decimal(amount),
                context={"reason": reason, "integration": "langchain"}
            )
            return f"Transaction successful. ID: {result.transaction_id}. Status: {result.status}"
        except Exception as e:
            return f"Transaction failed: {str(e)}"

    async def _arun(self, amount: str, reason: str) -> str:
        # For simplicity, calling the sync version
        return self._run(amount, reason)
