import os
import requests
import psycopg2
from app.jsd import jsd, smoothed_distribution

EWMA_LAMBDA = 0.3
C_SUSTAIN = 3
N_MIN_SAMPLES = 20
SMOOTHING_ALPHA = 1.0

live_counts = {}
ewma_state = {}
consecutive = {}
baseline_slow = {}
drift_thresholds = {}

CONTAINMENT_ADDR = os.getenv("CONTAINMENT_ADDR", "http://containment-controller:8083")

def record_risk_event(conn, agent_id, feature, score, ewma, threshold, low_confidence=False):
    try:
        with conn.cursor() as cur:
            cur.execute("""
                INSERT INTO risk_events (agent_id, feature, score, ewma_score, threshold, low_confidence)
                VALUES (%s, %s, %s, %s, %s, %s)
            """, (agent_id, feature, score or 0, ewma or 0, threshold or 0, low_confidence))
            conn.commit()
    except Exception as e:
        print(f"record_risk_event err: {e}")
        conn.rollback()

def propose_escalation(agent_id, level, feature, s, ewma, threshold):
    try:
        requests.put(f"{CONTAINMENT_ADDR}/v1/containment/agent/{agent_id}", json={
            "level": level,
            "reason": f"{feature} drift exceeded",
            "actor": "drift-detector"
        })
    except Exception as e:
        print(f"Failed to propose escalation: {e}")

def derive_level(feature, ewma):
    if feature == "amount_minor":
        return "QUARANTINE"
    if feature in ["counterparty", "action_type"]:
        return "THROTTLE"
    return "OBSERVE"

def get_threshold(conn, agent_id, feature):
    if (agent_id, feature) in drift_thresholds:
        return drift_thresholds[(agent_id, feature)]
    try:
        with conn.cursor() as cur:
            cur.execute("""
                SELECT t.threshold FROM drift_thresholds t
                JOIN agents a ON a.persona = t.persona
                WHERE a.id = %s AND t.feature = %s
            """, (agent_id, feature))
            row = cur.fetchone()
            if row:
                val = float(row[0])
                drift_thresholds[(agent_id, feature)] = val
                return val
    except Exception as e:
        print(f"get_threshold err: {e}")
        conn.rollback()
    return None

def on_new_action(event, r, conn):
    agent_id = event.get('agent_id')
    if not agent_id: return
    
    if agent_id not in live_counts:
        live_counts[agent_id] = {"action_type": {}, "counterparty": {}, "amount_minor": {}, "timing": {}}
        ewma_state[agent_id] = {"action_type": 0, "counterparty": 0, "amount_minor": 0, "timing": 0}
        consecutive[agent_id] = {"action_type": 0, "counterparty": 0, "amount_minor": 0, "timing": 0}
        baseline_slow[agent_id] = {"action_type": {}, "counterparty": {}, "amount_minor": {}, "timing": {}}

    at = event.get('action_type', 'unknown')
    live_counts[agent_id]['action_type'][at] = live_counts[agent_id]['action_type'].get(at, 0) + 1

    cp = event.get('counterparty_id', 'unknown')
    live_counts[agent_id]['counterparty'][cp] = live_counts[agent_id]['counterparty'].get(cp, 0) + 1

    for feature in ["action_type", "counterparty"]:
        counts = live_counts[agent_id][feature]
        n = sum(counts.values())
        if n < N_MIN_SAMPLES:
            record_risk_event(conn, agent_id, feature, None, None, None, True)
            continue
            
        threshold = get_threshold(conn, agent_id, feature)
        if threshold is None:
            record_risk_event(conn, agent_id, feature, None, None, None, True)
            continue

        q = baseline_slow[agent_id][feature]
        all_bins = list(set(list(counts.keys()) + list(q.keys()) + ['OTHER']))

        p_smooth = smoothed_distribution(counts, all_bins, SMOOTHING_ALPHA)
        q_smooth = smoothed_distribution(q, all_bins, SMOOTHING_ALPHA)

        s = jsd(p_smooth, q_smooth)
        prev_ewma = ewma_state[agent_id][feature]
        new_ewma = EWMA_LAMBDA * s + (1 - EWMA_LAMBDA) * prev_ewma
        ewma_state[agent_id][feature] = new_ewma

        # Set prometheus metric
        from app.main import aegis_drift_score
        aegis_drift_score.labels(agent_id=agent_id, feature=feature).set(new_ewma)

        record_risk_event(conn, agent_id, feature, s, new_ewma, threshold, False)

        if new_ewma > threshold:
            consecutive[agent_id][feature] += 1
        else:
            consecutive[agent_id][feature] = 0

        if consecutive[agent_id][feature] >= C_SUSTAIN:
            propose_escalation(agent_id, derive_level(feature, new_ewma), feature, s, new_ewma, threshold)
            consecutive[agent_id][feature] = 0
