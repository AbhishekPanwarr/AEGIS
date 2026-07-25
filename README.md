# AEGIS

**Agent Enforcement, Governance, and Intervention System**

AEGIS is a governance framework for autonomous AI agents that execute financial transactions. It enforces a six-stage pipeline — identity, containment, policy, budget, execution, audit — that every agent action must pass through before any money moves.

The system is built as a working prototype per a fixed implementation plan. It demonstrates:

- **Capability tokens** with Ed25519 signing, epoch-based revocation, and caveat attenuation
- **Mandate binding** — an explicit second gate that checks counterparty scope and amount ceilings, separate from Cedar policy decisions
- **Atomic budget reservations** via Redis Lua scripts — 1,000 concurrent requests against a shared cap land at exactly the cap, every time
- **Containment ladder** (OBSERVE → THROTTLE → QUARANTINE → HALT) with epoch bumps and sub-second revocation propagation
- **Saga compensation** — emergency stop mid-transaction triggers automatic rollback of completed legs
- **Tamper-evident audit chain** — two-phase write pattern (pre-execute stub + finalize) ensures no action goes unrecorded
- **Operator dashboard** with fleet map, budget management, containment controls, and approval queue

---

## Architecture

### Services

| Service | Tech | Port | Role |
|---|---|---|---|
| postgres | PostgreSQL 16 | 5432 | Durable store — mandates, budgets, sagas, containment, decisions |
| redis | Redis 7 | 6379 | Fast state — epoch counters, budget reservations, velocity, pub/sub |
| cedar-agent | permitio/cedar-agent | 8180 | Cedar policy store + authorization decisions |
| identity-service | Go 1.22 | 8081 | Mandates, Ed25519 token mint/attenuate/renew |
| budget-ledger | Go 1.22 | 8082 | Atomic budget reservations (Lua scripts), velocity checks |
| containment-controller | Go 1.22 | 8083 | Containment ladder, epoch bumps, saga compensation, approvals |
| audit-chain | Go 1.22 | 8084 | Two-phase decision records with hash chain |
| gateway | Go 1.22 | 8080 | Policy enforcement point — orchestrates the six-stage pipeline |
| mock-rails | Go 1.22 | 8090 | Stub payment rail with saga failure injection |
| dashboard | React + Vite | 5173 | Operator UI — fleet map, budgets, containment, approvals |
| agent-sim | Python 3.12 | — | Simulated fleet, attack harness |

### Six-stage pipeline

Every `POST /v1/act` request flows through these stages in order. Any stage can short-circuit with a denial — later stages are never called.

1. **Identity** — Verify Ed25519 signature on the capability token. Check expiry. Check token epochs against the local epoch cache (rejects with `EPOCH_STALE` if the token was minted before a revocation).
2. **Containment** — Fetch the current containment level for the agent. `HALT` denies immediately. `QUARANTINE` or `requires_approval` caveat routes to the approval queue (202). `THROTTLE` enforces velocity cap and new-counterparty escalation. `OBSERVE` passes through.
3. **Policy** — Call cedar-agent for authorization. Then check the live mandate's counterparty scope and amount ceiling (the explicit second gate — Cedar can say Allow but AEGIS can still say Deny).
4. **Budget** — Atomic Redis Lua script reserves the amount on every node in the budget path. Two-pass check-then-set inside a single Lua script guarantees `committed + reserved ≤ cap` under any concurrency.
5. **Execute** — Call mock-rails. On failure, release the reservation and finalize audit with `RAIL_ERROR`.
6. **Audit** — Finalize the pre-execute stub with the decision and latency. The stub was written before Stage 4, so even a crash between budget and execution leaves evidence.

---

## Quick start

```bash
# Build and start all 11 containers
make up

# Seed demo data: 4 agents, 4 mandates, budget tree, Cedar policies, containment states
make seed
```

Once the stack is up:

- **Gateway API**: http://localhost:8080
- **Dashboard**: http://localhost:5173
- **Cedar-agent**: http://localhost:8180
- **Postgres**: localhost:5432 (user: aegis, pass: aegis, db: aegis)

### Send an action through the pipeline

```bash
# Get a mandate ID from the seeded data
MANDATE_ID=$(docker compose exec -T postgres psql -U aegis -d aegis -t -A -c \
  "SELECT id FROM mandates WHERE status='live' LIMIT 1")

# Get the root budget node ID
BUDGET_NODE=$(curl -s http://localhost:8082/v1/budget-node-by-label/root | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")

# Mint a capability token
TOKEN=$(curl -s -X POST http://localhost:8081/v1/tokens/mint \
  -H "Content-Type: application/json" \
  -d "{\"agent_id\":\"00000000-0000-0000-0000-000000000010\",\"mandate_id\":\"$MANDATE_ID\",\"caveats\":[]}")

# Send a payment through the full six-stage pipeline
curl -s -X POST http://localhost:8080/v1/act \
  -H "Content-Type: application/json" \
  -d "{\"token\":\"$TOKEN\",\"action_type\":\"SendPayment\",\"counterparty_id\":\"vendor_x\",\"amount_minor\":100,\"idempotency_key\":\"test-1\",\"budget_path\":[\"$BUDGET_NODE\"]}"
```

### Try an out-of-scope counterparty

```bash
# Same token, but counterparty "vendor_z" is not in the mandate's allowlist
curl -s -X POST http://localhost:8080/v1/act \
  -H "Content-Type: application/json" \
  -d "{\"token\":\"$TOKEN\",\"action_type\":\"SendPayment\",\"counterparty_id\":\"vendor_z\",\"amount_minor\":100,\"idempotency_key\":\"test-2\",\"budget_path\":[\"$BUDGET_NODE\"]}"
# → 403 MANDATE_SCOPE_VIOLATION
```

### Emergency stop

```bash
# Halt a specific agent
curl -s -X POST http://localhost:8083/v1/emergency-stop \
  -H "Content-Type: application/json" \
  -d '{"scope_type":"agent","scope_id":"00000000-0000-0000-0000-000000000010","reason":"demo","actor":"operator"}'

# Next action from that agent → 403 CONTAINMENT_HALT or EPOCH_STALE
```

---

## Tests

```bash
# Unit tests (no Docker needed)
go test ./pkg/...

# Golden tests — 43 cases covering mandate scope, expiry, budget cap, policy denial, etc.
# Requires a running, seeded stack.
go test ./tests/golden/... -v -timeout 120s

# Control tests — 6 cases: emergency stop, throttle, quarantine, saga compensation
go test ./tests/control/... -v -timeout 120s

# Race benchmark — 1000 concurrent reservations × 10 runs, must hit exact cap
make race-bench

# Revocation latency — click-to-first-deny must be under 1 second
make revocation-bench
```

---

## Makefile targets

| Target | Description |
|---|---|
| `make up` | Build and start all containers |
| `make down` | Stop all containers |
| `make seed` | Populate demo data (agents, mandates, budgets, Cedar, containment) |
| `make test` | Run Go unit tests for all services |
| `make race-bench` | Run the 1000-concurrent-request race benchmark |
| `make revocation-bench` | Measure emergency-stop-to-first-denial latency |
| `make logs` | Follow all container logs |

---

## Repository structure

```
aegis/
  docker-compose.yml
  go.work                         # Go workspace tying all modules together
  Makefile
  .env.example
  
  migrations/
    001_init.sql                  # Full PostgreSQL schema (14 tables)
  
  pkg/                            # Shared Go packages
    capability/                   # Token + mandate structs, Ed25519 sign/verify
    apierr/                       # Standard error envelope (Section 4.4)
    telemetry/                    # Structured JSON logger
    idgen/                        # UUIDv4 helpers
  
  services/
    gateway/                      # Six-stage pipeline enforcement point
    identity-service/             # Mandates, token mint/attenuate/renew
    budget-ledger/                # Lua-script atomic reservations, velocity, reaper
    containment-controller/       # Ladder, epochs, sagas, approvals
    audit-chain/                  # Two-phase decision records
    mock-rails/                   # Stub payment rail with failure injection
  
  policies/cedar/                 # Cedar schema + per-persona policies
  simulation/agent-sim/           # One-shot action sender
  dashboard/                      # React + Vite + TypeScript operator UI
  scripts/                        # Seed data, revocation benchmark
  tests/
    golden/                       # 43 golden test cases
    race/                         # Concurrency benchmarks
    control/                      # Phase 2 control-plane tests
  observability/                  # Prometheus/Grafana config (reserved)
```

---

## Implementation phases

This prototype was built in three phases, each ending with a runnable system:

- **Phase 0 — Walking Skeleton**: Docker Compose, gateway with stubbed stages, mock-rails, audit insert. One action end-to-end.
- **Phase 1 — The Spine**: Real Ed25519 tokens, Cedar policy enforcement, atomic budget reservations, mandate scope checks, two-phase audit. 43 golden tests. 100-concurrent race test.
- **Phase 2 — Control**: Full containment ladder, epoch-based revocation with pub/sub, saga registration and compensation, dashboard v1, 1000-concurrent race benchmark, revocation latency benchmark.

---

## License

MIT
