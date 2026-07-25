package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"aegis/pkg/telemetry"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool   *pgxpool.Pool
	logger *telemetry.Logger
}

func New(ctx context.Context, logger *telemetry.Logger) (*Store, error) {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		return nil, fmt.Errorf("POSTGRES_DSN not set")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	logger.Info("postgres_connected")
	return &Store{pool: pool, logger: logger}, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) HealthCheck(ctx context.Context) error { return s.pool.Ping(ctx) }

// --- containment_state ---

type ContainmentState struct {
	ScopeType string `json:"scope_type"`
	ScopeID   string `json:"scope_id"`
	Level     string `json:"level"`
	Reason    string `json:"reason,omitempty"`
	Actor     string `json:"actor"`
}

func (s *Store) GetContainmentState(ctx context.Context, scopeType, scopeID string) (*ContainmentState, error) {
	var cs ContainmentState
	err := s.pool.QueryRow(ctx, `
		SELECT scope_type, scope_id, level, COALESCE(reason, ''), actor
		FROM containment_state WHERE scope_type = $1 AND scope_id = $2
	`, scopeType, scopeID).Scan(&cs.ScopeType, &cs.ScopeID, &cs.Level, &cs.Reason, &cs.Actor)
	if err != nil {
		return nil, err
	}
	return &cs, nil
}

func (s *Store) UpsertContainmentState(ctx context.Context, cs ContainmentState) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO containment_state (scope_type, scope_id, level, reason, actor)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (scope_type, scope_id) DO UPDATE SET level = $3, reason = $4, actor = $5
	`, cs.ScopeType, cs.ScopeID, cs.Level, cs.Reason, cs.Actor)
	return err
}

func (s *Store) GetAllContainmentStates(ctx context.Context) ([]ContainmentState, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT scope_type, scope_id, level, COALESCE(reason, ''), actor
		FROM containment_state ORDER BY scope_type, scope_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ContainmentState
	for rows.Next() {
		var cs ContainmentState
		if err := rows.Scan(&cs.ScopeType, &cs.ScopeID, &cs.Level, &cs.Reason, &cs.Actor); err != nil {
			return nil, err
		}
		result = append(result, cs)
	}
	return result, nil
}

// --- containment_events ---

type ContainmentEvent struct {
	ID            int64  `json:"id"`
	ScopeType     string `json:"scope_type"`
	ScopeID       string `json:"scope_id"`
	FromLevel     string `json:"from_level,omitempty"`
	ToLevel       string `json:"to_level"`
	Reason        string `json:"reason,omitempty"`
	Actor         string `json:"actor"`
	EpochBumpedTo int64  `json:"epoch_bumped_to,omitempty"`
	CreatedAt     string `json:"created_at"`
}

func (s *Store) InsertContainmentEvent(ctx context.Context, ev ContainmentEvent) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO containment_events (scope_type, scope_id, from_level, to_level, reason, actor, epoch_bumped_to)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, ev.ScopeType, ev.ScopeID, ev.FromLevel, ev.ToLevel, ev.Reason, ev.Actor, ev.EpochBumpedTo)
	return err
}

func (s *Store) GetRecentEvents(ctx context.Context, limit int) ([]ContainmentEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, scope_type, scope_id, COALESCE(from_level, ''), to_level, COALESCE(reason, ''), actor, COALESCE(epoch_bumped_to, 0), created_at::text
		FROM containment_events ORDER BY id DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ContainmentEvent
	for rows.Next() {
		var ev ContainmentEvent
		if err := rows.Scan(&ev.ID, &ev.ScopeType, &ev.ScopeID, &ev.FromLevel, &ev.ToLevel, &ev.Reason, &ev.Actor, &ev.EpochBumpedTo, &ev.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, ev)
	}
	return result, nil
}

// --- sagas ---

type Saga struct {
	ID                string          `json:"id"`
	SagaType          string          `json:"saga_type"`
	State             string          `json:"state"`
	PointOfNoReturn   *int            `json:"point_of_no_return_leg,omitempty"`
	Legs              json.RawMessage `json:"legs"`
	ReservationID     *string         `json:"reservation_id,omitempty"`
	CreatedAt         string          `json:"created_at"`
	UpdatedAt         string          `json:"updated_at"`
}

func (s *Store) CreateSaga(ctx context.Context, id, sagaType string, legs json.RawMessage, reservationID *string) (*Saga, error) {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sagas (id, saga_type, state, legs, reservation_id)
		VALUES ($1, $2, 'RUNNING', $3, $4)
	`, id, sagaType, legs, reservationID)
	if err != nil {
		return nil, err
	}
	return s.GetSaga(ctx, id)
}

func (s *Store) GetSaga(ctx context.Context, id string) (*Saga, error) {
	var sg Saga
	var resID *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, saga_type, state, point_of_no_return_leg, legs, reservation_id, created_at::text, updated_at::text
		FROM sagas WHERE id = $1
	`, id).Scan(&sg.ID, &sg.SagaType, &sg.State, &sg.PointOfNoReturn, &sg.Legs, &resID, &sg.CreatedAt, &sg.UpdatedAt)
	if err != nil {
		return nil, err
	}
	sg.ReservationID = resID
	return &sg, nil
}

func (s *Store) UpdateSagaState(ctx context.Context, id, state string, pointOfNoReturn *int, legs json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sagas SET state = $2, point_of_no_return_leg = $3, legs = $4, updated_at = now() WHERE id = $1
	`, id, state, pointOfNoReturn, legs)
	return err
}

func (s *Store) GetRunningSagas(ctx context.Context) ([]Saga, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, saga_type, state, point_of_no_return_leg, legs, reservation_id, created_at::text, updated_at::text
		FROM sagas WHERE state = 'RUNNING'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Saga
	for rows.Next() {
		var sg Saga
		var resID *string
		if err := rows.Scan(&sg.ID, &sg.SagaType, &sg.State, &sg.PointOfNoReturn, &sg.Legs, &resID, &sg.CreatedAt, &sg.UpdatedAt); err != nil {
			return nil, err
		}
		sg.ReservationID = resID
		result = append(result, sg)
	}
	return result, nil
}

// --- approval_requests ---

type ApprovalRequest struct {
	ID              string          `json:"id"`
	DecisionContext json.RawMessage `json:"decision_context"`
	AgentID         string          `json:"agent_id"`
	State           string          `json:"state"`
	Approver        *string         `json:"approver,omitempty"`
	DecidedAt       *string        `json:"decided_at,omitempty"`
	RationaleCode   *string         `json:"rationale_code,omitempty"`
	RequestedAt     string          `json:"requested_at"`
}

func (s *Store) CreateApproval(ctx context.Context, id, agentID string, decisionContext json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO approval_requests (id, decision_context, agent_id, state)
		VALUES ($1, $2, $3, 'PENDING')
	`, id, decisionContext, agentID)
	return err
}

func (s *Store) GetApproval(ctx context.Context, id string) (*ApprovalRequest, error) {
	var ar ApprovalRequest
	var approver *string
	var decidedAt *string
	var rationale *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, decision_context, agent_id::text, state, approver, decided_at::text, rationale_code, requested_at::text
		FROM approval_requests WHERE id = $1
	`, id).Scan(&ar.ID, &ar.DecisionContext, &ar.AgentID, &ar.State, &approver, &decidedAt, &rationale, &ar.RequestedAt)
	if err != nil {
		return nil, err
	}
	ar.Approver = approver
	ar.DecidedAt = decidedAt
	ar.RationaleCode = rationale
	return &ar, nil
}

func (s *Store) GetPendingApprovals(ctx context.Context) ([]ApprovalRequest, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, decision_context, agent_id::text, state, approver, decided_at::text, rationale_code, requested_at::text
		FROM approval_requests WHERE state = 'PENDING' ORDER BY requested_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ApprovalRequest
	for rows.Next() {
		var ar ApprovalRequest
		var approver *string
		var decidedAt *string
		var rationale *string
		if err := rows.Scan(&ar.ID, &ar.DecisionContext, &ar.AgentID, &ar.State, &approver, &decidedAt, &rationale, &ar.RequestedAt); err != nil {
			return nil, err
		}
		ar.Approver = approver
		ar.DecidedAt = decidedAt
		ar.RationaleCode = rationale
		result = append(result, ar)
	}
	return result, nil
}

func (s *Store) DecideApproval(ctx context.Context, id, decision, approver, rationaleCode string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE approval_requests SET state = $2, approver = $3, decided_at = now(), rationale_code = $4
		WHERE id = $1 AND state = 'PENDING'
	`, id, decision, approver, rationaleCode)
	return err
}

// --- recovery_cases (stub for Phase 2) ---

func (s *Store) CreateRecoveryCase(ctx context.Context, id, agentID string, haltEventID int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO recovery_cases (id, agent_id, halt_event_id, status)
		VALUES ($1, $2, $3, 'PENDING')
	`, id, agentID, haltEventID)
	return err
}

func _unused() {
	_ = time.Now
}
