package clients

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"aegis/pkg/capability"
)

type RailClient struct {
	baseURL string
	client  *http.Client
}

func NewRailClient(baseURL string) *RailClient {
	return &RailClient{baseURL: baseURL, client: &http.Client{Timeout: 5 * time.Second}}
}

type RailExecuteRequest struct {
	ActionType     string `json:"action_type"`
	CounterpartyID string `json:"counterparty_id"`
	AmountMinor    int64  `json:"amount_minor"`
}

type RailExecuteResponse struct {
	Status  string `json:"status"`
	RailRef string `json:"rail_ref"`
}

func (c *RailClient) Execute(ctx context.Context, req RailExecuteRequest) (*RailExecuteResponse, error) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/execute", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create rail request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call rail: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rail returned status %d", resp.StatusCode)
	}

	var r RailExecuteResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("decode rail response: %w", err)
	}
	return &r, nil
}

type IdentityClient struct {
	baseURL string
	client  *http.Client
}

func NewIdentityClient(baseURL string) *IdentityClient {
	return &IdentityClient{baseURL: baseURL, client: &http.Client{Timeout: 5 * time.Second}}
}

func (c *IdentityClient) GetPubkey(ctx context.Context) ([]byte, error) {
	resp, err := c.client.Get(c.baseURL + "/v1/pubkey")
	if err != nil {
		return nil, fmt.Errorf("get pubkey: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Pubkey string `json:"pubkey"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode pubkey: %w", err)
	}

	return base64.StdEncoding.DecodeString(result.Pubkey)
}

type AgentInfo struct {
	ID      string `json:"id"`
	GroupID string `json:"group_id"`
	Persona string `json:"persona"`
	Status  string `json:"status"`
}

func (c *IdentityClient) GetAgent(ctx context.Context, agentID string) (*AgentInfo, error) {
	resp, err := c.client.Get(c.baseURL + "/v1/agents/" + agentID)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get agent: status %d", resp.StatusCode)
	}

	var a AgentInfo
	if err := json.NewDecoder(resp.Body).Decode(&a); err != nil {
		return nil, fmt.Errorf("decode agent: %w", err)
	}
	return &a, nil
}

type MandateInfo struct {
	ID                string          `json:"id"`
	CounterpartyScope json.RawMessage `json:"counterparty_scope"`
	CeilingMinor      int64           `json:"ceiling_minor"`
	Currency          string          `json:"currency"`
	ValidFrom         string          `json:"valid_from"`
	ValidTo           string          `json:"valid_to"`
	MaxDepth          int             `json:"max_depth"`
	Status            string          `json:"status"`
}

func (c *IdentityClient) GetMandate(ctx context.Context, mandateID string) (*MandateInfo, error) {
	resp, err := c.client.Get(c.baseURL + "/v1/mandates/" + mandateID)
	if err != nil {
		return nil, fmt.Errorf("get mandate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get mandate: status %d", resp.StatusCode)
	}

	var m MandateInfo
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("decode mandate: %w", err)
	}
	return &m, nil
}

type PolicyClient struct {
	baseURL string
	client  *http.Client
}

func NewPolicyClient(baseURL string) *PolicyClient {
	return &PolicyClient{baseURL: baseURL, client: &http.Client{Timeout: 5 * time.Second}}
}

type CedarRequest struct {
	Principal string                 `json:"principal"`
	Action    string                 `json:"action"`
	Resource  string                 `json:"resource"`
	Context   map[string]interface{} `json:"context"`
}

type CedarDiagnostics struct {
	Reason []string `json:"reason"`
	Errors []string `json:"errors"`
}

type CedarResponse struct {
	Decision    string          `json:"decision"`
	Diagnostics CedarDiagnostics `json:"diagnostics"`
}

func (c *PolicyClient) IsAuthorized(ctx context.Context, req CedarRequest) (*CedarResponse, error) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/is_authorized", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create cedar request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call cedar-agent: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cedar-agent returned status %d", resp.StatusCode)
	}

	var cr CedarResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return nil, fmt.Errorf("decode cedar response: %w", err)
	}
	return &cr, nil
}

type BudgetClient struct {
	baseURL string
	client  *http.Client
}

func NewBudgetClient(baseURL string) *BudgetClient {
	return &BudgetClient{baseURL: baseURL, client: &http.Client{Timeout: 5 * time.Second}}
}

type BudgetReserveRequest struct {
	Path           []string `json:"path"`
	AmountMinor    int64    `json:"amount_minor"`
	IdempotencyKey string   `json:"idempotency_key"`
}

type BudgetReserveResponse struct {
	ReservationID string `json:"reservation_id"`
	State         string `json:"state"`
}

func (c *BudgetClient) Reserve(ctx context.Context, req BudgetReserveRequest) (*BudgetReserveResponse, error) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/reservations", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create reserve request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call budget-ledger reserve: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		var errResp struct {
			Error struct {
				Code    string                 `json:"code"`
				Message string                 `json:"message"`
				Details map[string]interface{} `json:"details"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, &BudgetExceededError{Details: errResp.Error.Details}
	}

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("budget reserve returned status %d", resp.StatusCode)
	}

	var r BudgetReserveResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("decode reserve response: %w", err)
	}
	return &r, nil
}

type BudgetExceededError struct {
	Details map[string]interface{}
}

func (e *BudgetExceededError) Error() string {
	return "budget exceeded"
}

func (c *BudgetClient) Commit(ctx context.Context, reservationID string) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/v1/reservations/%s/commit", c.baseURL, reservationID), bytes.NewReader([]byte("{}")))
	if err != nil {
		return fmt.Errorf("create commit request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("call budget-ledger commit: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("budget commit returned status %d", resp.StatusCode)
	}
	return nil
}

func (c *BudgetClient) Release(ctx context.Context, reservationID string) error {
	body, _ := json.Marshal(map[string]string{"reason": "rail_error"})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/v1/reservations/%s/release", c.baseURL, reservationID), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create release request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("call budget-ledger release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("budget release returned status %d", resp.StatusCode)
	}
	return nil
}

type AuditClient struct {
	baseURL string
	client  *http.Client
}

func NewAuditClient(baseURL string) *AuditClient {
	return &AuditClient{baseURL: baseURL, client: &http.Client{Timeout: 5 * time.Second}}
}

type AuditStubRequest struct {
	AgentID     *string         `json:"agent_id"`
	MandateID   *string         `json:"mandate_id"`
	TokenID     *string         `json:"token_id"`
	ActionType  string          `json:"action_type"`
	FullContext json.RawMessage `json:"full_context"`
}

type AuditStubResponse struct {
	DecisionID string `json:"decision_id"`
	Seq        int64  `json:"seq"`
}

func (c *AuditClient) CreateStub(ctx context.Context, req AuditStubRequest) (*AuditStubResponse, error) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/decisions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create audit request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call audit-chain: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("audit-chain returned status %d", resp.StatusCode)
	}

	var r AuditStubResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("decode audit response: %w", err)
	}
	return &r, nil
}

type AuditFinalizeRequest struct {
	Decision   string `json:"decision"`
	ReasonCode string `json:"reason_code"`
	LatencyMs  *int64 `json:"latency_ms"`
}

func (c *AuditClient) Finalize(ctx context.Context, decisionID string, req AuditFinalizeRequest) error {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, fmt.Sprintf("%s/v1/decisions/%s/finalize", c.baseURL, decisionID), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create finalize request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("call audit-chain finalize: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("audit-chain finalize returned status %d", resp.StatusCode)
	}
	return nil
}

func _unused() {
	_ = capability.Caveat{}
}

// --- Containment client ---

type ContainmentClient struct {
	baseURL string
	client  *http.Client
}

func NewContainmentClient(baseURL string) *ContainmentClient {
	return &ContainmentClient{baseURL: baseURL, client: &http.Client{Timeout: 5 * time.Second}}
}

func (c *ContainmentClient) GetLevel(ctx context.Context, agentID, groupID, mandateID string) (string, error) {
	url := fmt.Sprintf("%s/v1/containment/level?agent_id=%s&group_id=%s&mandate_id=%s",
		c.baseURL, agentID, groupID, mandateID)

	resp, err := c.client.Get(url)
	if err != nil {
		return "OBSERVE", nil // fail open on connection error
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "OBSERVE", nil
	}

	var result struct {
		Level string `json:"level"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "OBSERVE", nil
	}
	return result.Level, nil
}

type ApprovalCreateRequest struct {
	DecisionContext json.RawMessage `json:"decision_context"`
	AgentID         string          `json:"agent_id"`
}

func (c *ContainmentClient) CreateApproval(ctx context.Context, req ApprovalCreateRequest) (string, error) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/approvals", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("create approval: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		return "", fmt.Errorf("create approval returned %d", resp.StatusCode)
	}

	var result struct {
		ApprovalID string `json:"approval_id"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.ApprovalID, nil
}

type ApprovalInfo struct {
	ID              string          `json:"id"`
	DecisionContext json.RawMessage `json:"decision_context"`
	AgentID         string          `json:"agent_id"`
	State           string          `json:"state"`
}

func (c *ContainmentClient) GetApproval(ctx context.Context, approvalID string) (*ApprovalInfo, error) {
	resp, err := c.client.Get(fmt.Sprintf("%s/v1/approvals/%s", c.baseURL, approvalID))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("get approval: status %d", resp.StatusCode)
	}

	var ar ApprovalInfo
	json.NewDecoder(resp.Body).Decode(&ar)
	return &ar, nil
}

// VelocityCheck calls budget-ledger to check if agent exceeded velocity cap.
func (c *BudgetClient) VelocityCheck(ctx context.Context, agentID string) (bool, error) {
	resp, err := c.client.Get(fmt.Sprintf("%s/v1/velocity-check/%s", c.baseURL, agentID))
	if err != nil {
		return true, nil // fail open
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return true, nil
	}

	var result struct {
		Allowed bool  `json:"allowed"`
		Count   int64 `json:"count"`
		Cap     int64 `json:"cap"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Allowed, nil
}
