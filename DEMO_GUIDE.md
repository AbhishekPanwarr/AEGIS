# AEGIS Demo Guide

This document provides a comprehensive, step-by-step guide for presenting the AEGIS prototype. It highlights the narrative, specific execution commands, expected system behaviors, and the exact telemetry visualisations to showcase during the 7-minute live demonstration.

## Narrative Overview
The demo proves that autonomous agents can be governed securely without sacrificing speed. We will demonstrate how AEGIS intercepts malicious out-of-scope actions, strictly enforces sub-millisecond distributed budgets against aggressive concurrency, halts erratic agents mid-flight, detects coordinated fleet anomalies, and guarantees absolute cryptographic immutability of its execution logs.

---

## Beat 1: The Attack (Identity & Mandates)

**Goal:** Prove that stolen credentials or compromised LLMs cannot bypass strict identity boundaries.
**Security Property:** Cryptographic mandate attenuation and scope verification.

**Steps:**
1. Execute: `bash scripts/demo.sh` (or manually run `python simulation/agent-sim/app/attacks/injection.py`)
2. Observe the terminal output.

**Expected Output:**
The Cedar policy engine evaluates the action as `ALLOW` (the agent structurally requested a valid action). However, the AEGIS gateway intercepts the call at Stage 3, returning `MANDATE_SCOPE_VIOLATION`.

**What to Highlight:**
- Show that despite the agent technically bypassing the LLM prompt-guard, the rigid PKI mandate cryptographically prevented the execution.

---

## Beat 2: The Race (Distributed Budgeting)

**Goal:** Prove that the budget ledger correctly caps spending despite massive concurrent burst requests.
**Security Property:** Atomic distributed state execution (Lua).

**Steps:**
1. The script triggers `bash scripts/run_race_benchmark.sh`.

**Expected Output:**
The terminal will display a side-by-side comparison. A naive optimistic-locking ledger typically overshoots by thousands. AEGIS will report `Overshoot: 0`.
*(Screenshot: `docs/screenshots/grafana_enforcement.png`)*

**What to Highlight:**
- Open the Grafana **Budget Velocity & Exceptions** dashboard.
- Show the massive spike in request volume, immediately met by a wall of `403 BUDGET_EXCEEDED` lines once the exact cap is breached.

---

## Beat 3: The Halt (Sagas & Containment)

**Goal:** Demonstrate the emergency containment system halting a live agent.
**Security Property:** Idempotent saga rollbacks and strict isolation.

**Steps:**
1. The script fires a `PUT /v1/containment/agent/<ID>` payload with `level=HALT`.

**Expected Output:**
The agent state immediately updates to `HALT`. The terminal logs will show the saga reaper executing and reporting `orphaned_funds_minor: 0`.
*(Screenshot: `docs/screenshots/fleet_map_halt.png`)*

**What to Highlight:**
- Open the React **Dashboard Fleet Map**. Point out the specific agent instantly turning RED.
- Highlight the absolute lack of orphaned resources (funds locked mid-flight were cleanly released).

---

## Beat 4: The Herd (Behavioral Analytics)

**Goal:** Detect coordinated, subtle attacks across multiple agents.
**Security Property:** Continuous fleet-correlation and JSD drift detection.

**Steps:**
1. The script injects the poisoned data feed using `poison_feed.py`.
2. Wait ~5 seconds (or up to 2 minutes in production).

**Expected Output:**
The Analytics service identifies the cosine similarity crossing `0.95`. The terminal will log the cohort escalation to `THROTTLE` or `QUARANTINE`.
*(Screenshot: `docs/screenshots/grafana_fleet_risk.png`)*

**What to Highlight:**
- Open the Grafana **Fleet Risk** dashboard.
- Show the `aegis_fleet_correlation_index` spiking alongside the individual `aegis_drift_score` gauges.

---

## Beat 5: The Proof (Audit Chain Immutability)

**Goal:** Prove the system is tamper-evident, even against an insider with direct database access.
**Security Property:** SHA-256 hash-linking and cryptographic checkpointing.

**Steps:**
1. The script connects directly to Postgres and executes an `UPDATE` on `decision_records` altering a `reason_code`.
2. The script triggers `POST /v1/verify-chain`.

**Expected Output:**
The JSON response immediately highlights the failure:
```json
{
  "valid": false,
  "broken_at_seq": 3,
  "expected_hash": "...",
  "actual_hash": "..."
}
```

**What to Highlight:**
- AEGIS dynamically rebuilds the canonical payload based on the exact DB state. Because the `reason_code` changed, the recalculated hash fails to match the `payload_commitment` stored by the Gateway at execution time, irrevocably breaking the chain.

---

## Troubleshooting Appendix
- **Metrics not displaying in Grafana?** Ensure `prometheus` is successfully scraping the target ports (8083 for containment, 8085 for analytics) via `http://localhost:9090/targets`.
- **Database Connection Refused?** Ensure the Postgres container is healthy and the `POSTGRES_DSN` environment variable is identically formatted across all `.env` instances.
- **Demo Script stuck?** Run `make reset` to forcefully drop the volumes and recreate the clean execution state.

## Automation Notes
The primary demonstration is entirely orchestrated by `scripts/demo.sh`. Navigating to the Dashboards (React UI & Grafana) is manual, serving as visual reinforcement for the terminal assertions.
