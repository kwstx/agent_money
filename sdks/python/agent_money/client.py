import time
import uuid
import functools
import requests
from decimal import Decimal
from typing import Optional, Dict, Any, List, Callable
from .models import TransactionResult
from .exceptions import (
    AgentMoneyError, 
    PolicyViolationError, 
    RailFailureError, 
    AuthenticationError,
    RateLimitError
)

class AgentClient:
    def __init__(
        self, 
        base_url: str, 
        api_key: str, 
        agent_id: str,
        timeout: int = 30,
        max_retries: int = 3
    ):
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.agent_id = agent_id
        self.timeout = timeout
        self.max_retries = max_retries
        self.session = requests.Session()
        self.session.headers.update({
            "Authorization": f"Bearer {self.api_key}",
            "X-Consumer-Username": self.agent_id,
            "Content-Type": "application/json"
        })

    def spend(
        self, 
        amount: Decimal, 
        context: Dict[str, Any], 
        constraints: List[Any] = None,
        request_id: str = None
    ) -> TransactionResult:
        """
        Executes a spend request via the orchestration service.
        """
        if request_id is None:
            request_id = str(uuid.uuid4())

        payload = {
            "request_id": request_id,
            "amount": float(amount),
            "currency": context.get("currency", "USD"),
            "context": context,
            "constraints": constraints or []
        }

        retries = 0
        while retries <= self.max_retries:
            try:
                response = self.session.post(
                    f"{self.base_url}/spend",
                    json=payload,
                    timeout=self.timeout
                )
                
                if response.status_code == 202 or response.status_code == 200:
                    return TransactionResult.from_dict(response.json())
                
                if response.status_code == 401:
                    raise AuthenticationError("Invalid API key")
                if response.status_code == 403:
                    raise PolicyViolationError(
                        "Policy violation: " + response.text,
                        policy_details=response.json() if response.headers.get("Content-Type") == "application/json" else None
                    )
                if response.status_code == 429:
                    raise RateLimitError("Rate limit exceeded")
                if response.status_code >= 500:
                    if retries < self.max_retries:
                        retries += 1
                        time.sleep(2 ** retries)
                        continue
                    raise RailFailureError(f"Server error: {response.text}")
                
                response.raise_for_status()

            except requests.exceptions.RequestException as e:
                if retries < self.max_retries:
                    retries += 1
                    time.sleep(2 ** retries)
                    continue
                raise AgentMoneyError(f"Network error: {str(e)}")

        raise AgentMoneyError("Max retries exceeded")

def pay_per_action(
    client: AgentClient, 
    amount: Decimal, 
    context_provider: Optional[Callable[..., Dict[str, Any]]] = None
):
    """
    Decorator to automatically trigger a spend request before executing a function.
    """
    def decorator(func):
        @functools.wraps(func)
        def wrapper(*args, **kwargs):
            ctx = context_provider(*args, **kwargs) if context_provider else {"action": func.__name__}
            # Pre-execution check or spend
            client.spend(amount, ctx)
            return func(*args, **kwargs)
        return wrapper
    return decorator
