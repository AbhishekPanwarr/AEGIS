-- migrations/001_init.sql
-- AEGIS complete schema for the prototype, applied as a single migration.
-- Every table from Section 5.1 of the implementation plan, verbatim.

CREATE EXTENSION IF NOT EXISTS pgcrypto;  -- for gen_random_uuid()

CREATE TABLE agents (
  id UUID PRIMARY KEY,
  group_id UUID NOT NULL,
  persona TEXT NOT NULL,
  pubkey BYTEA NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',        -- active | disabled
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_by TEXT NOT NULL
);

CREATE TABLE mandates (
  id UUID PRIMARY KEY,
  issuer TEXT NOT NULL,
  purpose_code TEXT NOT NULL,
  counterparty_scope JSONB NOT NULL,
  ceiling_minor BIGINT NOT NULL,
  currency TEXT NOT NULL DEFAULT 'USD',
  valid_from TIMESTAMPTZ NOT NULL,
  valid_to TIMESTAMPTZ NOT NULL,
  max_depth INT NOT NULL DEFAULT 1,
  recurrence JSONB,
  nonce TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'live',           -- live | revoked | expired
  signer_pubkey BYTEA NOT NULL,
  signature BYTEA NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  revoked_at TIMESTAMPTZ,
  revoked_reason TEXT
);

CREATE TABLE budget_nodes (
  id UUID PRIMARY KEY,
  parent_id UUID REFERENCES budget_nodes(id),
  label TEXT NOT NULL,
  currency TEXT NOT NULL DEFAULT 'USD',
  cap_minor BIGINT NOT NULL,
  velocity_rules JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_by TEXT
);
-- committed / reserved live in Redis at runtime for speed (see 5.2).
-- this table is the durable settlement ledger, one row per state change:
CREATE TABLE budget_ledger_events (
  id BIGSERIAL PRIMARY KEY,
  reservation_id UUID NOT NULL,
  node_id UUID NOT NULL REFERENCES budget_nodes(id),
  event_type TEXT NOT NULL,                      -- RESERVE | COMMIT | RELEASE | EXPIRE
  amount_minor BIGINT NOT NULL,
  idempotency_key TEXT NOT NULL,
  saga_id UUID,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sagas (
  id UUID PRIMARY KEY,
  saga_type TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'RUNNING',          -- RUNNING | COMPLETED | COMPENSATING
                                                   -- | COMPENSATED | MANUAL_RESOLVE
  point_of_no_return_leg INT,
  legs JSONB NOT NULL,
  reservation_id UUID,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE containment_state (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  scope_type TEXT NOT NULL,                       -- fleet | group | agent | mandate
  scope_id TEXT NOT NULL,                         -- 'ALL' for fleet
  level TEXT NOT NULL,                            -- OBSERVE | THROTTLE | QUARANTINE | HALT
  reason TEXT,
  actor TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (scope_type, scope_id)
);

CREATE TABLE containment_events (
  id BIGSERIAL PRIMARY KEY,
  scope_type TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  from_level TEXT,
  to_level TEXT NOT NULL,
  reason TEXT,
  actor TEXT NOT NULL,
  epoch_bumped_to BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE recovery_cases (
  id UUID PRIMARY KEY,
  agent_id UUID NOT NULL,
  halt_event_id BIGINT REFERENCES containment_events(id),
  replay_result JSONB,
  adversarial_result JSONB,
  drift_trend JSONB,
  status TEXT NOT NULL DEFAULT 'PENDING',         -- PENDING | READY | APPROVED | REJECTED
  approver_1 TEXT,
  approver_1_at TIMESTAMPTZ,
  approver_2 TEXT,
  approver_2_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE policy_versions (
  version_hash TEXT PRIMARY KEY,                  -- sha256 of the cedar source
  cedar_source TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'DRAFT',            -- DRAFT | SHADOW | CANARY | ACTIVE | RETIRED
  author TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  activated_at TIMESTAMPTZ
);

CREATE TABLE decision_records (
  seq BIGSERIAL PRIMARY KEY,
  decision_id UUID NOT NULL UNIQUE,
  prev_hash TEXT NOT NULL,
  this_hash TEXT NOT NULL,
  payload_commitment TEXT NOT NULL,               -- sha256 of the canonical UNMASKED payload
  masked_payload JSONB NOT NULL,                  -- redacted view actually queried by the UI
  masked_paths JSONB,
  agent_id UUID,
  mandate_id UUID,
  token_id UUID,
  action_type TEXT NOT NULL,
  policy_version_hash TEXT,
  decision TEXT NOT NULL,                          -- PENDING_EXECUTE | ALLOW | DENY
  reason_code TEXT NOT NULL,
  latency_ms NUMERIC,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_decision_records_agent ON decision_records(agent_id, created_at);

CREATE TABLE audit_checkpoints (
  id BIGSERIAL PRIMARY KEY,
  up_to_seq BIGINT NOT NULL,
  checkpoint_hash TEXT NOT NULL,
  signature BYTEA NOT NULL,
  signer_pubkey BYTEA NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE approval_requests (
  id UUID PRIMARY KEY,
  decision_context JSONB NOT NULL,
  agent_id UUID NOT NULL,
  requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  state TEXT NOT NULL DEFAULT 'PENDING',          -- PENDING | APPROVED | DENIED | VOIDED
  approver TEXT,
  decided_at TIMESTAMPTZ,
  rationale_code TEXT
);

CREATE TABLE risk_events (
  id BIGSERIAL PRIMARY KEY,
  agent_id UUID NOT NULL,
  feature TEXT NOT NULL,                          -- action_type | counterparty | amount
                                                    -- | timing | correlation
  score NUMERIC NOT NULL,
  ewma_score NUMERIC,
  threshold NUMERIC,
  low_confidence BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE drift_thresholds (
  persona TEXT NOT NULL,
  feature TEXT NOT NULL,
  threshold NUMERIC NOT NULL,
  sample_size INT NOT NULL,
  calibrated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (persona, feature)
);
