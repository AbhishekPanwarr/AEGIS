package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"aegis/pkg/apierr"
	"aegis/pkg/idgen"
	"aegis/pkg/telemetry"
	"aegis/services/budget-ledger/internal/reserve"
	"aegis/services/budget-ledger/internal/tree"

	"github.com/redis/go-redis/v9"
)

type Handler struct {
	tree    *tree.Service
	reserve *reserve.Service
	rdb     *redis.Client
	log     *telemetry.Logger
	naive   bool
}

func New(tr *tree.Service, rs *reserve.Service, rdb *redis.Client, log *telemetry.Logger) *Handler {
	naive := os.Getenv("DEMO_NAIVE_MODE") == "true"
	return &Handler{tree: tr, reserve: rs, rdb: rdb, log: log, naive: naive}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/budget-nodes", h.HandleCreateNode)
	mux.HandleFunc("PUT /v1/budget-nodes/{id}", h.HandleUpdateNode)
	mux.HandleFunc("GET /v1/budget-nodes/{id}/usage", h.HandleGetUsage)
	mux.HandleFunc("GET /v1/budget-nodes/{id}", h.HandleGetNode)
	mux.HandleFunc("GET /v1/budget-nodes", h.HandleListNodes)
	mux.HandleFunc("GET /v1/budget-node-by-label/{label}", h.HandleGetByLabel)
	mux.HandleFunc("POST /v1/reservations", h.HandleReserve)
	mux.HandleFunc("POST /v1/reservations/{id}/commit", h.HandleCommit)
	mux.HandleFunc("POST /v1/reservations/{id}/release", h.HandleRelease)
	mux.HandleFunc("GET /v1/velocity-check/{agent_id}", h.HandleVelocityCheck)
	mux.HandleFunc("GET /healthz", h.HandleHealthz)

	if h.naive {
		mux.HandleFunc("POST /v1/reservations-naive", h.HandleNaiveReserve)
	} else {
		mux.HandleFunc("POST /v1/reservations-naive", func(w http.ResponseWriter, r *http.Request) {
			apierr.WriteError(w, http.StatusNotFound, "BUDGET_EXCEEDED", "naive mode disabled")
		})
	}

	return mux
}

func (h *Handler) HandleCreateNode(w http.ResponseWriter, r *http.Request) {
	var req tree.CreateNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, "BUDGET_EXCEEDED", "invalid JSON body")
		return
	}

	if req.Label == "" || req.CapMinor <= 0 {
		apierr.WriteError(w, http.StatusBadRequest, "BUDGET_EXCEEDED", "label and cap_minor are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	node, err := h.tree.Create(ctx, req)
	if err != nil {
		h.log.Error("create_node_failed", map[string]interface{}{"error": err.Error()})
		apierr.WriteError(w, http.StatusInternalServerError, "BUDGET_EXCEEDED", "failed to create budget node")
		return
	}

	h.log.Info("node_created", map[string]interface{}{"node_id": node.ID, "label": node.Label})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(node)
}

func (h *Handler) HandleUpdateNode(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	var req struct {
		CapMinor      *int64          `json:"cap_minor"`
		VelocityRules json.RawMessage `json:"velocity_rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, "BUDGET_EXCEEDED", "invalid JSON body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	node, err := h.tree.Update(ctx, nodeID, req.CapMinor, req.VelocityRules)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, "BUDGET_EXCEEDED", "failed to update budget node")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(node)
}

func (h *Handler) HandleGetNode(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	node, err := h.tree.Get(ctx, nodeID)
	if err != nil {
		apierr.WriteError(w, http.StatusNotFound, "BUDGET_EXCEEDED", "budget node not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(node)
}

func (h *Handler) HandleGetUsage(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	usage, err := h.tree.GetUsage(ctx, nodeID)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, "BUDGET_EXCEEDED", "failed to get usage")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(usage)
}

func (h *Handler) HandleGetByLabel(w http.ResponseWriter, r *http.Request) {
	label := r.PathValue("label")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	node, err := h.tree.GetNodeByLabel(ctx, label)
	if err != nil {
		apierr.WriteError(w, http.StatusNotFound, "BUDGET_EXCEEDED", "budget node not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(node)
}

func (h *Handler) HandleReserve(w http.ResponseWriter, r *http.Request) {
	var req reserve.ReserveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, "BUDGET_EXCEEDED", "invalid JSON body")
		return
	}

	if len(req.Path) == 0 || req.AmountMinor <= 0 || req.IdempotencyKey == "" {
		apierr.WriteError(w, http.StatusBadRequest, "BUDGET_EXCEEDED", "path, amount_minor, and idempotency_key are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.reserve.Reserve(ctx, req)
	if err != nil {
		if bee, ok := err.(*reserve.BudgetExceededError); ok {
			details := map[string]interface{}{
				"node_id":          bee.NodeID,
				"cap_minor":        bee.CapMinor,
				"committed_minor":  bee.CommittedMinor,
				"reserved_minor":   bee.ReservedMinor,
			}
			apierr.WriteError(w, http.StatusConflict, apierr.BudgetExceeded,
				fmt.Sprintf("budget exceeded at node %s", bee.NodeID), details)
			return
		}
		h.log.Error("reserve_failed", map[string]interface{}{"error": err.Error()})
		apierr.WriteError(w, http.StatusInternalServerError, "BUDGET_EXCEEDED", "reservation failed")
		return
	}

	for _, nodeID := range req.Path {
		_ = h.tree.LedgerInsert(ctx, resp.ReservationID, nodeID, "RESERVE", req.IdempotencyKey, req.AmountMinor)
	}

	h.log.Info("reservation_held", map[string]interface{}{
		"reservation_id": resp.ReservationID,
		"amount":         req.AmountMinor,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) HandleCommit(w http.ResponseWriter, r *http.Request) {
	reservationID := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	state, err := h.reserve.Commit(ctx, reservationID)
	if err != nil {
		h.log.Error("commit_failed", map[string]interface{}{"error": err.Error(), "reservation_id": reservationID})
		apierr.WriteError(w, http.StatusInternalServerError, "BUDGET_EXCEEDED", "commit failed: "+err.Error())
		return
	}

	h.log.Info("reservation_committed", map[string]interface{}{"reservation_id": reservationID})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"state": state})
}

func (h *Handler) HandleRelease(w http.ResponseWriter, r *http.Request) {
	reservationID := r.PathValue("id")
	var req struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	state, err := h.reserve.Release(ctx, reservationID, req.Reason)
	if err != nil {
		h.log.Error("release_failed", map[string]interface{}{"error": err.Error(), "reservation_id": reservationID})
		apierr.WriteError(w, http.StatusInternalServerError, "BUDGET_EXCEEDED", "release failed: "+err.Error())
		return
	}

	h.log.Info("reservation_released", map[string]interface{}{"reservation_id": reservationID})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"state": state})
}

func (h *Handler) HandleNaiveReserve(w http.ResponseWriter, r *http.Request) {
	var req reserve.ReserveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, "BUDGET_EXCEEDED", "invalid JSON body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	reservationID := idgen.New()

	for _, nodeID := range req.Path {
		capStr, _ := h.rdb.Get(ctx, fmt.Sprintf("budget:node:%s:cap", nodeID)).Result()
		resStr, _ := h.rdb.Get(ctx, fmt.Sprintf("budget:node:%s:reserved", nodeID)).Result()
		commStr, _ := h.rdb.Get(ctx, fmt.Sprintf("budget:node:%s:committed", nodeID)).Result()

		cap, _ := strconv.ParseInt(capStr, 10, 64)
		res, _ := strconv.ParseInt(resStr, 10, 64)
		comm, _ := strconv.ParseInt(commStr, 10, 64)

		if comm+res+req.AmountMinor > cap {
			apierr.WriteError(w, http.StatusConflict, apierr.BudgetExceeded,
				fmt.Sprintf("naive budget exceeded at node %s", nodeID))
			return
		}
	}

	for _, nodeID := range req.Path {
		resStr, _ := h.rdb.Get(ctx, fmt.Sprintf("budget:node:%s:reserved", nodeID)).Result()
		res, _ := strconv.ParseInt(resStr, 10, 64)
		h.rdb.Set(ctx, fmt.Sprintf("budget:node:%s:reserved", nodeID), res+req.AmountMinor, 0)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(reserve.ReserveResponse{ReservationID: reservationID, State: "HELD"})
}

func (h *Handler) HandleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := h.rdb.Ping(ctx).Err(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "unavailable"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *Handler) HandleListNodes(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	rows, err := h.tree.ListAll(ctx)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, "BUDGET_EXCEEDED", "failed to list nodes")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rows)
}

func (h *Handler) HandleVelocityCheck(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agent_id")
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	bucket := time.Now().Unix() / 60
	key := fmt.Sprintf("velocity:%s:%d", agentID, bucket)

	count, _ := h.rdb.Incr(ctx, key).Result()
	h.rdb.Expire(ctx, key, 60*time.Second)

	cap := int64(5)
	if v := os.Getenv("THROTTLE_VELOCITY_CAP_PER_MIN"); v != "" {
		cap, _ = strconv.ParseInt(v, 10, 64)
	}

	allowed := count <= cap

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"allowed": allowed,
		"count":   count,
		"cap":     cap,
	})
}
