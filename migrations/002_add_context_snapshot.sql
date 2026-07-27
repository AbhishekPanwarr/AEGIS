-- migrations/002_add_context_snapshot.sql
ALTER TABLE decision_records ADD COLUMN IF NOT EXISTS context_snapshot JSONB;
