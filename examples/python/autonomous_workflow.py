import sys
import os
from decimal import Decimal

# Add sdk path for demonstration
sys.path.append(os.path.abspath(os.path.join(os.path.dirname(__file__), "../../sdks/python")))

from agent_money import AgentClient, PolicyViolationError

def run_autonomous_task():
    client = AgentClient(
        base_url="http://localhost:8080",
        api_key="your_api_key",
        agent_id="research_agent_001"
    )

    # 1. Evaluate task cost before execution
    task_description = "Summarize top 10 papers on AGI safety"
    estimated_tokens = 5000
    estimated_cost = Decimal("0.05")

    print(f"Autonomous check: Executing '{task_description}'")
    print(f"Estimated cost: ${estimated_cost}")

    try:
        # Pre-authorizing the spend
        result = client.spend(
            amount=estimated_cost,
            context={
                "task": "summarization",
                "estimated_tokens": estimated_tokens,
                "urgency": "high"
            }
        )
        print(f"Spend approved! Transaction ID: {result.transaction_id}")
        
        # Now execute the actual task logic...
        print("Executing task logic...")
        
    except PolicyViolationError as e:
        print(f"Policy Blocked Action: {str(e)}")
        print("Agent Strategy: Fallback to a cheaper model or skip the task.")
    except Exception as e:
        print(f"System Error: {str(e)}")

if __name__ == "__main__":
    run_autonomous_task()
