import os
import time
import logging
import uuid
from decimal import Decimal
from datetime import datetime, timezone
import schedule
import psycopg2
from psycopg2.extras import RealDictCursor
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.psycopg2 import Psycopg2Instrumentor

# Setup logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(name)s - %(levelname)s - %(message)s')
logger = logging.getLogger("reconciliation-worker")

# OpenTelemetry setup
trace.set_tracer_provider(TracerProvider())
otlp_exporter = OTLPSpanExporter(endpoint=os.getenv("OTEL_COLLECTOR_URL", "http://otel-collector:4317"), insecure=True)
trace.get_tracer_provider().add_span_processor(BatchSpanProcessor(otlp_exporter))
tracer = trace.get_tracer(__name__)
Psycopg2Instrumentor().instrument()

DATABASE_URL = os.getenv("DATABASE_URL", "postgresql://admin:password@postgres:5432/agent_money")

def get_db_connection():
    return psycopg2.connect(DATABASE_URL, cursor_factory=RealDictCursor)

def fetch_exchange_rate(from_curr, to_curr, conn):
    if from_curr == to_curr:
        return Decimal('1.0')
    
    with conn.cursor() as cur:
        cur.execute(
            "SELECT rate FROM exchange_rates WHERE from_currency = %s AND to_currency = %s",
            (from_curr, to_curr)
        )
        row = cur.fetchone()
        if row:
            return Decimal(str(row['rate']))
        
        # Try reverse
        cur.execute(
            "SELECT rate FROM exchange_rates WHERE from_currency = %s AND to_currency = %s",
            (to_curr, from_curr)
        )
        row = cur.fetchone()
        if row:
            return Decimal('1.0') / Decimal(str(row['rate']))
            
    logger.warning(f"No exchange rate found for {from_curr} to {to_curr}, defaulting to 1.0 (DANGEROUS)")
    return Decimal('1.0')

def notify_administrators(tx_id, diff, run_id):
    # In a real system, this could send an email, Slack message, or PagerDuty alert
    logger.error(f"ALERT: Discrepancy detected in run {run_id} for transaction {tx_id}. Difference: {diff}")
    # Example: requests.post("https://hooks.slack.com/services/...", json={"text": "..."})

def reconcile():
    with tracer.start_as_current_span("reconciliation_run") as span:
        logger.info("Starting reconciliation run...")
        conn = get_db_connection()
        run_id = str(uuid.uuid4())
        
        try:
            with conn.cursor() as cur:
                # Initialize run
                cur.execute(
                    "INSERT INTO reconciliation_runs (run_id, status) VALUES (%s, 'running')",
                    (run_id,)
                )
                conn.commit()

                # 1. Fetch pending external confirmations
                cur.execute(
                    "SELECT * FROM external_confirmations WHERE reconciled_at IS NULL"
                )
                confirmations = cur.fetchall()
                
                matched_count = 0
                discrepancy_count = 0
                adjustment_count = 0

                for conf in confirmations:
                    tx_id = conf['transaction_id']
                    external_amount = Decimal(str(conf['amount']))
                    external_currency = conf['currency']
                    
                    # Fetch internal transaction
                    cur.execute(
                        "SELECT * FROM transactions WHERE transaction_id = %s",
                        (tx_id,)
                    )
                    tx = cur.fetchone()
                    
                    if not tx:
                        # Log discrepancy: External confirmation for unknown transaction
                        cur.execute(
                            "INSERT INTO discrepancies (run_id, confirmation_id, description, severity) VALUES (%s, %s, %s, 'high')",
                            (run_id, conf['confirmation_id'], f"External confirmation for unknown transaction {tx_id}", "high")
                        )
                        discrepancy_count += 1
                        continue

                    internal_amount = Decimal(str(tx['amount']))
                    internal_currency = tx['currency']

                    # Convert external amount to internal currency for comparison if needed
                    rate = fetch_exchange_rate(external_currency, internal_currency, conn)
                    converted_external_amount = (external_amount * rate).quantize(Decimal('0.00000001'))

                    # Compare
                    if converted_external_amount == internal_amount:
                        # Match!
                        cur.execute(
                            "UPDATE external_confirmations SET reconciled_at = %s WHERE confirmation_id = %s",
                            (datetime.now(timezone.utc), conf['confirmation_id'])
                        )
                        matched_count += 1
                        logger.info(f"Matched transaction {tx_id}")
                    else:
                        # Discrepancy!
                        diff = converted_external_amount - internal_amount
                        cur.execute(
                            "INSERT INTO discrepancies (run_id, transaction_id, confirmation_id, description, severity) VALUES (%s, %s, %s, %s, 'medium')",
                            (run_id, tx_id, conf['confirmation_id'], f"Amount mismatch: internal={internal_amount}{internal_currency}, external={external_amount}{external_currency} (converted={converted_external_amount}{internal_currency}), diff={diff}", "medium")
                        )
                        discrepancy_count += 1
                        
                        # Create adjustment entry in ledger
                        cur.execute(
                            "INSERT INTO ledger_entries (transaction_id, account_id, amount, entry_type, metadata) VALUES (%s, 'reconciliation_adjustment', %s, 'settlement', %s)",
                            (tx_id, float(diff), '{"reason": "reconciliation_mismatch"}')
                        )
                        adjustment_count += 1
                        
                        # Mark as reconciled with adjustment
                        cur.execute(
                            "UPDATE external_confirmations SET reconciled_at = %s WHERE confirmation_id = %s",
                            (datetime.now(timezone.utc), conf['confirmation_id'])
                        )
                        logger.warning(f"Discrepancy for {tx_id}: diff {diff}")
                        
                        # Notify administrators (Mock)
                        notify_administrators(tx_id, diff, run_id)

                # Update run status
                summary = {
                    "matched": matched_count,
                    "discrepancies": discrepancy_count,
                    "adjustments": adjustment_count
                }
                cur.execute(
                    "UPDATE reconciliation_runs SET status = 'completed', completed_at = %s, summary = %s WHERE run_id = %s",
                    (datetime.now(timezone.utc), str(summary).replace("'", '"'), run_id)
                )
                conn.commit()
                logger.info(f"Reconciliation run {run_id} completed: {summary}")
                
        except Exception as e:
            logger.error(f"Reconciliation run {run_id} failed: {e}")
            if conn:
                with conn.cursor() as cur:
                    cur.execute(
                        "UPDATE reconciliation_runs SET status = 'failed', completed_at = %s WHERE run_id = %s",
                        (datetime.now(timezone.utc), run_id)
                    )
                conn.commit()
        finally:
            if conn:
                conn.close()

def run_scheduler():
    # Run every minute for testing, could be every hour/day in prod
    schedule.every(1).minutes.do(reconcile)
    
    logger.info("Scheduler started. Running reconciliation every minute.")
    while True:
        schedule.run_pending()
        time.sleep(1)

if __name__ == "__main__":
    # Initial run
    reconcile()
    # Start scheduler
    run_scheduler()
