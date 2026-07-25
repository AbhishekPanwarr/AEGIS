package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Clients struct {
	Rail   *RailClient
	Budget *BudgetClient
	Audit  *AuditClient
}

func New(railsAddr, budgetAddr, auditAddr string) *Clients {
	return &Clients{
		Rail:   NewRailClient(railsAddr),
		Budget: NewBudgetClient(budgetAddr),
		Audit:  NewAuditClient(auditAddr),
	}
}

// --- Rail client ---

type RailClient struct{ baseURL string; client *http.Client }

func NewRailClient(baseURL string) *RailClient {
	return &RailClient{baseURL: baseURL, client: &http.Client{Timeout: 10 * time.Second}}
}

type RailExecuteRequest struct {
	ActionType     string `json:"action_type"`
	CounterpartyID string `json:"counterparty_id"`
	AmountMinor    int64  `json:"amount_minor"`
}

func (c *RailClient) Execute(ctx context.Context, req RailExecuteRequest) (map[string]interface{}, error) {
	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/execute", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call rail: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode != 200 {
		return result, fmt.Errorf("rail returned %d", resp.StatusCode)
	}
	return result, nil
}

// --- Budget client ---

type BudgetClient struct{ baseURL string; client *http.Client }

func NewBudgetClient(baseURL string) *BudgetClient {
	return &BudgetClient{baseURL: baseURL, client: &http.Client{Timeout: 5 * time.Second}}
}

func (c *BudgetClient) Release(ctx context.Context, reservationID string) error {
	body, _ := json.Marshal(map[string]string{"reason": "halt_compensation"})
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/v1/reservations/%s/release", c.baseURL, reservationID), bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("call budget release: %w", err)
	}
	resp.Body.Close()
	return nil
}

// --- Audit client ---

type AuditClient struct{ baseURL string; client *http.Client }

func NewAuditClient(baseURL string) *AuditClient {
	return &AuditClient{baseURL: baseURL, client: &http.Client{Timeout: 5 * time.Second}}
}

type AuditStubRequest struct {
	ActionType  string          `json:"action_type"`
	FullContext json.RawMessage `json:"full_context"`
}

type AuditStubResponse struct {
	DecisionID string `json:"decision_id"`
}

func (c *AuditClient) CreateStub(ctx context.Context, req AuditStubRequest) (*AuditStubResponse, error) {
	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/decisions", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call audit: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		return nil, fmt.Errorf("audit returned %d", resp.StatusCode)
	}

	var r AuditStubResponse
	json.NewDecoder(resp.Body).Decode(&r)
	return &r, nil
}

type AuditFinalizeRequest struct {
	Decision   string `json:"decision"`
	ReasonCode string `json:"reason_code"`
	LatencyMs  *int64 `json:"latency_ms"`
}

func (c *AuditClient) Finalize(ctx context.Context, decisionID string, req AuditFinalizeRequest) error {
	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(ctx, "PATCH", fmt.Sprintf("%s/v1/decisions/%s/finalize", c.baseURL, decisionID), bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("call audit finalize: %w", err)
	}
	resp.Body.Close()
	return nil
}
