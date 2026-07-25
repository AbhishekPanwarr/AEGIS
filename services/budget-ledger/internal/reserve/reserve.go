package reserve

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"aegis/pkg/idgen"

	"github.com/redis/go-redis/v9"
)

type Service struct {
	rdb *redis.Client
	reserveScript *redis.Script
	commitScript  *redis.Script
	releaseScript *redis.Script
}

func New(rdb *redis.Client) *Service {
	return &Service{
		rdb:           rdb,
		reserveScript: redis.NewScript(ReserveScriptSrc),
		commitScript:  redis.NewScript(CommitScriptSrc),
		releaseScript: redis.NewScript(ReleaseScriptSrc),
	}
}

type ReserveRequest struct {
	Path           []string `json:"path"`
	AmountMinor    int64    `json:"amount_minor"`
	IdempotencyKey string   `json:"idempotency_key"`
	SagaID         string   `json:"saga_id,omitempty"`
}

type ReserveResponse struct {
	ReservationID string `json:"reservation_id"`
	State         string `json:"state"`
}

type BudgetExceededError struct {
	NodeID         string
	CapMinor       int64
	CommittedMinor int64
	ReservedMinor  int64
}

func (e *BudgetExceededError) Error() string {
	return fmt.Sprintf("budget exceeded at node %s", e.NodeID)
}

func (s *Service) Reserve(ctx context.Context, req ReserveRequest) (*ReserveResponse, error) {
	reservationID := idgen.New()
	ttlSeconds := 30
	expiresAt := time.Now().Unix() + int64(ttlSeconds)

	args := []interface{}{
		req.IdempotencyKey,
		req.AmountMinor,
		reservationID,
		ttlSeconds,
		expiresAt,
	}
	for _, node := range req.Path {
		args = append(args, node)
	}

	result, err := s.reserveScript.Run(ctx, s.rdb, nil, args...).Result()
	if err != nil {
		return nil, fmt.Errorf("run reserve script: %w", err)
	}

	parts, ok := result.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected reserve script result type: %T", result)
	}

	status, _ := parts[0].(string)

	switch status {
	case "HELD":
		rid, _ := parts[1].(string)
		return &ReserveResponse{ReservationID: rid, State: "HELD"}, nil
	case "EXISTS":
		rid, _ := parts[1].(string)
		return &ReserveResponse{ReservationID: rid, State: "HELD"}, nil
	case "DENY":
		nodeID, _ := parts[1].(string)
		capVal, _ := parts[2].(interface{}).(string)
		commVal, _ := parts[3].(interface{}).(string)
		resVal, _ := parts[4].(interface{}).(string)
		cap, _ := strconv.ParseInt(capVal, 10, 64)
		comm, _ := strconv.ParseInt(commVal, 10, 64)
		res, _ := strconv.ParseInt(resVal, 10, 64)
		return nil, &BudgetExceededError{
			NodeID: nodeID, CapMinor: cap, CommittedMinor: comm, ReservedMinor: res,
		}
	default:
		return nil, fmt.Errorf("unexpected reserve status: %s", status)
	}
}

func (s *Service) Commit(ctx context.Context, reservationID string) (string, error) {
	result, err := s.commitScript.Run(ctx, s.rdb, nil, reservationID).Result()
	if err != nil {
		return "", fmt.Errorf("run commit script: %w", err)
	}

	parts, ok := result.([]interface{})
	if !ok {
		return "", fmt.Errorf("unexpected commit script result type: %T", result)
	}

	status, _ := parts[0].(string)
	if status == "ERROR" {
		msg, _ := parts[1].(string)
		return "", fmt.Errorf("commit error: %s", msg)
	}

	return status, nil
}

func (s *Service) Release(ctx context.Context, reservationID, reason string) (string, error) {
	result, err := s.releaseScript.Run(ctx, s.rdb, nil, reservationID, reason).Result()
	if err != nil {
		return "", fmt.Errorf("run release script: %w", err)
	}

	parts, ok := result.([]interface{})
	if !ok {
		return "", fmt.Errorf("unexpected release script result type: %T", result)
	}

	status, _ := parts[0].(string)
	if status == "ERROR" {
		msg, _ := parts[1].(string)
		return "", fmt.Errorf("release error: %s", msg)
	}

	return status, nil
}

func (s *Service) ReleaseInternal(ctx context.Context, reservationID, reason string) error {
	_, err := s.Release(ctx, reservationID, reason)
	return err
}
