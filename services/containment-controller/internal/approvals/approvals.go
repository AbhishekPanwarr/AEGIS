package approvals

import (
	"context"
	"encoding/json"
	"fmt"

	"aegis/pkg/idgen"
	"aegis/pkg/telemetry"
	"aegis/services/containment-controller/internal/store"
)

type Service struct {
	store  *store.Store
	logger *telemetry.Logger
}

func New(st *store.Store, logger *telemetry.Logger) *Service {
	return &Service{store: st, logger: logger}
}

type CreateRequest struct {
	DecisionContext json.RawMessage `json:"decision_context"`
	AgentID         string          `json:"agent_id"`
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (string, error) {
	approvalID := idgen.New()
	if err := s.store.CreateApproval(ctx, approvalID, req.AgentID, req.DecisionContext); err != nil {
		return "", fmt.Errorf("create approval: %w", err)
	}
	s.logger.Info("approval_created", map[string]interface{}{
		"approval_id": approvalID, "agent_id": req.AgentID,
	})
	return approvalID, nil
}

func (s *Service) GetPending(ctx context.Context) ([]store.ApprovalRequest, error) {
	return s.store.GetPendingApprovals(ctx)
}

func (s *Service) Get(ctx context.Context, id string) (*store.ApprovalRequest, error) {
	return s.store.GetApproval(ctx, id)
}

type DecideRequest struct {
	Decision      string `json:"decision"`
	Actor         string `json:"actor"`
	RationaleCode string `json:"rationale_code"`
}

func (s *Service) Decide(ctx context.Context, approvalID string, req DecideRequest) (*store.ApprovalRequest, error) {
	if req.Decision != "APPROVED" && req.Decision != "DENIED" {
		return nil, fmt.Errorf("invalid decision: %s", req.Decision)
	}

	if err := s.store.DecideApproval(ctx, approvalID, req.Decision, req.Actor, req.RationaleCode); err != nil {
		return nil, fmt.Errorf("decide approval: %w", err)
	}

	ar, err := s.store.GetApproval(ctx, approvalID)
	if err != nil {
		return nil, err
	}

	s.logger.Info("approval_decided", map[string]interface{}{
		"approval_id": approvalID, "decision": req.Decision, "actor": req.Actor,
	})

	return ar, nil
}
