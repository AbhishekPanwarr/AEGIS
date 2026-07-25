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

func (s *Store) Close(ctx context.Context) error {
	s.pool.Close()
	return nil
}

func (s *Store) HealthCheck(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Agent represents an agent record.
type Agent struct {
	ID        string `json:"id"`
	GroupID   string `json:"group_id"`
	Persona   string `json:"persona"`
	Status    string `json:"status"`
	Pubkey    []byte `json:"-"`
	PubkeyB64 string `json:"pubkey,omitempty"`
}

func (s *Store) GetAgent(ctx context.Context, agentID string) (*Agent, error) {
	var a Agent
	var pubkey []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, group_id, persona, status, pubkey
		FROM agents WHERE id = $1
	`, agentID).Scan(&a.ID, &a.GroupID, &a.Persona, &a.Status, &pubkey)
	if err != nil {
		return nil, fmt.Errorf("get agent %s: %w", agentID, err)
	}
	a.Pubkey = pubkey
	return &a, nil
}

func (s *Store) ListAgents(ctx context.Context) ([]Agent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, group_id, persona, status
		FROM agents ORDER BY persona
	`)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	var result []Agent
	for rows.Next() {
		var a Agent
		if err := rows.Scan(&a.ID, &a.GroupID, &a.Persona, &a.Status); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, nil
}

// Mandate represents a mandate record.
type Mandate struct {
	ID                string          `json:"id"`
	Issuer            string          `json:"issuer"`
	PurposeCode       string          `json:"purpose_code"`
	CounterpartyScope json.RawMessage `json:"counterparty_scope"`
	CeilingMinor      int64           `json:"ceiling_minor"`
	Currency          string          `json:"currency"`
	ValidFrom         time.Time       `json:"valid_from"`
	ValidTo           time.Time       `json:"valid_to"`
	MaxDepth          int             `json:"max_depth"`
	Recurrence        json.RawMessage `json:"recurrence"`
	Nonce             string          `json:"nonce"`
	Status            string          `json:"status"`
	SignerPubkey      []byte          `json:"-"`
	Signature         []byte          `json:"-"`
	GroupID           string          `json:"group_id"`
}

func (s *Store) GetMandate(ctx context.Context, mandateID string) (*Mandate, error) {
	var m Mandate
	err := s.pool.QueryRow(ctx, `
		SELECT m.id, m.issuer, m.purpose_code, m.counterparty_scope,
		       m.ceiling_minor, m.currency, m.valid_from, m.valid_to,
		       m.max_depth, m.recurrence, m.nonce, m.status,
		       m.signer_pubkey, m.signature,
		       a.group_id
		FROM mandates m
		JOIN agents a ON a.id = $1::text::uuid
		WHERE m.id = $2
		LIMIT 1
	`, "", mandateID).Scan(
		&m.ID, &m.Issuer, &m.PurposeCode, &m.CounterpartyScope,
		&m.CeilingMinor, &m.Currency, &m.ValidFrom, &m.ValidTo,
		&m.MaxDepth, &m.Recurrence, &m.Nonce, &m.Status,
		&m.SignerPubkey, &m.Signature,
		&m.GroupID,
	)
	if err != nil {
		// Fallback: get mandate without joining agents (group_id may not be needed)
		err = s.pool.QueryRow(ctx, `
			SELECT id, issuer, purpose_code, counterparty_scope,
			       ceiling_minor, currency, valid_from, valid_to,
			       max_depth, recurrence, nonce, status,
			       signer_pubkey, signature
			FROM mandates WHERE id = $1
		`, mandateID).Scan(
			&m.ID, &m.Issuer, &m.PurposeCode, &m.CounterpartyScope,
			&m.CeilingMinor, &m.Currency, &m.ValidFrom, &m.ValidTo,
			&m.MaxDepth, &m.Recurrence, &m.Nonce, &m.Status,
			&m.SignerPubkey, &m.Signature,
		)
		if err != nil {
			return nil, fmt.Errorf("get mandate %s: %w", mandateID, err)
		}
	}
	return &m, nil
}

func (s *Store) CreateMandate(ctx context.Context, m *Mandate) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO mandates
		  (id, issuer, purpose_code, counterparty_scope, ceiling_minor, currency,
		   valid_from, valid_to, max_depth, recurrence, nonce, status,
		   signer_pubkey, signature)
		VALUES
		  ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'live', $12, $13)
	`, m.ID, m.Issuer, m.PurposeCode, m.CounterpartyScope, m.CeilingMinor, m.Currency,
		m.ValidFrom, m.ValidTo, m.MaxDepth, m.Recurrence, m.Nonce,
		m.SignerPubkey, m.Signature)
	if err != nil {
		return fmt.Errorf("create mandate: %w", err)
	}
	return nil
}

func (s *Store) RevokeMandate(ctx context.Context, mandateID, reason string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE mandates
		SET status = 'revoked', revoked_at = now(), revoked_reason = $2
		WHERE id = $1 AND status = 'live'
	`, mandateID, reason)
	if err != nil {
		return fmt.Errorf("revoke mandate: %w", err)
	}
	return nil
}
