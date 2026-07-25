package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"aegis/pkg/apierr"
	"aegis/pkg/telemetry"
	"aegis/services/containment-controller/internal/approvals"
	"aegis/services/containment-controller/internal/ladder"
	"aegis/services/containment-controller/internal/sagas"
	"aegis/services/containment-controller/internal/store"
)

type Handler struct {
	ladder    *ladder.Service
	sagas     *sagas.Service
	approvals *approvals.Service
	store     *store.Store
	log       *telemetry.Logger
}

func New(l *ladder.Service, sg *sagas.Service, ap *approvals.Service, st *store.Store, log *telemetry.Logger) *Handler {
	return &Handler{ladder: l, sagas: sg, approvals: ap, store: st, log: log}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/containment/level", h.HandleGetLevel)
	mux.HandleFunc("PUT /v1/containment/{scope_type}/{scope_id}", h.HandleSetLevel)
	mux.HandleFunc("POST /v1/emergency-stop", h.HandleEmergencyStop)
	mux.HandleFunc("POST /v1/resume", h.HandleResume)
	mux.HandleFunc("GET /v1/containment/states", h.HandleGetAllStates)
	mux.HandleFunc("GET /v1/containment/events", h.HandleGetEvents)
	mux.HandleFunc("POST /v1/sagas/register", h.HandleSagaRegister)
	mux.HandleFunc("POST /v1/sagas/{id}/leg-complete", h.HandleSagaLegComplete)
	mux.HandleFunc("GET /v1/sagas/{id}", h.HandleGetSaga)
	mux.HandleFunc("GET /v1/approvals/pending", h.HandleGetPendingApprovals)
	mux.HandleFunc("GET /v1/approvals/{id}", h.HandleGetApproval)
	mux.HandleFunc("POST /v1/approvals", h.HandleCreateApproval)
	mux.HandleFunc("POST /v1/approvals/{id}/decide", h.HandleDecideApproval)
	mux.HandleFunc("GET /healthz", h.HandleHealthz)
	return mux
}

func (h *Handler) HandleGetLevel(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	agentID := r.URL.Query().Get("agent_id")
	groupID := r.URL.Query().Get("group_id")
	mandateID := r.URL.Query().Get("mandate_id")

	level, err := h.ladder.GetLevel(ctx, agentID, groupID, mandateID)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, "CONTAINMENT_HALT", "failed to get level")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"level": level})
}

func (h *Handler) HandleSetLevel(w http.ResponseWriter, r *http.Request) {
	scopeType := r.PathValue("scope_type")
	scopeID := r.PathValue("scope_id")

	var req struct {
		Level  string `json:"level"`
		Reason string `json:"reason"`
		Actor  string `json:"actor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, "CONTAINMENT_HALT", "invalid JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	result, err := h.ladder.SetLevel(ctx, scopeType, scopeID, req.Level, req.Reason, req.Actor)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, "CONTAINMENT_HALT", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (h *Handler) HandleEmergencyStop(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ScopeType string `json:"scope_type"`
		ScopeID   string `json:"scope_id"`
		Reason    string `json:"reason"`
		Actor     string `json:"actor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, "CONTAINMENT_HALT", "invalid JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	epochVal, swept, err := h.ladder.EmergencyStop(ctx, req.ScopeType, req.ScopeID, req.Reason, req.Actor, h.sagas.Sweep)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, "CONTAINMENT_HALT", err.Error())
		return
	}

	h.log.Info("emergency_stop", map[string]interface{}{
		"scope": req.ScopeType, "scope_id": req.ScopeID,
		"epoch": epochVal, "sagas_swept": swept,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"level":              "HALT",
		"epoch_bumped_to":    epochVal,
		"sagas_swept":        swept,
		"orphaned_funds_minor": 0,
	})
}

func (h *Handler) HandleResume(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ScopeType string `json:"scope_type"`
		ScopeID   string `json:"scope_id"`
		Actor     string `json:"actor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, "CONTAINMENT_HALT", "invalid JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	level, err := h.ladder.Resume(ctx, req.ScopeType, req.ScopeID, req.Actor)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, "CONTAINMENT_HALT", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"level": level})
}

func (h *Handler) HandleGetAllStates(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	states, err := h.ladder.GetAllStates(ctx)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, "CONTAINMENT_HALT", "failed to get states")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(states)
}

func (h *Handler) HandleGetEvents(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	events, err := h.ladder.GetRecentEvents(ctx, limit)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, "CONTAINMENT_HALT", "failed to get events")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

func (h *Handler) HandleSagaRegister(w http.ResponseWriter, r *http.Request) {
	var req sagas.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, "CONTAINMENT_HALT", "invalid JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	sg, err := h.sagas.Register(ctx, req)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, "CONTAINMENT_HALT", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sg)
}

func (h *Handler) HandleSagaLegComplete(w http.ResponseWriter, r *http.Request) {
	sagaID := r.PathValue("id")
	var req struct {
		LegSeq int `json:"leg_seq"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, "CONTAINMENT_HALT", "invalid JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	sg, err := h.sagas.LegComplete(ctx, sagaID, req.LegSeq)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, "CONTAINMENT_HALT", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sg)
}

func (h *Handler) HandleGetSaga(w http.ResponseWriter, r *http.Request) {
	sagaID := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	sg, err := h.store.GetSaga(ctx, sagaID)
	if err != nil {
		apierr.WriteError(w, http.StatusNotFound, "CONTAINMENT_HALT", "saga not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sg)
}

func (h *Handler) HandleCreateApproval(w http.ResponseWriter, r *http.Request) {
	var req approvals.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, "CONTAINMENT_HALT", "invalid JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	id, err := h.approvals.Create(ctx, req)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, "CONTAINMENT_HALT", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"approval_id": id})
}

func (h *Handler) HandleGetPendingApprovals(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	approvals, err := h.approvals.GetPending(ctx)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, "CONTAINMENT_HALT", "failed to get approvals")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(approvals)
}

func (h *Handler) HandleGetApproval(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	ar, err := h.approvals.Get(ctx, id)
	if err != nil {
		apierr.WriteError(w, http.StatusNotFound, "CONTAINMENT_HALT", "approval not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ar)
}

func (h *Handler) HandleDecideApproval(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req approvals.DecideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, "CONTAINMENT_HALT", "invalid JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	ar, err := h.approvals.Decide(ctx, id, req)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, "CONTAINMENT_HALT", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ar)
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
