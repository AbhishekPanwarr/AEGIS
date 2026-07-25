package sagas

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"aegis/pkg/idgen"
	"aegis/pkg/telemetry"
	"aegis/services/containment-controller/internal/clients"
	"aegis/services/containment-controller/internal/store"

	"github.com/redis/go-redis/v9"
)

type Service struct {
	store   *store.Store
	rdb     *redis.Client
	clients *clients.Clients
	logger  *telemetry.Logger
	mu      sync.Mutex
}

func New(st *store.Store, rdb *redis.Client, cl *clients.Clients, logger *telemetry.Logger) *Service {
	return &Service{store: st, rdb: rdb, clients: cl, logger: logger}
}

type Leg struct {
	RailAction     string `json:"rail_action"`
	CounterpartyID string `json:"counterparty_id"`
	AmountMinor    int64  `json:"amount_minor"`
	Status         string `json:"status"`
}

type LegsData struct {
	AgentID string `json:"agent_id"`
	GroupID string `json:"group_id"`
	Steps   []Leg  `json:"steps"`
}

type RegisterRequest struct {
	SagaType      string `json:"saga_type"`
	Legs          []Leg  `json:"legs"`
	AgentID       string `json:"agent_id"`
	GroupID       string `json:"group_id"`
	ReservationID string `json:"reservation_id,omitempty"`
}

func (s *Service) Register(ctx context.Context, req RegisterRequest) (*store.Saga, error) {
	sagaID := idgen.New()

	legsData := LegsData{
		AgentID: req.AgentID,
		GroupID: req.GroupID,
		Steps:   req.Legs,
	}
	for i := range legsData.Steps {
		if legsData.Steps[i].Status == "" {
			legsData.Steps[i].Status = "PENDING"
		}
	}

	legsJSON, _ := json.Marshal(legsData)

	var resID *string
	if req.ReservationID != "" {
		resID = &req.ReservationID
	}

	sg, err := s.store.CreateSaga(ctx, sagaID, req.SagaType, legsJSON, resID)
	if err != nil {
		return nil, fmt.Errorf("create saga: %w", err)
	}

	s.logger.Info("saga_registered", map[string]interface{}{
		"saga_id": sagaID, "saga_type": req.SagaType, "legs": len(req.Legs),
	})

	return sg, nil
}

func (s *Service) LegComplete(ctx context.Context, sagaID string, legSeq int) (*store.Saga, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sg, err := s.store.GetSaga(ctx, sagaID)
	if err != nil {
		return nil, fmt.Errorf("get saga: %w", err)
	}

	var legsData LegsData
	if err := json.Unmarshal(sg.Legs, &legsData); err != nil {
		return nil, fmt.Errorf("unmarshal legs: %w", err)
	}

	if legSeq < 0 || legSeq >= len(legsData.Steps) {
		return nil, fmt.Errorf("invalid leg_seq %d", legSeq)
	}

	legsData.Steps[legSeq].Status = "DONE"

	// Check if all legs done
	allDone := true
	for _, step := range legsData.Steps {
		if step.Status != "DONE" && step.Status != "COMPENSATED" {
			allDone = false
			break
		}
	}

	newState := sg.State
	if allDone && sg.State == "RUNNING" {
		newState = "COMPLETED"
	}

	legsJSON, _ := json.Marshal(legsData)
	if err := s.store.UpdateSagaState(ctx, sagaID, newState, sg.PointOfNoReturn, legsJSON); err != nil {
		return nil, fmt.Errorf("update saga: %w", err)
	}

	s.logger.Info("saga_leg_complete", map[string]interface{}{
		"saga_id": sagaID, "leg_seq": legSeq, "new_state": newState,
	})

	return s.store.GetSaga(ctx, sagaID)
}

func (s *Service) Sweep(ctx context.Context, scopeType, scopeID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	runningSagas, err := s.store.GetRunningSagas(ctx)
	if err != nil {
		s.logger.Error("saga_sweep_query_failed", map[string]interface{}{"error": err.Error()})
		return 0
	}

	swept := 0
	for _, sg := range runningSagas {
		var legsData LegsData
		if err := json.Unmarshal(sg.Legs, &legsData); err != nil {
			continue
		}

		// Check if saga is in scope
		inScope := false
		switch scopeType {
		case "fleet":
			inScope = true
		case "group":
			inScope = legsData.GroupID == scopeID
		case "agent":
			inScope = legsData.AgentID == scopeID
		}

		if !inScope {
			continue
		}

		s.logger.Info("saga_sweep_compensating", map[string]interface{}{
			"saga_id": sg.ID, "agent_id": legsData.AgentID,
		})

		s.compensateSaga(ctx, &sg, &legsData)
		swept++
	}

	s.logger.Info("saga_sweep_complete", map[string]interface{}{
		"scope": scopeType, "scope_id": scopeID, "swept": swept,
	})

	return swept
}

func (s *Service) compensateSaga(ctx context.Context, sg *store.Saga, legsData *LegsData) {
	// Find the last completed leg (point of no return)
	lastDone := -1
	for i, step := range legsData.Steps {
		if step.Status == "DONE" {
			lastDone = i
		}
	}

	// Mark saga as COMPENSATING
	legsJSON, _ := json.Marshal(legsData)
	por := lastDone
	_ = s.store.UpdateSagaState(ctx, sg.ID, "COMPENSATING", &por, legsJSON)

	// Compensate all completed legs in reverse order
	for i := lastDone; i >= 0; i-- {
		if legsData.Steps[i].Status != "DONE" {
			continue
		}

		leg := legsData.Steps[i]
		reverseAction := reverseAction(leg.RailAction)

		s.logger.Info("saga_compensating_leg", map[string]interface{}{
			"saga_id": sg.ID, "leg": i, "original": leg.RailAction, "reverse": reverseAction,
		})

		// Call mock-rails with the reverse action
		_, err := s.clients.Rail.Execute(ctx, clients.RailExecuteRequest{
			ActionType:     reverseAction,
			CounterpartyID: leg.CounterpartyID,
			AmountMinor:    leg.AmountMinor,
		})
		if err != nil {
			s.logger.Error("saga_compensation_rail_failed", map[string]interface{}{
				"saga_id": sg.ID, "leg": i, "error": err.Error(),
			})
		}

		// Write audit event
		auditCtx, _ := json.Marshal(map[string]interface{}{
			"saga_id": sg.ID, "leg": i, "action": reverseAction,
			"counterparty": leg.CounterpartyID, "amount": leg.AmountMinor,
		})
		stub, _ := s.clients.Audit.CreateStub(ctx, clients.AuditStubRequest{
			ActionType:  "SAGA_COMPENSATION",
			FullContext: auditCtx,
		})
		if stub != nil {
			latency := int64(0)
			_ = s.clients.Audit.Finalize(ctx, stub.DecisionID, clients.AuditFinalizeRequest{
				Decision: "ALLOW", ReasonCode: "SAGA_COMPENSATED", LatencyMs: &latency,
			})
		}

		legsData.Steps[i].Status = "COMPENSATED"
	}

	// Release budget reservation if any
	if sg.ReservationID != nil && *sg.ReservationID != "" {
		_ = s.clients.Budget.Release(ctx, *sg.ReservationID)
		s.logger.Info("saga_reservation_released", map[string]interface{}{
			"saga_id": sg.ID, "reservation_id": *sg.ReservationID,
		})
	}

	// Mark saga as COMPENSATED
	allCompensated := true
	for _, step := range legsData.Steps {
		if step.Status == "DONE" {
			allCompensated = false
			break
		}
	}
	if allCompensated {
		legsJSON, _ := json.Marshal(legsData)
		_ = s.store.UpdateSagaState(ctx, sg.ID, "COMPENSATED", &por, legsJSON)
		s.logger.Info("saga_compensated", map[string]interface{}{"saga_id": sg.ID})
	}
}

func reverseAction(action string) string {
	switch action {
	case "debit":
		return "credit"
	case "credit":
		return "debit"
	case "convert_fx":
		return "reverse_fx"
	default:
		return "reverse_" + action
	}
}
