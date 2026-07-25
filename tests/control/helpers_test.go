package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"aegis/pkg/capability"
)

const (
	gatewayURL      = "http://localhost:8080"
	identityURL     = "http://localhost:8081"
	budgetURL       = "http://localhost:8082"
	containmentURL  = "http://localhost:8083"
	procurementAgent = "00000000-0000-0000-0000-000000000010"
	treasuryAgent    = "00000000-0000-0000-0000-000000000020"
	refundsAgent     = "00000000-0000-0000-0000-000000000030"
)

func skipIfNoStack(t *testing.T) {
	t.Helper()
	resp, err := http.Get(gatewayURL + "/healthz")
	if err != nil || resp.StatusCode != 200 {
		t.Skip("stack not running")
	}
	if resp != nil {
		resp.Body.Close()
	}
}

func getMandateID(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("docker", "compose", "exec", "-T", "postgres",
		"psql", "-U", "aegis", "-d", "aegis", "-t", "-A", "-c",
		"SELECT id FROM mandates WHERE status='live' LIMIT 1").Output()
	if err != nil {
		t.Fatalf("get mandate: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func getBudgetNode(t *testing.T) string {
	t.Helper()
	resp, err := http.Get(budgetURL + "/v1/budget-node-by-label/root")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("get budget node: %v", err)
	}
	defer resp.Body.Close()
	var node struct{ ID string `json:"id"` }
	json.NewDecoder(resp.Body).Decode(&node)
	return node.ID
}

func mintToken(t *testing.T, agentID, mandateID string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"agent_id": agentID, "mandate_id": mandateID, "caveats": []interface{}{},
	})
	resp, err := http.Post(identityURL+"/v1/tokens/mint", "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != 201 {
		t.Fatalf("mint token: %v status=%d", err, resp.StatusCode)
	}
	defer resp.Body.Close()
	var tok map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&tok)
	b, _ := json.Marshal(tok)
	return string(b)
}

func sendAct(t *testing.T, token, actionType, counterparty string, amount int64, idemKey string, budgetPath []string) (*http.Response, []byte) {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"token": token, "action_type": actionType, "counterparty_id": counterparty,
		"amount_minor": amount, "idempotency_key": idemKey, "budget_path": budgetPath,
	})
	resp, err := http.Post(gatewayURL+"/v1/act", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /v1/act: %v", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, respBody
}

func setContainmentLevel(t *testing.T, scopeType, scopeID, level, reason string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"level": level, "reason": reason, "actor": "test",
	})
	req, _ := http.NewRequest("PUT", fmt.Sprintf("%s/v1/containment/%s/%s", containmentURL, scopeType, scopeID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("set containment level: %v status=%d", err, resp.StatusCode)
	}
	resp.Body.Close()
}

func emergencyStop(t *testing.T, scopeType, scopeID string) map[string]interface{} {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"scope_type": scopeType, "scope_id": scopeID,
		"reason": "test", "actor": "test",
	})
	resp, err := http.Post(containmentURL+"/v1/emergency-stop", "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("emergency stop: %v status=%d", err, resp.StatusCode)
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return result
}

func resumeAgent(t *testing.T, scopeType, scopeID string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"scope_type": scopeType, "scope_id": scopeID, "actor": "test",
	})
	resp, err := http.Post(containmentURL+"/v1/resume", "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("resume: %v status=%d", err, resp.StatusCode)
	}
	resp.Body.Close()
}

func getErrorCode(body []byte) string {
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	json.Unmarshal(body, &e)
	return e.Error.Code
}

func createMandate(t *testing.T, counterparties []string, ceiling int64) string {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"issuer": "test", "purpose_code": "TEST",
		"counterparty_scope": map[string]interface{}{"type": "allowlist", "values": counterparties},
		"ceiling_minor": ceiling, "valid_from": "2026-01-01T00:00:00.000Z",
		"valid_to": "2027-01-01T00:00:00.000Z", "max_depth": 2,
	})
	resp, err := http.Post(identityURL+"/v1/mandates", "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != 201 {
		t.Fatalf("create mandate: %v status=%d", err, resp.StatusCode)
	}
	defer resp.Body.Close()
	var m struct{ ID string `json:"id"` }
	json.NewDecoder(resp.Body).Decode(&m)
	return m.ID
}

func createFreshBudgetNode(t *testing.T, label string, cap int64) string {
	t.Helper()
	label = fmt.Sprintf("%s-%d", label, time.Now().UnixNano())
	body, _ := json.Marshal(map[string]interface{}{
		"label": label, "cap_minor": cap, "currency": "USD",
	})
	resp, err := http.Post(budgetURL+"/v1/budget-nodes", "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != 201 {
		t.Fatalf("create budget node: %v status=%d", err, resp.StatusCode)
	}
	defer resp.Body.Close()
	var node struct{ ID string `json:"id"` }
	json.NewDecoder(resp.Body).Decode(&node)
	return node.ID
}

func _unused() {
	_ = capability.Caveat{}
}
