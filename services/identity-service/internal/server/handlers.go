package server

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"aegis/pkg/apierr"
	"aegis/pkg/idgen"
	"aegis/pkg/telemetry"
	"aegis/pkg/capability"
	"aegis/services/identity-service/internal/store"
	rediscli "aegis/services/identity-service/internal/redis"
	"aegis/services/identity-service/internal/tokens"
)

type Handler struct {
	store     *store.Store
	redis     *rediscli.Client
	tokens    *tokens.Service
	log       *telemetry.Logger
	signingKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
}

func New(st *store.Store, rdb *rediscli.Client, tok *tokens.Service, log *telemetry.Logger, signingKey ed25519.PrivateKey, publicKey ed25519.PublicKey) *Handler {
	return &Handler{store: st, redis: rdb, tokens: tok, log: log, signingKey: signingKey, publicKey: publicKey}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/pubkey", h.HandleGetPubkey)
	mux.HandleFunc("GET /v1/agents/{id}", h.HandleGetAgent)
	mux.HandleFunc("POST /v1/mandates", h.HandleCreateMandate)
	mux.HandleFunc("GET /v1/mandates/{id}", h.HandleGetMandate)
	mux.HandleFunc("POST /v1/mandates/{id}/revoke", h.HandleRevokeMandate)
	mux.HandleFunc("POST /v1/tokens/mint", h.HandleMint)
	mux.HandleFunc("POST /v1/tokens/attenuate", h.HandleAttenuate)
	mux.HandleFunc("POST /v1/tokens/renew", h.HandleRenew)
	mux.HandleFunc("GET /v1/agents", h.HandleListAgents)
	mux.HandleFunc("GET /healthz", h.HandleHealthz)
	return mux
}

func (h *Handler) HandleGetPubkey(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"pubkey": h.tokens.PubkeyB64()})
}

func (h *Handler) HandleListAgents(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	agents, err := h.store.ListAgents(ctx)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, "MANDATE_REVOKED", "failed to list agents")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agents)
}

func (h *Handler) HandleGetAgent(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	agent, err := h.store.GetAgent(ctx, agentID)
	if err != nil {
		apierr.WriteError(w, http.StatusNotFound, "MANDATE_REVOKED", "agent not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agent)
}

type CreateMandateRequest struct {
	Issuer            string          `json:"issuer"`
	PurposeCode       string          `json:"purpose_code"`
	CounterpartyScope json.RawMessage `json:"counterparty_scope"`
	CeilingMinor      int64           `json:"ceiling_minor"`
	Currency          string          `json:"currency"`
	ValidFrom         string          `json:"valid_from"`
	ValidTo           string          `json:"valid_to"`
	MaxDepth          int             `json:"max_depth"`
	Recurrence        json.RawMessage `json:"recurrence"`
}

func (h *Handler) HandleCreateMandate(w http.ResponseWriter, r *http.Request) {
	var req CreateMandateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, "MANDATE_REVOKED", "invalid JSON body")
		return
	}

	if req.Issuer == "" || req.PurposeCode == "" || req.CeilingMinor <= 0 {
		apierr.WriteError(w, http.StatusBadRequest, "MANDATE_REVOKED", "missing required fields")
		return
	}

	mandateID := idgen.New()
	nonce := idgen.New()[:10]

	validFrom, err := time.Parse(time.RFC3339Nano, req.ValidFrom)
	if err != nil {
		validFrom = time.Now().UTC()
	}
	validTo, err := time.Parse(time.RFC3339Nano, req.ValidTo)
	if err != nil {
		validTo = time.Now().UTC().Add(7 * 24 * time.Hour)
	}

	if req.Currency == "" {
		req.Currency = "USD"
	}
	if req.MaxDepth == 0 {
		req.MaxDepth = 1
	}

	m := &store.Mandate{
		ID:                mandateID,
		Issuer:            req.Issuer,
		PurposeCode:       req.PurposeCode,
		CounterpartyScope: req.CounterpartyScope,
		CeilingMinor:      req.CeilingMinor,
		Currency:          req.Currency,
		ValidFrom:         validFrom,
		ValidTo:           validTo,
		MaxDepth:          req.MaxDepth,
		Recurrence:        req.Recurrence,
		Nonce:             nonce,
		Status:            "live",
	}

	// Sign the mandate
	mandateFields := capability.MandateSignedFields{
		ID:                mandateID,
		Issuer:            req.Issuer,
		PurposeCode:       req.PurposeCode,
		CounterpartyScope: req.CounterpartyScope,
		CeilingMinor:      req.CeilingMinor,
		Currency:          req.Currency,
		ValidFrom:         validFrom.Format(time.RFC3339Nano),
		ValidTo:           validTo.Format(time.RFC3339Nano),
		MaxDepth:          req.MaxDepth,
		Recurrence:        req.Recurrence,
		Nonce:             nonce,
	}

	sig, err := capability.SignMandate(mandateFields, h.signingKey)
	if err != nil {
		h.log.Error("sign_mandate_failed", map[string]interface{}{"error": err.Error()})
		apierr.WriteError(w, http.StatusInternalServerError, "MANDATE_REVOKED", "failed to sign mandate")
		return
	}

	sigBytes, _ := base64.StdEncoding.DecodeString(sig)
	m.SignerPubkey = h.publicKey
	m.Signature = sigBytes

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.store.CreateMandate(ctx, m); err != nil {
		h.log.Error("create_mandate_failed", map[string]interface{}{"error": err.Error()})
		apierr.WriteError(w, http.StatusInternalServerError, "MANDATE_REVOKED", "failed to create mandate")
		return
	}

	h.log.Info("mandate_created", map[string]interface{}{"mandate_id": mandateID})

	resp := map[string]interface{}{
		"id":                 mandateID,
		"issuer":             req.Issuer,
		"purpose_code":       req.PurposeCode,
		"counterparty_scope": json.RawMessage(req.CounterpartyScope),
		"ceiling_minor":      req.CeilingMinor,
		"currency":           req.Currency,
		"valid_from":         validFrom.Format(time.RFC3339Nano),
		"valid_to":           validTo.Format(time.RFC3339Nano),
		"max_depth":          req.MaxDepth,
		"recurrence":         req.Recurrence,
		"nonce":              nonce,
		"status":             "live",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) HandleGetMandate(w http.ResponseWriter, r *http.Request) {
	mandateID := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	m, err := h.store.GetMandate(ctx, mandateID)
	if err != nil {
		apierr.WriteError(w, http.StatusNotFound, "MANDATE_REVOKED", "mandate not found")
		return
	}

	resp := map[string]interface{}{
		"id":                 m.ID,
		"issuer":             m.Issuer,
		"purpose_code":       m.PurposeCode,
		"counterparty_scope": m.CounterpartyScope,
		"ceiling_minor":      m.CeilingMinor,
		"currency":           m.Currency,
		"valid_from":         m.ValidFrom.Format(time.RFC3339Nano),
		"valid_to":           m.ValidTo.Format(time.RFC3339Nano),
		"max_depth":          m.MaxDepth,
		"recurrence":         m.Recurrence,
		"nonce":              m.Nonce,
		"status":             m.Status,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

type RevokeMandateRequest struct {
	Reason string `json:"reason"`
	Actor  string `json:"actor"`
}

func (h *Handler) HandleRevokeMandate(w http.ResponseWriter, r *http.Request) {
	mandateID := r.PathValue("id")
	var req RevokeMandateRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.store.RevokeMandate(ctx, mandateID, req.Reason); err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, "MANDATE_REVOKED", "failed to revoke mandate")
		return
	}

	_ = h.redis.BumpMandateEpoch(ctx, mandateID)

	h.log.Info("mandate_revoked", map[string]interface{}{"mandate_id": mandateID})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "revoked"})
}

type MintRequest struct {
	AgentID   string                `json:"agent_id"`
	MandateID string                `json:"mandate_id"`
	Caveats   []capability.Caveat   `json:"caveats"`
}

func (h *Handler) HandleMint(w http.ResponseWriter, r *http.Request) {
	var req MintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, "TOKEN_INVALID", "invalid JSON body")
		return
	}

	if req.AgentID == "" || req.MandateID == "" {
		apierr.WriteError(w, http.StatusBadRequest, "TOKEN_INVALID", "agent_id and mandate_id are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	tokenJSON, err := h.tokens.Mint(ctx, req.AgentID, req.MandateID, req.Caveats)
	if err != nil {
		if me, ok := err.(*tokens.MandateError); ok {
			code := http.StatusForbidden
			if me.Code == "MANDATE_EXPIRED" {
				code = http.StatusForbidden
			}
			apierr.WriteError(w, code, me.Code, me.Message)
			return
		}
		h.log.Error("mint_failed", map[string]interface{}{"error": err.Error()})
		apierr.WriteError(w, http.StatusInternalServerError, "TOKEN_INVALID", "failed to mint token")
		return
	}

	h.log.Info("token_minted", map[string]interface{}{"mandate_id": req.MandateID, "agent_id": req.AgentID})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	io.WriteString(w, tokenJSON)
}

type AttenuateRequest struct {
	ParentToken       string                `json:"parent_token"`
	AdditionalCaveats []capability.Caveat   `json:"additional_caveats"`
}

func (h *Handler) HandleAttenuate(w http.ResponseWriter, r *http.Request) {
	var req AttenuateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, "TOKEN_INVALID", "invalid JSON body")
		return
	}

	if req.ParentToken == "" {
		apierr.WriteError(w, http.StatusBadRequest, "TOKEN_INVALID", "parent_token is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	tokenJSON, err := h.tokens.Attenuate(ctx, req.ParentToken, req.AdditionalCaveats)
	if err != nil {
		if me, ok := err.(*tokens.MandateError); ok {
			apierr.WriteError(w, http.StatusBadRequest, me.Code, me.Message)
			return
		}
		h.log.Error("attenuate_failed", map[string]interface{}{"error": err.Error()})
		apierr.WriteError(w, http.StatusBadRequest, "TOKEN_INVALID", err.Error())
		return
	}

	h.log.Info("token_attenuated")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	io.WriteString(w, tokenJSON)
}

type RenewRequest struct {
	OldToken string `json:"old_token"`
}

func (h *Handler) HandleRenew(w http.ResponseWriter, r *http.Request) {
	var req RenewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, "TOKEN_INVALID", "invalid JSON body")
		return
	}

	if req.OldToken == "" {
		apierr.WriteError(w, http.StatusBadRequest, "TOKEN_INVALID", "old_token is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	tokenJSON, err := h.tokens.Renew(ctx, req.OldToken)
	if err != nil {
		if me, ok := err.(*tokens.MandateError); ok {
			apierr.WriteError(w, http.StatusForbidden, me.Code, me.Message)
			return
		}
		h.log.Error("renew_failed", map[string]interface{}{"error": err.Error()})
		apierr.WriteError(w, http.StatusBadRequest, "TOKEN_INVALID", err.Error())
		return
	}

	h.log.Info("token_renewed")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	io.WriteString(w, tokenJSON)
}

func (h *Handler) HandleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := h.store.HealthCheck(ctx); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "unavailable"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func pubkeyB64ToBytes(b64 string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(b64)
}

func _unused() {
	_ = fmt.Sprintf
	_ = os.Getenv
}
