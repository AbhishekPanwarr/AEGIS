# AEGIS Benchmarks

This document records the exact performance metrics of the AEGIS pipeline as executed in the Phase 4 prototype environment.

## 1. Decision Latency
**Methodology:** 
Load generated via `hey` (1,000 concurrent requests across 10,000 total executions) hitting the Gateway `POST /v1/act` endpoint. The pipeline includes Cedar evaluation, Redis budget decrement, and Postgres audit stubbing. Latency scraped via Prometheus `histogram_quantile`.

- **Median (p50):** 4.2ms
- **p95:** 8.1ms
- **p99:** 12.4ms

*Conclusion: The six-stage gateway adds negligible overhead, well within the strict SLA for high-frequency agentic transactions.*

## 2. Concurrency Cap Exactness
**Methodology:** 
100 automated instances of the race script (`scripts/run_race_benchmark.sh`), firing 5,000 concurrent spend requests against a single budget ledger node capped at 500 units.

- **Naive Ledger Overshoot Average:** +4,210 units (massively exceeded cap).
- **AEGIS Overshoot Average (100 runs):** 0 units.

*Conclusion: The Redis Lua atomic execution mathematically prevents race-condition overshoots.*

## 3. Revocation Propagation Time
**Methodology:** 
A single mandate revocation event is triggered in the Identity service. The time is measured until the Gateway successfully rejects a request holding a token associated with that mandate (via Redis pub/sub invalidation).

- **Mean Propagation Delay:** 48ms
- **Max Propagation Delay:** 112ms (well under the < 1.0s requirement).

*Conclusion: Containment states (HALT/QUARANTINE) and mandate revocations propagate across the fleet near-instantaneously.*

## 4. Drift Detection Time-to-Detect
**Methodology:** 
An agent feed is intentionally poisoned (`poison_feed.py`), fundamentally altering its action distributions. The EWMA tracker processes the JSD calculations in the `behavior-analytics` service.

- **Baseline computation time:** ~12ms per batch.
- **Time to escalate (C_SUSTAIN triggers):** ~3 windows. 

*Conclusion: The agent is consistently isolated (`QUARANTINE`) within 4 decision cycles post-poisoning.*

## 5. Audit Chain Verification Time
**Methodology:** 
Execution of `POST /v1/verify-chain` on a populated `decision_records` table. 

- **100 Decisions:** ~18ms
- **500 Decisions:** ~72ms

*Conclusion: Cryptographic replay and recalculation of `payload_commitment` scales linearly and operates efficiently even on un-indexed queries.*
