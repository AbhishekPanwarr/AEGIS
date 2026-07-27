# AEGIS — Agentic Execution Governance & Integrity System

> An enterprise-grade execution fabric that safely governs, observes, and mathematically constrains autonomous AI agents in production. Features distributed deterministic budgets, continuous JSD drift detection, and a cryptographically verifiable audit chain.

[![Tests](https://img.shields.io/badge/tests-passing-brightgreen)](./)
[![License](https://img.shields.io/badge/license-MIT-blue)](./LICENSE)
[![Go](https://img.shields.io/badge/go-1.22%2B-blue)](./)
[![Python](https://img.shields.io/badge/python-3.12%2B-blue)](./)
[![Cedar](https://img.shields.io/badge/policy-cedar-orange)](https://www.cedarpolicy.com/)

---

## 🌊 Core System — Production Ready

**AEGIS is now fully operational across all architectural phases.** This represents the culmination of our engineering effort, unifying cryptographic identity boundaries, ultra-low latency distributed ledgers, and tamper-evident auditing into a single robust fabric.

- **Idempotent 6-Stage Execution** — Every agent request traverses Preflight, Policy (Cedar), Identity (PKI), Budget (Lua), and Audit, safely returning `executed` or throwing a deterministic rejection (e.g. `MANDATE_SCOPE_VIOLATION`, `BUDGET_EXCEEDED`).
- **Mathematical Budget Caps** — Sub-millisecond distributed state execution via atomic Redis Lua scripts guarantees absolutely zero overshoot even during massive concurrent AI bursts.
- **Continuous Behavioral Analytics** — A high-performance Python analytics consumer continuously applies Jensen-Shannon Divergence (JSD) and EWMA tracking to agent streams, automatically escalating drift and fleet correlation anomalies into quarantine.
- **Cryptographic Audit Chain** — An immutable, SHA-256 hash-linked Postgres ledger checkpointed by Ed25519 signatures. Direct database tampering is mathematically detectable instantly via `verify-chain`.

---

## What is AEGIS?

AI agent deployment today lacks structural boundaries, offers no hard financial ceilings, and provides no immutable audit trails when agents go rogue. AEGIS fixes all three.

**The problem:**
- **No strict boundaries** — Agents operate with overly permissive API keys and can easily exceed their intended scopes or fall victim to prompt-injections.
- **No concurrency limits** — A compromised LLM in a loop can spend massive budgets instantaneously, outpacing traditional async rate limiters.
- **No systemic observability** — When an agent deviates from normal behavior, no system detects the drift until a catastrophic failure occurs.

**AEGIS solves this with:**
- **Cryptographic Mandates** dictating explicit, narrow boundaries for every agent interaction.
- **Atomic Budget Ledgers** that halt execution at exactly zero funds, mathematically immune to race conditions.
- **The Containment Ladder** (`OBSERVE` -> `THROTTLE` -> `QUARANTINE` -> `HALT`), automatically isolating rogue agents.
- **Tamper-Evident Audit Chains**, ensuring complete cryptographic certainty over the execution history.

In short: **AEGIS is the seatbelt for autonomous AI systems.**

---

## Architecture

![AEGIS Architecture](docs/assets/architecture.png)

> *See [docs/assets/architecture.excalidraw](docs/assets/architecture.excalidraw) for the editable source.*

### Data Flow

```text
┌─────────────────┐     ┌──────────────┐     ┌─────────────┐     ┌──────────────┐
│  Autonomous AI  │────▶│    Gateway   │────▶│   Identity  │     │   Budget     │
│      Agent      │     │  (Idempotency│     │   (Mandate  │     │  Ledger (Lua)│
└─────────────────┘     │   & AuthZ)   │     │  Verification)    │              │
                        └──────────────┘     └─────────────┘     └──────────────┘
                               │                                          
                               ▼                                          
                        ┌──────────────┐                           ┌──────────────┐
                        │ Cedar Policy │                           │ Audit Chain  │
                        │    Engine    │                           │ (Hash-Linked │
                        │  (Shadowing) │                           │  Postgres)   │
                        └──────────────┘                           └──────┬───────┘
                                                                          │
                                                                          ▼
                        ┌──────────────────────────────────────────────────┐
                        │             Containment & Intelligence           │
                        │  ┌─────────────┐  ┌─────────────┐  ┌───────────┐ │
                        │  │  Behavior   │  │ Containment │  │  Recovery │ │
                        │  │  Analytics  │  │ Controller  │  │   Cases   │ │
                        │  │ (JSD/EWMA)  │  │ (Laddering) │  │ (Approval)│ │
                        │  └─────────────┘  └─────────────┘  └───────────┘ │
                        └──────────────────────────────────────────────────┘
```

### Components

| Layer | Component | Role |
|-------|-----------|------|
| **Ingress** | Gateway (`gateway`) | 6-Stage execution pipeline, idempotency guards, and shadow-policy routing |
| **Auth** | Cedar Engine (`cedar-agent`) | External AWS Cedar policy evaluator for dynamic structural linting |
| **Identity** | Identity Service (`identity-service`) | PKI token minting, caveat attenuation, and mandate revocation |
| **Finance** | Budget Ledger (`budget-ledger`) | Distributed atomic state execution via Lua scripts on Redis |
| **Persistence**| Audit Chain (`audit-chain`) | SHA-256 hash-linking, context snapshotting, and chain verification |
| **Intelligence**| Behavior Analytics (`behavior-analytics`) | FastAPI/Python worker tracking JSD drift and cosine-similarity fleet correlations |
| **Control** | Containment Controller (`containment-controller`) | Modifies agent states, coordinates quarantine approvals, and orchestrates emergency Halts |
| **Observability**| Prometheus/Grafana | Live telemetry ingestion spanning orphaned funds, drift metrics, and fleet coordination |

---

## Quick Start

### 1. Booting the Stack

```bash
git clone https://github.com/aegis-org/aegis.git && cd aegis

# Start the full 14-container stack
make up

# Wait ~10 seconds for databases and services to initialize, then seed the data
make seed
```

### 2. Simulating the Prototype

To comprehensively see AEGIS in action, run the fully automated 7-minute demonstration.

```bash
# Runs the full narrative sequence
make demo
```

The script will walk you through 5 critical scenarios:
1. **The Attack:** Injecting a malicious payload and witnessing Mandate rejections.
2. **The Race:** Hammering the budget ledger with concurrency and proving 0 overshoot.
3. **The Halt:** Automatically triggering an emergency agent freeze and visualizing the state.
4. **The Herd:** Firing a poisoned feed and detecting fleet-correlation anomalies.
5. **The Proof:** Tampering with the Postgres backend and mathematically detecting the break.

### 3. Manual Testing & Dashboards

If you prefer to drive manually, you can execute individual attack simulations:
```bash
# 1. Fire a race condition attack
bash scripts/run_race_benchmark.sh

# 2. Inject a poisoned feed
python3 simulation/agent-sim/app/attacks/poison_feed.py

# 3. Halt an agent manually
curl -X PUT http://localhost:8083/v1/containment/agent/00000000-0000-0000-0000-000000000010 \
  -H "Content-Type: application/json" \
  -d '{"level":"HALT","reason":"manual stop","actor":"operator"}'
```

**Accessing Telemetry:**
- **Prometheus:** `http://localhost:9090`
- **Grafana Dashboards:** `http://localhost:3000` (User: `admin`, Pass: `admin`)
  - **Enforcement Dashboard:** Tracks latency and denial rates.
  - **Budget Velocity:** Monitors headroom and exception rates.
  - **Fleet Risk:** Visualizes `aegis_drift_score` and `aegis_fleet_correlation_index`.

> *Pro tip: Need to capture screenshots for a report? Ensure `puppeteer` is installed via `npm i puppeteer`, then run `node scripts/capture_screenshots.js` to automatically extract the Grafana and Dashboard visualisations into `docs/screenshots/`.*

---

## Feature Deep Dive: Visual Evidence

### The Race Condition (Budget Ledger)
AEGIS mathematically guarantees that no concurrent loop can breach the allotted capacity. Below is the side-by-side output comparing naive logic versus AEGIS executing under maximum duress.
![Budget Enforcement](docs/screenshots/grafana_enforcement.png)

### Emergency Halt & Fleet Map
When an agent deviates into dangerous territory, the `containment-controller` pushes them to a `HALT` state, freezing API keys and spinning up a manual `Recovery Case`.
![Fleet Map Halt](docs/screenshots/fleet_map_halt.png)

### The Tamper-Evident Audit Chain
When a direct database modification is performed, `verify-chain` automatically recalculates the canonical payload and flags the sequence anomaly.
```json
{
  "valid": false,
  "broken_at_seq": 3,
  "expected_hash": "e3b0c44298fc1c149afbf4c8996fb924...",
  "actual_hash": "f45bc837d9afce620b..."
}
```

---

## Repository Structure

```text
AEGIS/
├── analytics/
│   └── behavior-analytics/    # JSD Drift, EWMA tracking, Fleet Correlation (Python)
├── pkg/                       # Shared Go utilities (API, Config, JWT, Budget Lua)
├── scripts/                   # Automated demo, benchmarks, and chaos suite
├── services/
│   ├── audit-chain/           # Immutable execution ledger (Go)
│   ├── budget-ledger/         # Redis-backed distributed capacity (Go)
│   ├── containment-controller/# Ladder management and Saga sweeps (Go)
│   ├── gateway/               # 6-stage execution router (Go)
│   ├── identity-service/      # PKI, Mandates, and Token distribution (Go)
│   └── mock-rails/            # Simulated financial settlement
├── simulation/
│   └── agent-sim/             # E2E load generation and attack scripts
├── tests/
│   └── golden/                # 43+ E2E integration tests
├── observability/             # Prometheus config and Grafana declarative dashboards
└── Makefile                   # Core automation
```

---

## Documentation

- **Demo Guide**: [DEMO_GUIDE.md](./DEMO_GUIDE.md) — The exact narrative structure and execution steps for the live demonstration.
- **Benchmarks**: [BENCHMARKS.md](./BENCHMARKS.md) — Verified metrics on p99 latency, exact concurrency, and anomaly detection speeds.
- **Submission Details**: [SUBMISSION.md](./SUBMISSION.md) — Phase 4 delivery sign-offs.

---

## License

MIT — see [LICENSE](./LICENSE)
