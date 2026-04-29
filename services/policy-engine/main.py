import json
import logging
from kafka import KafkaConsumer, KafkaProducer

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger("policy-engine")

def evaluate_policy(transaction):
    """
    Core policy logic. 
    In a real app, this would use a rules engine or OPA (Open Policy Agent).
    """
    amount = float(transaction.get("amount", 0))
    currency = transaction.get("currency", "USD")
    
    # Example rule: Limit transactions to 1000 USD
    if currency == "USD" and amount > 1000:
        return "FAILED", "Amount exceeds limit"
    
    return "APPROVED", None

def main():
    consumer = KafkaConsumer(
        'transaction-events',
        bootstrap_servers=['localhost:9092'],
        auto_offset_reset='earliest',
        group_id='policy-engine-group',
        value_deserializer=lambda x: json.loads(x.decode('utf-8'))
    )
    
    producer = KafkaProducer(
        bootstrap_servers=['localhost:9092'],
        value_serializer=lambda x: json.dumps(x).encode('utf-8')
    )

    logger.info("Policy Engine started, consuming from transaction-events")

    for message in consumer:
        tx = message.value
        if tx.get("status") != "PENDING":
            continue
            
        logger.info(f"Evaluating transaction {tx['transaction_id']}")
        
        status, reason = evaluate_policy(tx)
        tx["status"] = status
        if reason:
            tx["metadata"] = tx.get("metadata", {})
            tx["metadata"]["failure_reason"] = reason
            
        logger.info(f"Transaction {tx['transaction_id']} evaluated: {status}")
        
        # Emit an update event
        producer.send('transaction-events', value=tx)

if __name__ == "__main__":
    main()
