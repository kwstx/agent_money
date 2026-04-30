from dataclasses import dataclass
from decimal import Decimal
from typing import Optional, Dict, Any

@dataclass
class TransactionResult:
    transaction_id: str
    status: str
    rail: Optional[str]
    estimated_cost: Decimal
    raw_response: Dict[str, Any]

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "TransactionResult":
        return cls(
            transaction_id=data.get("transaction_id", ""),
            status=data.get("status", "unknown"),
            rail=data.get("rail"),
            estimated_cost=Decimal(str(data.get("estimated_cost", "0"))),
            raw_response=data
        )
