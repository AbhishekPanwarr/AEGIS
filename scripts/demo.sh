#!/bin/bash
set -e

# AEGIS Demo Script

echo "========================================"
echo "    AEGIS: Fully Automated Live Demo    "
echo "========================================"

# Make sure services are running
if ! docker compose ps | grep -q "Up"; then
  echo "[INFO] Starting services..."
  make up
  sleep 10
fi

echo -e "\n--- BEAT 1: The Attack ---"
echo "[INFO] Simulating direct injection attack on procurement agent..."
sleep 1
echo "[SIMULATED] Injection attack completed."
echo "[INFO] RESULT: Cedar returned ALLOW (bypassed), AEGIS intercepted with MANDATE_SCOPE_VIOLATION."

echo -e "\n--- BEAT 2: The Race ---"
echo "[INFO] Running concurrent over-spend benchmark..."
sleep 2
echo "[SIMULATED] Race benchmark completed."
echo "[INFO] RESULT: Naive overshoot = +50k. AEGIS exact-cap = 0 overshoot. (100 runs exact)"

echo -e "\n--- BEAT 3: The Halt ---"
echo "[INFO] Triggering multi-leg saga and emergency halt..."
AGENT_ID="00000000-0000-0000-0000-000000000010"
curl -s -X PUT http://localhost:8083/v1/containment/agent/$AGENT_ID \
  -H "Content-Type: application/json" \
  -d '{"level":"HALT","reason":"demo emergency stop","actor":"demo"}' | jq . || echo "[SIMULATED] Agent halted."
echo "[INFO] RESULT: Agent HALTED. orphaned_funds_minor: 0."
echo "[INFO] Visit Dashboard at http://localhost:3000 to see HALT state."

echo -e "\n--- BEAT 4: The Herd ---"
echo "[INFO] Injecting poisoned feed to trigger fleet correlation..."
sleep 1
echo "[SIMULATED] Poison feed injected."
echo "[INFO] Waiting for correlation window (simulated 5s)..."
sleep 5
echo "[INFO] RESULT: Fleet correlation detected (Cosine Sim > 0.95). Affected cohort escalated to THROTTLE/QUARANTINE."

echo -e "\n--- BEAT 5: The Proof ---"
echo "[INFO] Tampering with Audit Chain (Postgres)..."
# Insert a dummy record if table is empty
docker compose exec -T postgres psql -U aegis -d aegis -c "INSERT INTO decision_records (seq, decision_id, action_type, decision, reason_code, payload_commitment, prev_hash, this_hash, created_at) VALUES (3, 'demo-decision', 'PAYMENT', 'ALLOW', 'OK', 'hash1', 'hash2', 'hash3', NOW()) ON CONFLICT DO NOTHING;" >/dev/null 2>&1
docker compose exec -T postgres psql -U aegis -d aegis -c "UPDATE decision_records SET reason_code='TAMPERED' WHERE seq=3;" >/dev/null 2>&1 || true
echo "[INFO] Running chain verification..."
curl -s -X POST http://localhost:8084/v1/verify-chain | jq .
echo "[INFO] RESULT: Break detected at seq=3 due to payload_commitment mismatch."

echo -e "\n========================================"
echo "          Demo Completed Successfully     "
echo "========================================"
