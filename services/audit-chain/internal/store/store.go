package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"crypto/ed25519"

	"aegis/pkg/apierr"
	"aegis/pkg/idgen"
	"aegis/pkg/telemetry"
	"aegis/services/audit-chain/internal/chain"
	"aegis/services/audit-chain/internal/masking"

	"github.com/jackc/pgx/v5"
)

type CreateDecisionRequest struct {
	AgentID           *string         `json:"agent_id"`
	MandateID         *string         `json:"mandate_id"`
	TokenID           *string         `json:"token_id"`
	ActionType        string          `json:"action_type"`
	PolicyVersionHash string          `json:"policy_version_hash"`
	FullContext       json.RawMessage `json:"full_context"`
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

type VerifyChainRequest struct {
	FromSeq *int64 `json:"from_seq,omitempty"`
	ToSeq   *int64 `json:"to_seq,omitempty"`
}

type VerifyChainResponse struct {
	Valid        bool   `json:"valid"`
	BrokenAtSeq  int64  `json:"broken_at_seq,omitempty"`
	ExpectedHash string `json:"expected_hash,omitempty"`
	ActualHash   string `json:"actual_hash,omitempty"`
}

type ReplayResponse struct {
	OriginalDecision string `json:"original_decision"`
	ReplayedDecision string `json:"replayed_decision"`
	Match            bool   `json:"match"`
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

	logger.Info("postgres_connected", nil)
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

	var prevHash string
	err := s.conn.QueryRow(ctx, "SELECT this_hash FROM decision_records ORDER BY seq DESC LIMIT 1").Scan(&prevHash)
	if err != nil {
		if err == pgx.ErrNoRows {
			prevHash = "genesis"
		} else {
			return CreateDecisionResponse{}, fmt.Errorf("get prev hash: %w", err)
		}
	}

	var contextSnapshot json.RawMessage = []byte("{}")
	if len(req.FullContext) > 0 {
		contextSnapshot = req.FullContext
	}

	var seq int64
	err = s.conn.QueryRow(ctx, `
		INSERT INTO decision_records
		  (decision_id, prev_hash, this_hash, payload_commitment,
		   masked_payload, masked_paths,
		   agent_id, mandate_id, token_id,
		   action_type, policy_version_hash, decision, reason_code, latency_ms, context_snapshot)
		VALUES
		  ($1, $2, 'pending', 'pending',
		   '{}'::jsonb, NULL,
		   $3, $4, $5,
		   $6, $7, 'PENDING_EXECUTE', 'PENDING', NULL, $8)
		RETURNING seq
	`,
		decisionID, prevHash,
		req.AgentID, req.MandateID, req.TokenID,
		req.ActionType, req.PolicyVersionHash, contextSnapshot,
	).Scan(&seq)

	if err != nil {
		return CreateDecisionResponse{}, fmt.Errorf("insert decision_records stub: %w", err)
	}

	s.logger.Info("stub_created", map[string]interface{}{
		"decision_id": decisionID,
		"seq":         seq,
	})

	return CreateDecisionResponse{DecisionID: decisionID, Seq: seq}, nil
}

func (s *Store) Finalize(ctx context.Context, decisionID string, req FinalizeRequest) (FinalizeResponse, error) {
	var prevHash, policyVersionHash, actionType string
	var agentID, mandateID, tokenID *string
	var contextSnapshot json.RawMessage

	err := s.conn.QueryRow(ctx, `
		SELECT prev_hash, agent_id, mandate_id, token_id, action_type, COALESCE(policy_version_hash, ''), context_snapshot
		FROM decision_records
		WHERE decision_id = $1
	`, decisionID).Scan(&prevHash, &agentID, &mandateID, &tokenID, &actionType, &policyVersionHash, &contextSnapshot)
	if err != nil {
		return FinalizeResponse{}, fmt.Errorf("find stub: %w", err)
	}

	payload := chain.CanonicalPayload{
		DecisionID:        decisionID,
		AgentID:           agentID,
		MandateID:         mandateID,
		TokenID:           tokenID,
		ActionType:        actionType,
		PolicyVersionHash: policyVersionHash,
		Decision:          req.Decision,
		ReasonCode:        req.ReasonCode,
		Context:           contextSnapshot,
	}
	
	payloadCommitment := chain.ComputeCommitment(payload)
	masked, maskedPaths := masking.Apply(contextSnapshot)
	pathsJSON, _ := json.Marshal(maskedPaths)
	thisHash := chain.ComputeThisHash(prevHash, payloadCommitment, policyVersionHash)

	_, err = s.conn.Exec(ctx, `
		UPDATE decision_records
		SET decision = $2,
		    reason_code = $3,
		    latency_ms = $4,
		    this_hash = $5,
		    payload_commitment = $6,
		    masked_payload = $7,
		    masked_paths = $8
		WHERE decision_id = $1
	`, decisionID, req.Decision, req.ReasonCode, req.LatencyMs, thisHash, payloadCommitment, string(masked), string(pathsJSON))
	
	if err != nil {
		return FinalizeResponse{}, fmt.Errorf("finalize update: %w", err)
	}

	return FinalizeResponse{DecisionID: decisionID, ThisHash: thisHash}, nil
}

func (s *Store) VerifyChain(ctx context.Context, fromSeq, toSeq *int64) (VerifyChainResponse, error) {
	query := `SELECT seq, decision_id, agent_id, mandate_id, token_id, action_type, decision, reason_code, context_snapshot, prev_hash, this_hash, payload_commitment, COALESCE(policy_version_hash, '') FROM decision_records`
	args := []interface{}{}
	if fromSeq != nil && toSeq != nil {
		query += " WHERE seq >= $1 AND seq <= $2"
		args = append(args, *fromSeq, *toSeq)
	}
	query += " ORDER BY seq ASC"

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return VerifyChainResponse{}, err
	}
	defer rows.Close()

	var lastThisHash string

	for rows.Next() {
		var seq int64
		var decisionID, actionType, decision, reasonCode, prevHash, thisHash, payloadCommitment, policyVersionHash string
		var agentID, mandateID, tokenID *string
		var contextSnapshot json.RawMessage
		
		if err := rows.Scan(&seq, &decisionID, &agentID, &mandateID, &tokenID, &actionType, &decision, &reasonCode, &contextSnapshot, &prevHash, &thisHash, &payloadCommitment, &policyVersionHash); err != nil {
			return VerifyChainResponse{}, err
		}

		if lastThisHash != "" && prevHash != lastThisHash {
			return VerifyChainResponse{Valid: false, BrokenAtSeq: seq, ExpectedHash: lastThisHash, ActualHash: prevHash}, nil
		}
		
		if thisHash == "pending" {
			continue // skip unfinalized stubs
		}

		expectedPayloadCommitment := chain.ComputeCommitment(chain.CanonicalPayload{
			DecisionID:        decisionID,
			AgentID:           agentID,
			MandateID:         mandateID,
			TokenID:           tokenID,
			ActionType:        actionType,
			PolicyVersionHash: policyVersionHash,
			Decision:          decision,
			ReasonCode:        reasonCode,
			Context:           contextSnapshot,
		})
		
		if expectedPayloadCommitment != payloadCommitment {
			return VerifyChainResponse{Valid: false, BrokenAtSeq: seq, ExpectedHash: expectedPayloadCommitment, ActualHash: payloadCommitment}, nil
		}

		expectedThisHash := chain.ComputeThisHash(prevHash, payloadCommitment, policyVersionHash)
		if expectedThisHash != thisHash {
			return VerifyChainResponse{Valid: false, BrokenAtSeq: seq, ExpectedHash: expectedThisHash, ActualHash: thisHash}, nil
		}
		
		lastThisHash = thisHash
	}

	return VerifyChainResponse{Valid: true}, nil
}

func (s *Store) Replay(ctx context.Context, decisionID string) (ReplayResponse, error) {
	var originalDecision, reasonCode string
	var agentID, mandateID, actionType string
	var contextSnapshot []byte
	err := s.conn.QueryRow(ctx, "SELECT decision, reason_code, agent_id, mandate_id, action_type, context_snapshot FROM decision_records WHERE decision_id = $1", decisionID).Scan(&originalDecision, &reasonCode, &agentID, &mandateID, &actionType, &contextSnapshot)
	if err != nil {
		return ReplayResponse{}, err
	}
	
	var actReq struct {
		CounterpartyID string `json:"counterparty_id"`
		AmountMinor    int64  `json:"amount_minor"`
	}
	_ = json.Unmarshal(contextSnapshot, &actReq)

	cedarReq := map[string]interface{}{
		"principal": fmt.Sprintf(`Agent::"%s"`, agentID),
		"action":    fmt.Sprintf(`Action::"%s"`, actionType),
		"resource":  fmt.Sprintf(`Counterparty::"%s"`, actReq.CounterpartyID),
		"context": map[string]interface{}{
			"amount_minor": actReq.AmountMinor,
			"mandate_id":   mandateID,
		},
	}
	
	importBytes, _ := json.Marshal(cedarReq)
	httpReq, _ := http.NewRequest(http.MethodPost, "http://cedar-agent:8180/v1/is_authorized", bytes.NewBuffer(importBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	
	replayedDecision := "DENY" // Default fail-closed
	resp, err := http.DefaultClient.Do(httpReq)
	if err == nil {
		defer resp.Body.Close()
		var cResp map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&cResp); err == nil {
			if d, ok := cResp["decision"].(string); ok {
				replayedDecision = d
			}
		}
	}
	
	// Format of decisions from cedar: "Allow" or "Deny". DB stores "ALLOW" or "DENY"
	importDecision := "DENY"
	if replayedDecision == "Allow" {
		importDecision = "ALLOW"
	}
	
	return ReplayResponse{OriginalDecision: originalDecision, ReplayedDecision: importDecision, Match: originalDecision == importDecision}, nil
}

func (s *Store) HandleCreateDecision(w http.ResponseWriter, r *http.Request) {
	var req CreateDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, "RAIL_ERROR", "invalid json", nil)
		return
	}
	resp, err := s.CreateStub(r.Context(), req)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, "RAIL_ERROR", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

func (s *Store) HandleFinalize(w http.ResponseWriter, r *http.Request) {
	var req FinalizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, "RAIL_ERROR", "invalid json", nil)
		return
	}
	resp, err := s.Finalize(r.Context(), r.PathValue("id"), req)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, "RAIL_ERROR", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Store) HandleGetDecisions(w http.ResponseWriter, r *http.Request) {
	rows, err := s.conn.Query(r.Context(), "SELECT masked_payload FROM decision_records ORDER BY seq DESC LIMIT 100")
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, "RAIL_ERROR", err.Error(), nil)
		return
	}
	defer rows.Close()
	
	var results []json.RawMessage
	for rows.Next() {
		var m json.RawMessage
		rows.Scan(&m)
		results = append(results, m)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func (s *Store) HandleVerifyChain(w http.ResponseWriter, r *http.Request) {
	var req VerifyChainRequest
	if r.ContentLength > 0 {
		json.NewDecoder(r.Body).Decode(&req)
	}
	resp, err := s.VerifyChain(r.Context(), req.FromSeq, req.ToSeq)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, "RAIL_ERROR", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Store) HandleReplay(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Replay(r.Context(), r.PathValue("id"))
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, "RAIL_ERROR", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Store) Checkpoint(ctx context.Context) error {
	var seq int64
	var thisHash string
	err := s.conn.QueryRow(ctx, "SELECT seq, this_hash FROM decision_records ORDER BY seq DESC LIMIT 1").Scan(&seq, &thisHash)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil
		}
		return err
	}

	keyPath := os.Getenv("CHECKPOINT_SIGNING_KEY_PATH")
	if keyPath == "" {
		keyPath = "/tmp/checkpoint_signing_key"
	}
	
	var privKey ed25519.PrivateKey
	data, err := os.ReadFile(keyPath)
	if err != nil {
		_, priv, err2 := ed25519.GenerateKey(nil)
		if err2 != nil {
			return err2
		}
		privKey = priv
		_ = os.WriteFile(keyPath, privKey, 0600)
	} else {
		privKey = ed25519.PrivateKey(data)
	}

	pubKey := privKey.Public().(ed25519.PublicKey)
	sig := ed25519.Sign(privKey, []byte(thisHash))

	_, err = s.conn.Exec(ctx, `
		INSERT INTO audit_checkpoints (up_to_seq, checkpoint_hash, signature, signer_pubkey)
		VALUES ($1, $2, $3, $4)
	`, seq, thisHash, sig, []byte(pubKey))
	return err
}

func (s *Store) HandleCheckpoint(w http.ResponseWriter, r *http.Request) {
	err := s.Checkpoint(r.Context())
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, "RAIL_ERROR", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusOK)
}
