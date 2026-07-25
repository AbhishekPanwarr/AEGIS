package tree

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"aegis/pkg/idgen"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Service struct {
	pool *pgxpool.Pool
	rdb  *redis.Client
}

func New(pool *pgxpool.Pool, rdb *redis.Client) *Service {
	return &Service{pool: pool, rdb: rdb}
}

type BudgetNode struct {
	ID            string          `json:"id"`
	ParentID      *string         `json:"parent_id,omitempty"`
	Label         string          `json:"label"`
	Currency      string          `json:"currency"`
	CapMinor      int64           `json:"cap_minor"`
	VelocityRules json.RawMessage `json:"velocity_rules,omitempty"`
}

type Usage struct {
	CapMinor       int64 `json:"cap_minor"`
	CommittedMinor int64 `json:"committed_minor"`
	ReservedMinor  int64 `json:"reserved_minor"`
	HeadroomMinor  int64 `json:"headroom_minor"`
}

type CreateNodeRequest struct {
	ParentID      *string         `json:"parent_id"`
	Label         string          `json:"label"`
	Currency      string          `json:"currency"`
	CapMinor      int64           `json:"cap_minor"`
	VelocityRules json.RawMessage `json:"velocity_rules"`
}

func (s *Service) Create(ctx context.Context, req CreateNodeRequest) (*BudgetNode, error) {
	nodeID := idgen.New()
	if req.Currency == "" {
		req.Currency = "USD"
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO budget_nodes (id, parent_id, label, currency, cap_minor, velocity_rules)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, nodeID, req.ParentID, req.Label, req.Currency, req.CapMinor, req.VelocityRules)
	if err != nil {
		return nil, fmt.Errorf("insert budget_node: %w", err)
	}

	s.rdb.Set(ctx, fmt.Sprintf("budget:node:%s:cap", nodeID), req.CapMinor, 0)
	s.rdb.Set(ctx, fmt.Sprintf("budget:node:%s:committed", nodeID), 0, 0)
	s.rdb.Set(ctx, fmt.Sprintf("budget:node:%s:reserved", nodeID), 0, 0)
	if req.ParentID != nil && *req.ParentID != "" {
		s.rdb.Set(ctx, fmt.Sprintf("budget:node:%s:parent", nodeID), *req.ParentID, 0)
	}

	return &BudgetNode{
		ID: nodeID, ParentID: req.ParentID, Label: req.Label,
		Currency: req.Currency, CapMinor: req.CapMinor, VelocityRules: req.VelocityRules,
	}, nil
}

func (s *Service) Update(ctx context.Context, nodeID string, capMinor *int64, velocityRules json.RawMessage) (*BudgetNode, error) {
	if capMinor != nil {
		_, err := s.pool.Exec(ctx, `UPDATE budget_nodes SET cap_minor = $2, updated_at = now(), updated_by = 'api' WHERE id = $1`, nodeID, *capMinor)
		if err != nil {
			return nil, fmt.Errorf("update budget_node cap: %w", err)
		}
		s.rdb.Set(ctx, fmt.Sprintf("budget:node:%s:cap", nodeID), *capMinor, 0)
	}

	if velocityRules != nil {
		_, err := s.pool.Exec(ctx, `UPDATE budget_nodes SET velocity_rules = $2, updated_at = now() WHERE id = $1`, nodeID, velocityRules)
		if err != nil {
			return nil, fmt.Errorf("update budget_node velocity: %w", err)
		}
	}

	return s.Get(ctx, nodeID)
}

func (s *Service) ListAll(ctx context.Context) ([]BudgetNode, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, parent_id, label, currency, cap_minor, velocity_rules
		FROM budget_nodes ORDER BY label
	`)
	if err != nil {
		return nil, fmt.Errorf("list budget_nodes: %w", err)
	}
	defer rows.Close()

	var result []BudgetNode
	for rows.Next() {
		var n BudgetNode
		var parentID *string
		if err := rows.Scan(&n.ID, &parentID, &n.Label, &n.Currency, &n.CapMinor, &n.VelocityRules); err != nil {
			return nil, err
		}
		n.ParentID = parentID
		result = append(result, n)
	}
	return result, nil
}

func (s *Service) Get(ctx context.Context, nodeID string) (*BudgetNode, error) {
	var n BudgetNode
	var parentID *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, parent_id, label, currency, cap_minor, velocity_rules
		FROM budget_nodes WHERE id = $1
	`, nodeID).Scan(&n.ID, &parentID, &n.Label, &n.Currency, &n.CapMinor, &n.VelocityRules)
	if err != nil {
		return nil, fmt.Errorf("get budget_node: %w", err)
	}
	n.ParentID = parentID
	return &n, nil
}

func (s *Service) GetUsage(ctx context.Context, nodeID string) (*Usage, error) {
	capStr, _ := s.rdb.Get(ctx, fmt.Sprintf("budget:node:%s:cap", nodeID)).Result()
	commStr, _ := s.rdb.Get(ctx, fmt.Sprintf("budget:node:%s:committed", nodeID)).Result()
	resStr, _ := s.rdb.Get(ctx, fmt.Sprintf("budget:node:%s:reserved", nodeID)).Result()

	cap, _ := strconv.ParseInt(capStr, 10, 64)
	comm, _ := strconv.ParseInt(commStr, 10, 64)
	res, _ := strconv.ParseInt(resStr, 10, 64)

	return &Usage{
		CapMinor:       cap,
		CommittedMinor: comm,
		ReservedMinor:  res,
		HeadroomMinor:  cap - comm - res,
	}, nil
}

func (s *Service) GetNodeByLabel(ctx context.Context, label string) (*BudgetNode, error) {
	var n BudgetNode
	var parentID *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, parent_id, label, currency, cap_minor, velocity_rules
		FROM budget_nodes WHERE label = $1 LIMIT 1
	`, label).Scan(&n.ID, &parentID, &n.Label, &n.Currency, &n.CapMinor, &n.VelocityRules)
	if err != nil {
		return nil, fmt.Errorf("get budget_node by label: %w", err)
	}
	n.ParentID = parentID
	return &n, nil
}

func (s *Service) SetCapDirectly(ctx context.Context, nodeID string, cap int64) error {
	s.rdb.Set(ctx, fmt.Sprintf("budget:node:%s:cap", nodeID), cap, 0)
	s.rdb.Set(ctx, fmt.Sprintf("budget:node:%s:committed", nodeID), 0, 0)
	s.rdb.Set(ctx, fmt.Sprintf("budget:node:%s:reserved", nodeID), 0, 0)
	return nil
}

func (s *Service) GetUsageDirect(ctx context.Context, nodeID string) (cap, comm, res int64) {
	capStr, _ := s.rdb.Get(ctx, fmt.Sprintf("budget:node:%s:cap", nodeID)).Result()
	commStr, _ := s.rdb.Get(ctx, fmt.Sprintf("budget:node:%s:committed", nodeID)).Result()
	resStr, _ := s.rdb.Get(ctx, fmt.Sprintf("budget:node:%s:reserved", nodeID)).Result()
	cap, _ = strconv.ParseInt(capStr, 10, 64)
	comm, _ = strconv.ParseInt(commStr, 10, 64)
	res, _ = strconv.ParseInt(resStr, 10, 64)
	return
}

func (s *Service) LedgerInsert(ctx context.Context, reservationID, nodeID, eventType, idempotencyKey string, amount int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO budget_ledger_events (reservation_id, node_id, event_type, amount_minor, idempotency_key)
		VALUES ($1, $2, $3, $4, $5)
	`, reservationID, nodeID, eventType, amount, idempotencyKey)
	if err != nil {
		return fmt.Errorf("insert ledger event: %w", err)
	}
	return nil
}

func _unused() {
	_ = os.Getenv
	_ = time.Now
}
