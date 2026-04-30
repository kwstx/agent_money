class AgentMoneyError(Exception):
    """Base exception for Agent Money SDK."""
    pass

class PolicyViolationError(AgentMoneyError):
    """Raised when a spend request violates the defined policy."""
    def __init__(self, message, policy_details=None):
        super().__init__(message)
        self.policy_details = policy_details

class RailFailureError(AgentMoneyError):
    """Raised when all payment rails fail to execute the transaction."""
    def __init__(self, message, transaction_id=None):
        super().__init__(message)
        self.transaction_id = transaction_id

class AuthenticationError(AgentMoneyError):
    """Raised when authentication fails."""
    pass

class RateLimitError(AgentMoneyError):
    """Raised when the agent exceeds its rate limit."""
    pass
