import os
import psycopg2
import time

def run_calibration():
    print("Running drift calibration...")
    dsn = os.getenv("POSTGRES_DSN", "postgresql://aegis:aegis@postgres:5432/aegis")
    
    # Retry loop for PG connection
    for _ in range(5):
        try:
            conn = psycopg2.connect(dsn)
            break
        except Exception as e:
            print("Waiting for postgres...", e)
            time.sleep(2)
    else:
        print("Failed to connect to Postgres")
        return

    try:
        with conn.cursor() as cur:
            cur.execute("""
                CREATE TABLE IF NOT EXISTS drift_thresholds (
                    persona VARCHAR(50),
                    feature VARCHAR(50),
                    threshold DECIMAL(10,4),
                    updated_at TIMESTAMPTZ DEFAULT now(),
                    PRIMARY KEY (persona, feature)
                )
            """)
            
            # Dummy thresholds based on seed data analysis (simulated)
            thresholds = [
                ('procurement', 'action_type', 0.1500),
                ('procurement', 'counterparty', 0.2500),
                ('treasury', 'action_type', 0.1000),
                ('treasury', 'counterparty', 0.2000),
            ]
            
            for persona, feature, t in thresholds:
                cur.execute("""
                    INSERT INTO drift_thresholds (persona, feature, threshold)
                    VALUES (%s, %s, %s)
                    ON CONFLICT (persona, feature) DO UPDATE SET threshold = %s, updated_at = now()
                """, (persona, feature, t, t))
                
            conn.commit()
            print("Calibration complete. Thresholds inserted.")
    except Exception as e:
        print(f"Calibration failed: {e}")
        conn.rollback()
    finally:
        conn.close()

if __name__ == "__main__":
    run_calibration()
