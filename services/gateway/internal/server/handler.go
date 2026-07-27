package server

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"aegis/pkg/apierr"
	"aegis/pkg/capability"
	"aegis/pkg/telemetry"
	"aegis/services/gateway/internal/clients"
	"aegis/services/gateway/internal/epoch"

	"github.com/redis/go-redis/v9"
)

type ActRequest struct {
	Token          string   `json:"token"`
	ActionType     string   `json:"action_type"`
	CounterpartyID string   `json:"counterparty_id"`
	AmountMinor    int64    `json:"amount_minor"`
	IdempotencyKey string   `json:"idempotency_key"`
	BudgetPath     []string `json:"budget_path"`
}

type ActResponse struct {
	Status      string `json:"status"`
	DecisionID  string `json:"decision_id"`
	RailRef     string `json:"rail_ref"`
}

type idemCacheEntry struct {
	StatusCode int          `json:"status_code"`
	Response   ActResponse  `json:"response"`
	Error      *idemError   `json:"error,omitempty"`
}

type idemError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type PreflightResponse struct {
	WouldAllow bool   `json:"would_allow"`
	ReasonCode string `json:"reason_code,omitempty"`
}

type Handler struct {
	identity   *clients.IdentityClient
	policy     *clients.PolicyClient
	budget     *clients.BudgetClient
	audit      *clients.AuditClient
	rail       *clients.RailClient
	containment *clients.ContainmentClient
	rdb        *redis.Client
	epochCache *epoch.Cache
	log        *telemetry.Logger
	pubkey     ed25519.PublicKey
}

func New(identity *clients.IdentityClient, policy *clients.PolicyClient, budget *clients.BudgetClient, audit *clients.AuditClient, rail *clients.RailClient, containment *clients.ContainmentClient, rdb *redis.Client, epochCache *epoch.Cache, log *telemetry.Logger, pubkey ed25519.PublicKey) *Handler {
	return &Handler{identity: identity, policy: policy, budget: budget, audit: audit, rail: rail, containment: containment, rdb: rdb, epochCache: epochCache, log: log, pubkey: pubkey}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/act", h.HandleAct)
	mux.HandleFunc("POST /v1/act/preflight", h.HandlePreflight)
	mux.HandleFunc("POST /v1/act/approve/{approval_id}", h.HandleApproveAct)
	mux.HandleFunc("POST /v1/policies/promote", h.HandlePromotePolicy)
	mux.HandleFunc("POST /v1/policy-versions/{hash}/retro-simulate", h.HandleRetroPolicy)
	mux.HandleFunc("GET /healthz", h.HandleHealthz)
	return mux
}

func (h *Handler) HandleAct(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	var req ActRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.TokenInvalid, "invalid JSON body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	// IDEMPOTENCY CHECK: if we've seen this idempotency_key before, return cached result
	if req.IdempotencyKey != "" && h.rdb != nil {
		cached, err := h.rdb.Get(ctx, "gateway:idem:"+req.IdempotencyKey).Bytes()
		if err == nil && len(cached) > 0 {
			var entry idemCacheEntry
			if err := json.Unmarshal(cached, &entry); err == nil {
				h.log.Info("idempotency_cache_hit", map[string]interface{}{
					"idempotency_key": req.IdempotencyKey,
					"decision_id":     entry.Response.DecisionID,
				})
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(entry.StatusCode)
				if entry.Error != nil {
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"error": map[string]string{"code": entry.Error.Code, "message": entry.Error.Message},
					})
				} else {
					_ = json.NewEncoder(w).Encode(entry.Response)
				}
				return
			}
		}
	}

	// Helper to cache the result
	cacheResult := func(statusCode int, resp *ActResponse, errCode, errMsg string) {
		if req.IdempotencyKey == "" || h.rdb == nil {
			return
		}
		entry := idemCacheEntry{StatusCode: statusCode}
		if resp != nil {
			entry.Response = *resp
		}
		if errCode != "" {
			entry.Error = &idemError{Code: errCode, Message: errMsg}
		}
		data, _ := json.Marshal(entry)
		h.rdb.Set(ctx, "gateway:idem:"+req.IdempotencyKey, data, 5*time.Minute)
	}

	// STAGE 1: IDENTITY
	tok, err := capability.VerifyAndParse(req.Token, h.pubkey)
	if err != nil {
		h.log.Error("stage_identity_failed", map[string]interface{}{"error": err.Error()})
		cacheResult(http.StatusForbidden, nil, apierr.TokenInvalid, err.Error())
		apierr.WriteError(w, http.StatusForbidden, apierr.TokenInvalid, err.Error())
		return
	}
	h.log.Info("stage_identity", map[string]interface{}{"result": "pass", "agent_id": tok.AgentID})

	// EPOCH CHECK (part of Stage 1 per Section 6.5)
	if h.epochCache != nil {
		if err := h.epochCache.Check(tok.EpochFleet, tok.EpochGroup, tok.EpochAgent, tok.EpochMandate, "", tok.AgentID, tok.MandateID); err != nil {
			h.log.Info("epoch_stale", map[string]interface{}{"error": err.Error(), "agent_id": tok.AgentID})
			cacheResult(http.StatusForbidden, nil, apierr.EpochStale, err.Error())
			apierr.WriteError(w, http.StatusForbidden, apierr.EpochStale, err.Error())
			return
		}
	}

	// STAGE 2: CONTAINMENT
	agent, err := h.identity.GetAgent(ctx, tok.AgentID)
	if err != nil {
		h.log.Error("stage_containment_agent_failed", map[string]interface{}{"error": err.Error()})
		apierr.WriteError(w, http.StatusForbidden, apierr.TokenInvalid, "failed to get agent info")
		return
	}

	level := "OBSERVE"
	if h.containment != nil {
		level, _ = h.containment.GetLevel(ctx, tok.AgentID, agent.GroupID, tok.MandateID)
	}
	h.log.Info("stage_containment", map[string]interface{}{"level": level, "agent_id": tok.AgentID})

	// Build the full context for potential approval request
	fullCtxBytes, _ := json.Marshal(map[string]interface{}{
		"action_type":     req.ActionType,
		"counterparty_id": req.CounterpartyID,
		"amount_minor":    req.AmountMinor,
		"containment":     level,
		"agent_id":        tok.AgentID,
		"mandate_id":      tok.MandateID,
	})

	switch level {
	case "HALT":
		cacheResult(http.StatusForbidden, nil, apierr.ContainmentHalt, "scope is at HALT")
		apierr.WriteError(w, http.StatusForbidden, apierr.ContainmentHalt, "scope is at HALT")
		return

	case "QUARANTINE":
		if tok.HasCaveat("requires_approval") || level == "QUARANTINE" {
			approvalID, _ := h.containment.CreateApproval(ctx, clients.ApprovalCreateRequest{
				DecisionContext: fullCtxBytes,
				AgentID:         tok.AgentID,
			})
			h.log.Info("quarantine_routed_to_approval", map[string]interface{}{"approval_id": approvalID})
			cacheResult(http.StatusAccepted, &ActResponse{Status: "pending_approval", DecisionID: approvalID}, "", "")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{
				"status":       "pending_approval",
				"approval_id":  approvalID,
			})
			return
		}

	case "THROTTLE":
		// Dial 3: New counterparty escalation
		if h.rdb != nil && isNewCounterparty(ctx, h.rdb, tok.AgentID, req.CounterpartyID) {
			approvalID, _ := h.containment.CreateApproval(ctx, clients.ApprovalCreateRequest{
				DecisionContext: fullCtxBytes,
				AgentID:         tok.AgentID,
			})
			h.log.Info("throttle_new_counterparty_routed", map[string]interface{}{"approval_id": approvalID})
			cacheResult(http.StatusAccepted, &ActResponse{Status: "pending_approval", DecisionID: approvalID}, "", "")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{
				"status":      "pending_approval",
				"approval_id": approvalID,
			})
			return
		}

		// Dial 2: Velocity cap
		allowed, _ := h.budget.VelocityCheck(ctx, tok.AgentID)
		if !allowed {
			cacheResult(http.StatusForbidden, nil, apierr.VelocityExceeded, "velocity cap exceeded")
			apierr.WriteError(w, http.StatusForbidden, apierr.VelocityExceeded, "velocity cap exceeded")
			return
		}
	}

	// STAGE 3: POLICY
	cedarReq := clients.CedarRequest{
		Principal: fmt.Sprintf(`Agent::"%s"`, tok.AgentID),
		Action:    fmt.Sprintf(`Action::"%s"`, req.ActionType),
		Resource:  fmt.Sprintf(`Counterparty::"%s"`, req.CounterpartyID),
		Context: map[string]interface{}{
			"amount_minor":      req.AmountMinor,
			"mandate_id":        tok.MandateID,
			"persona":           agent.Persona,
			"containment_level": level,
		},
	}

	cedarResp, err := h.policy.IsAuthorized(ctx, cedarReq)
	if err != nil {
		h.log.Error("stage_policy_failed", map[string]interface{}{"error": err.Error()})
		
		if !structuralLintFallback(req.ActionType) {
			apierr.WriteError(w, http.StatusForbidden, apierr.PolicyDenied, "policy service unavailable and fallback denied")
			return
		}
		h.log.Info("stage_policy_fallback", map[string]interface{}{"decision": "Allow"})
	} else if cedarResp.Decision != "Allow" {
		h.log.Info("stage_policy", map[string]interface{}{"decision": "Deny"})
		cacheResult(http.StatusForbidden, nil, apierr.PolicyDenied, "policy denied")
		apierr.WriteError(w, http.StatusForbidden, apierr.PolicyDenied, "policy denied")
		return
	} else {
		h.log.Info("stage_policy", map[string]interface{}{"decision": "Allow"})
	}

	// Mandate scope check (explicit second gate per Section 7.2)
	mandate, err := h.identity.GetMandate(ctx, tok.MandateID)
	if err != nil {
		h.log.Error("mandate_fetch_failed", map[string]interface{}{"error": err.Error()})
		apierr.WriteError(w, http.StatusForbidden, apierr.MandateRevoked, "failed to get mandate")
		return
	}

	if mandate.Status != "live" {
		h.log.Info("mandate_check", map[string]interface{}{"result": "revoked"})
		apierr.WriteError(w, http.StatusForbidden, apierr.MandateRevoked, "mandate is not live")
		return
	}

	if isExpired(mandate.ValidTo) {
		h.log.Info("mandate_check", map[string]interface{}{"result": "expired"})
		apierr.WriteError(w, http.StatusForbidden, apierr.MandateExpired, "mandate has expired")
		return
	}

	if !counterpartyInScope(req.CounterpartyID, mandate.CounterpartyScope) {
		h.log.Info("mandate_check", map[string]interface{}{"result": "counterparty_out_of_scope"})
		cacheResult(http.StatusForbidden, nil, apierr.MandateScopeViolation,
			fmt.Sprintf("counterparty %s is outside the live mandate's scope", req.CounterpartyID))
		apierr.WriteError(w, http.StatusForbidden, apierr.MandateScopeViolation,
			fmt.Sprintf("counterparty %s is outside the live mandate's scope", req.CounterpartyID),
			map[string]interface{}{"mandate_id": tok.MandateID, "counterparty": req.CounterpartyID})
		return
	}

	if req.AmountMinor > mandate.CeilingMinor {
		h.log.Info("mandate_check", map[string]interface{}{"result": "amount_exceeds_ceiling"})
		cacheResult(http.StatusForbidden, nil, apierr.MandateScopeViolation, "amount exceeds mandate ceiling")
		apierr.WriteError(w, http.StatusForbidden, apierr.MandateScopeViolation,
			"amount exceeds mandate ceiling")
		return
	}
	h.log.Info("mandate_check", map[string]interface{}{"result": "pass"})

	// Pre-execute audit stub (BEFORE budget, per Section 7.1)
	fullCtx, _ := json.Marshal(map[string]interface{}{
		"action_type":     req.ActionType,
		"counterparty_id": req.CounterpartyID,
		"amount_minor":    req.AmountMinor,
		"containment":     level,
	})

	stub, err := h.audit.CreateStub(ctx, clients.AuditStubRequest{
		AgentID:           &tok.AgentID,
		MandateID:         &tok.MandateID,
		TokenID:           &tok.TokenID,
		ActionType:        req.ActionType,
		PolicyVersionHash: "active-policy",
		FullContext:       fullCtx,
	})
	if err != nil {
		h.log.Error("audit_stub_failed", map[string]interface{}{"error": err.Error()})
	}

	decisionID := "unknown"
	if stub != nil {
		decisionID = stub.DecisionID
	}

	// STAGE 4: BUDGET
	reservation, err := h.budget.Reserve(ctx, clients.BudgetReserveRequest{
		Path:           req.BudgetPath,
		AmountMinor:    req.AmountMinor,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		latency := time.Since(start).Milliseconds()
		if stub != nil {
			_ = h.audit.Finalize(ctx, stub.DecisionID, clients.AuditFinalizeRequest{
				Decision: "DENY", ReasonCode: apierr.BudgetExceeded, LatencyMs: &latency,
			})
		}
		h.log.Error("stage_budget_failed", map[string]interface{}{"error": err.Error(), "decision_id": decisionID})
		if _, ok := err.(*clients.BudgetExceededError); ok {
			cacheResult(http.StatusForbidden, nil, apierr.BudgetExceeded, "budget exceeded")
			apierr.WriteError(w, http.StatusForbidden, apierr.BudgetExceeded, "budget exceeded")
			return
		}
		cacheResult(http.StatusForbidden, nil, apierr.BudgetExceeded, "budget service error")
		apierr.WriteError(w, http.StatusForbidden, apierr.BudgetExceeded, "budget service error")
		return
	}
	h.log.Info("stage_budget", map[string]interface{}{
		"reservation_id": reservation.ReservationID,
		"decision_id":    decisionID,
	})

	// STAGE 5: EXECUTE
	railResp, err := h.rail.Execute(ctx, clients.RailExecuteRequest{
		ActionType:     req.ActionType,
		CounterpartyID: req.CounterpartyID,
		AmountMinor:    req.AmountMinor,
	})
	if err != nil {
		_ = h.budget.Release(ctx, reservation.ReservationID)
		latency := time.Since(start).Milliseconds()
		if stub != nil {
			_ = h.audit.Finalize(ctx, stub.DecisionID, clients.AuditFinalizeRequest{
				Decision: "DENY", ReasonCode: apierr.RailError, LatencyMs: &latency,
			})
		}
		h.log.Error("stage_execute_failed", map[string]interface{}{"error": err.Error(), "decision_id": decisionID})
		cacheResult(http.StatusBadGateway, nil, apierr.RailError, "rail execution failed")
		apierr.WriteError(w, http.StatusBadGateway, apierr.RailError, "rail execution failed")
		return
	}
	h.log.Info("stage_execute", map[string]interface{}{
		"rail_ref":    railResp.RailRef,
		"decision_id": decisionID,
	})

	_ = h.budget.Commit(ctx, reservation.ReservationID)
	h.log.Info("stage_budget_committed", map[string]interface{}{"decision_id": decisionID})

	// STAGE 6: FINALIZE AUDIT
	latency := time.Since(start).Milliseconds()
	if stub != nil {
		_ = h.audit.Finalize(ctx, stub.DecisionID, clients.AuditFinalizeRequest{
			Decision: "ALLOW", ReasonCode: "OK", LatencyMs: &latency,
		})
	}

	h.log.Info("act_complete", map[string]interface{}{
		"decision_id": decisionID,
		"outcome":     "allow",
		"latency_ms":  latency,
	})

	if h.rdb != nil {
		eventData, _ := json.Marshal(map[string]interface{}{
			"agent_id":        tok.AgentID,
			"action_type":     req.ActionType,
			"counterparty_id": req.CounterpartyID,
			"amount_minor":    req.AmountMinor,
			"timestamp":       time.Now().Format(time.RFC3339),
			"decision":        "ALLOW",
		})
		h.rdb.XAdd(ctx, &redis.XAddArgs{
			Stream: "stream:decisions",
			Values: map[string]interface{}{"data": string(eventData)},
		})
	}

	resp := ActResponse{
		Status:     "executed",
		DecisionID: decisionID,
		RailRef:    railResp.RailRef,
	}

	cacheResult(http.StatusOK, &resp, "", "")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) HandlePreflight(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(PreflightResponse{WouldAllow: true})
}

func (h *Handler) HandleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func isExpired(validTo string) bool {
	t, err := time.Parse(time.RFC3339Nano, validTo)
	if err != nil {
		return false
	}
	return time.Now().After(t)
}

func counterpartyInScope(counterpartyID string, scopeJSON json.RawMessage) bool {
	var scope struct {
		Type   string   `json:"type"`
		Values []string `json:"values"`
	}
	if err := json.Unmarshal(scopeJSON, &scope); err != nil {
		return false
	}
	if scope.Type == "allowlist" {
		for _, v := range scope.Values {
			if v == counterpartyID {
				return true
			}
		}
		return false
	}
	return true
}

func structuralLintFallback(actionType string) bool {
	validActions := map[string]bool{
		"PAYMENT": true,
		"TRADE": true,
	}
	return validActions[actionType]
}

func isNewCounterparty(ctx context.Context, rdb *redis.Client, agentID, counterpartyID string) bool {
	key := fmt.Sprintf("counterparty:history:%s", agentID)
	isMember, _ := rdb.SIsMember(ctx, key, counterpartyID).Result()
	if !isMember {
		rdb.SAdd(ctx, key, counterpartyID)
		return true
	}
	return false
}

func (h *Handler) HandleApproveAct(w http.ResponseWriter, r *http.Request) {
	approvalID := r.PathValue("approval_id")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	// Fetch the approval request
	approval, err := h.containment.GetApproval(ctx, approvalID)
	if err != nil {
		apierr.WriteError(w, http.StatusNotFound, apierr.ApprovalDenied, "approval not found")
		return
	}

	if approval.State != "APPROVED" {
		apierr.WriteError(w, http.StatusForbidden, apierr.ApprovalDenied, "approval not in APPROVED state")
		return
	}

	// Extract the original decision context
	var origCtx struct {
		ActionType     string `json:"action_type"`
		CounterpartyID string `json:"counterparty_id"`
		AmountMinor    int64  `json:"amount_minor"`
		AgentID        string `json:"agent_id"`
		MandateID      string `json:"mandate_id"`
	}
	_ = json.Unmarshal(approval.DecisionContext, &origCtx)

	// For the re-drive, we need a budget path — use root or the one from context
	// In a real system this would be stored in the approval context
	h.log.Info("approval_re_drive", map[string]interface{}{
		"approval_id": approvalID,
		"action_type": origCtx.ActionType,
	})

	// Re-drive through stages 3-6 (policy, budget, execute, audit)
	// For Phase 2 simplicity, we just execute directly
	start := time.Now()

	cedarReq := clients.CedarRequest{
		Principal: fmt.Sprintf(`Agent::"%s"`, origCtx.AgentID),
		Action:    fmt.Sprintf(`Action::"%s"`, origCtx.ActionType),
		Resource:  fmt.Sprintf(`Counterparty::"%s"`, origCtx.CounterpartyID),
		Context: map[string]interface{}{
			"amount_minor":      origCtx.AmountMinor,
			"mandate_id":        origCtx.MandateID,
			"persona":           "approved",
			"containment_level": "OBSERVE",
		},
	}

	cedarResp, err := h.policy.IsAuthorized(ctx, cedarReq)
	if err != nil || cedarResp.Decision != "Allow" {
		apierr.WriteError(w, http.StatusForbidden, apierr.PolicyDenied, "policy denied on re-drive")
		return
	}

	// Create audit stub
	stub, _ := h.audit.CreateStub(ctx, clients.AuditStubRequest{
		AgentID:           &origCtx.AgentID,
		ActionType:        origCtx.ActionType,
		PolicyVersionHash: "active-policy",
		FullContext:       approval.DecisionContext,
	})

	decisionID := "unknown"
	if stub != nil {
		decisionID = stub.DecisionID
	}

	// Execute on rail
	railResp, err := h.rail.Execute(ctx, clients.RailExecuteRequest{
		ActionType:     origCtx.ActionType,
		CounterpartyID: origCtx.CounterpartyID,
		AmountMinor:    origCtx.AmountMinor,
	})
	if err != nil {
		latency := time.Since(start).Milliseconds()
		if stub != nil {
			_ = h.audit.Finalize(ctx, stub.DecisionID, clients.AuditFinalizeRequest{
				Decision: "DENY", ReasonCode: apierr.RailError, LatencyMs: &latency,
			})
		}
		apierr.WriteError(w, http.StatusBadGateway, apierr.RailError, "rail execution failed")
		return
	}

	latency := time.Since(start).Milliseconds()
	if stub != nil {
		_ = h.audit.Finalize(ctx, stub.DecisionID, clients.AuditFinalizeRequest{
			Decision: "ALLOW", ReasonCode: "OK", LatencyMs: &latency,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ActResponse{
		Status:     "executed",
		DecisionID: decisionID,
		RailRef:    railResp.RailRef,
	})
}
