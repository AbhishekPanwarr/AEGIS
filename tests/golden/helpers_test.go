package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"aegis/pkg/capability"
)

const (
	gatewayURL   = "http://localhost:8080"
	identityURL  = "http://localhost:8081"
	budgetURL    = "http://localhost:8082"
	procurementAgent = "00000000-0000-0000-0000-000000000010"
	treasuryAgent    = "00000000-0000-0000-0000-000000000020"
	refundsAgent     = "00000000-0000-0000-0000-000000000030"
	collectionsAgent = "00000000-0000-0000-0000-000000000040"
)

var mandateIDs = map[string]string{}
var rootBudgetNodeID string

func TestMain(m *testing.M) {
	if os.Getenv("AEGIS_TEST_SKIP_SEED") != "true" {
		// Discover mandate IDs and budget node from seed data
		discoverSeededData()
	}
	os.Exit(m.Run())
}

func discoverSeededData() {
	// Get all mandates by trying the 4 known agents
	resp, err := http.Get(identityURL + "/v1/agents/" + procurementAgent)
	if err != nil || resp.StatusCode != 200 {
		fmt.Println("[golden] WARNING: could not discover agents — is the stack running and seeded?")
		return
	}
	defer resp.Body.Close()

	// Try to mint a token for procurement agent to discover mandate
	// The seed script creates mandates but we need to find them
	// We'll query postgres directly for mandate IDs
	resp2, err := http.Get(budgetURL + "/v1/budget-node-by-label/root")
	if err == nil && resp2.StatusCode == 200 {
		var node struct {
			ID string `json:"id"`
		}
		json.NewDecoder(resp2.Body).Decode(&node)
		rootBudgetNodeID = node.ID
		resp2.Body.Close()
	}
	if rootBudgetNodeID == "" {
		fmt.Println("[golden] WARNING: could not discover root budget node")
	}
}

func mintToken(t *testing.T, agentID, mandateID string, caveats []capability.Caveat) string {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"agent_id":   agentID,
		"mandate_id": mandateID,
		"caveats":    caveats,
	})
	resp, err := http.Post(identityURL+"/v1/tokens/mint", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("mint token: status %d: %s", resp.StatusCode, string(b))
	}
	var tok map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&tok)
	b, _ := json.Marshal(tok)
	return string(b)
}

type actResponse struct {
	Status     string `json:"status"`
	DecisionID string `json:"decision_id"`
	RailRef    string `json:"rail_ref"`
}

type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func sendAct(t *testing.T, token, actionType, counterparty string, amount int64, idempKey string, budgetPath []string) (*http.Response, []byte) {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"token":           token,
		"action_type":     actionType,
		"counterparty_id": counterparty,
		"amount_minor":    amount,
		"idempotency_key": idempKey,
		"budget_path":     budgetPath,
	})
	resp, err := http.Post(gatewayURL+"/v1/act", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /v1/act: %v", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, respBody
}

func assertAllow(t *testing.T, resp *http.Response, body []byte) {
	t.Helper()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d: %s", resp.StatusCode, string(body))
		return
	}
	var r actResponse
	if err := json.Unmarshal(body, &r); err != nil {
		t.Errorf("decode response: %v", err)
		return
	}
	if r.Status != "executed" {
		t.Errorf("expected status=executed, got %s", r.Status)
	}
}

func assertDeny(t *testing.T, resp *http.Response, body []byte, expectedCode string) {
	t.Helper()
	if resp.StatusCode == 200 {
		t.Errorf("expected denial, got 200: %s", string(body))
		return
	}
	var e errorResponse
	if err := json.Unmarshal(body, &e); err != nil {
		t.Errorf("decode error response: %v body=%s", err, string(body))
		return
	}
	if e.Error.Code != expectedCode {
		t.Errorf("expected error code %s, got %s (body=%s)", expectedCode, e.Error.Code, string(body))
	}
}

func dummyToken() string {
	return `{"token_id":"x","agent_id":"x","mandate_id":"x","caveats":[],"chain":[],"epoch_fleet":0,"epoch_group":0,"epoch_agent":0,"epoch_mandate":0,"iat":0,"exp":0,"sig":"fake"}`
}

func expiredToken() string {
	return `{"token_id":"x","agent_id":"x","mandate_id":"x","caveats":[],"chain":[],"epoch_fleet":0,"epoch_group":0,"epoch_agent":0,"epoch_mandate":0,"iat":100,"exp":101,"sig":"AAAA"}`
}

func garbledToken() string {
	return `not-json-at-all`
}

func _unusedHelpers() {
	_ = strings.TrimSpace
	_ = time.Now
}
