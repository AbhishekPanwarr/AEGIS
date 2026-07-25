package store

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"aegis/pkg/apierr"
	"aegis/pkg/idgen"
	"aegis/pkg/telemetry"

	"github.com/jackc/pgx/v5"
)

type CreateDecisionRequest struct {
	AgentID      *string         `json:"agent_id"`
	MandateID    *string         `json:"mandate_id"`
	TokenID      *string         `json:"token_id"`
	ActionType   string          `json:"action_type"`
	FullContext  json.RawMessage `json:"full_context"`
}

type CreateDecisionResponse struct {
	DecisionID string `json:"decision_id"`
	Seq        int64  `json:"seq"`
}

type FinalizeRequest struct {
	Decision   string  `json:"decision"`
	ReasonCode string  `json:"reason_code"`
	LatencyMs  *int64  `json:"latency_ms"`
	RailResult *string `json:"rail_result,omitempty"`
}

type FinalizeResponse struct {
	DecisionID string `json:"decision_id"`
	ThisHash   string `json:"this_hash"`
}

type Store struct {
	conn   *pgx.Conn
	logger *telemetry.Logger
}

func New(ctx context.Context, logger *telemetry.Logger) (*Store, error) {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		return nil, fmt.Errorf("POSTGRES_DSN not set")
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}

	if err := conn.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	logger.Info("postgres_connected")
	return &Store{conn: conn, logger: logger}, nil
}

func (s *Store) Close(ctx context.Context) error {
	return s.conn.Close(ctx)
}

func (s *Store) HealthCheck(ctx context.Context) error {
	return s.conn.Ping(ctx)
}

func (s *Store) CreateStub(ctx context.Context, req CreateDecisionRequest) (CreateDecisionResponse, error) {
	decisionID := idgen.New()

	var agentID, mandateID, tokenID *string
	if req.AgentID != nil && *req.AgentID != "" {
		agentID = req.AgentID
	}
	if req.MandateID != nil && *req.MandateID != "" {
		mandateID = req.MandateID
	}
	if req.TokenID != nil && *req.TokenID != "" {
		tokenID = req.TokenID
	}

	var maskedPayload json.RawMessage = []byte("{}")
	if len(req.FullContext) > 0 {
		maskedPayload = req.FullContext
	}

	var seq int64

	err := s.conn.QueryRow(ctx, `
		INSERT INTO decision_records
		  (decision_id, prev_hash, this_hash, payload_commitment,
		   masked_payload, masked_paths,
		   agent_id, mandate_id, token_id,
		   action_type, policy_version_hash, decision, reason_code, latency_ms)
		VALUES
		  ($1, 'phase0', 'phase0', 'phase0',
		   $2::jsonb, NULL,
		   $3, $4, $5,
		   $6, NULL, 'PENDING_EXECUTE', 'PENDING', NULL)
		RETURNING seq
	`,
		decisionID,
		string(maskedPayload),
		agentID,
		mandateID,
		tokenID,
		req.ActionType,
	).Scan(&seq)

	if err != nil {
		return CreateDecisionResponse{}, fmt.Errorf("insert decision_records stub: %w", err)
	}

	s.logger.Info("stub_created", map[string]interface{}{
		"decision_id": decisionID,
		"seq":         seq,
		"action_type": req.ActionType,
	})

	return CreateDecisionResponse{DecisionID: decisionID, Seq: seq}, nil
}

func (s *Store) Finalize(ctx context.Context, decisionID string, req FinalizeRequest) (FinalizeResponse, error) {
	_, err := s.conn.Exec(ctx, `
		UPDATE decision_records
		SET decision = $2,
		    reason_code = $3,
		    latency_ms = $4,
		    this_hash = 'phase0',
		    payload_commitment = 'phase0',
		    prev_hash = 'phase0'
		WHERE decision_id = $1
	`, decisionID, req.Decision, req.ReasonCode, req.LatencyMs)
	if err != nil {
		return FinalizeResponse{}, fmt.Errorf("finalize decision: %w", err)
	}

	s.logger.Info("decision_finalized", map[string]interface{}{
		"decision_id": decisionID,
		"decision":    req.Decision,
		"reason_code": req.ReasonCode,
	})

	return FinalizeResponse{DecisionID: decisionID, ThisHash: "phase0"}, nil
}

func (s *Store) HandleCreateDecision(w http.ResponseWriter, r *http.Request) {
	var req CreateDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, "RAIL_ERROR",
			"invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}

	if req.ActionType == "" {
		apierr.WriteError(w, http.StatusBadRequest, "RAIL_ERROR", "action_type is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.CreateStub(ctx, req)
	if err != nil {
		s.logger.Error("create_stub_failed", map[string]interface{}{"error": err.Error()})
		apierr.WriteError(w, http.StatusInternalServerError, "RAIL_ERROR",
			"failed to write decision record", map[string]interface{}{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Store) HandleFinalize(w http.ResponseWriter, r *http.Request) {
	decisionID := r.PathValue("id")
	var req FinalizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, "RAIL_ERROR", "invalid JSON body")
		return
	}

	if req.Decision == "" || req.ReasonCode == "" {
		apierr.WriteError(w, http.StatusBadRequest, "RAIL_ERROR", "decision and reason_code are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.Finalize(ctx, decisionID, req)
	if err != nil {
		s.logger.Error("finalize_failed", map[string]interface{}{"error": err.Error(), "decision_id": decisionID})
		apierr.WriteError(w, http.StatusInternalServerError, "RAIL_ERROR",
			"failed to finalize decision", map[string]interface{}{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
