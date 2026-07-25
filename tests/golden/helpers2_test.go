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
)

func getMandateIDForAgent(t *testing.T, agentID string) string {
	t.Helper()
	// Query postgres for the most recent live mandate
	cmd := exec.Command("docker", "compose", "exec", "-T", "postgres",
		"psql", "-U", "aegis", "-d", "aegis", "-t", "-A", "-c",
		fmt.Sprintf("SELECT id FROM mandates WHERE status='live' ORDER BY created_at DESC LIMIT 1"))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("query mandate: %v", err)
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		t.Fatal("no live mandate found — run make seed first")
	}
	return id
}

func getBudgetNodeByLabel(t *testing.T, label string) string {
	t.Helper()
	resp, err := http.Get(budgetURL + "/v1/budget-node-by-label/" + label)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("get budget node %s: %v", label, err)
	}
	defer resp.Body.Close()
	var node struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&node)
	if node.ID == "" {
		t.Fatalf("budget node %s not found — run make seed first", label)
	}
	return node.ID
}

func revokeMandate(t *testing.T, mandateID string) {
	t.Helper()
	body := fmt.Sprintf(`{"reason":"test_revocation","actor":"golden_test"}`)
	resp, err := http.Post(identityURL+"/v1/mandates/"+mandateID+"/revoke", "application/json", strReader(body))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("revoke mandate: %v", err)
	}
	resp.Body.Close()
}

func strReader(s string) *reader {
	return &reader{s: s}
}

type reader struct {
	s string
	i int
}

func (r *reader) Read(p []byte) (int, error) {
	if r.i >= len(r.s) {
		return 0, fmt.Errorf("EOF")
	}
	n := copy(p, r.s[r.i:])
	r.i += n
	return n, nil
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

func createMandateForTest(t *testing.T, counterparties []string, ceiling int64) string {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"issuer":      "test",
		"purpose_code": "TEST",
		"counterparty_scope": map[string]interface{}{"type": "allowlist", "values": counterparties},
		"ceiling_minor": ceiling,
		"valid_from":   "2026-01-01T00:00:00.000Z",
		"valid_to":     "2027-01-01T00:00:00.000Z",
		"max_depth":    2,
	})
	resp, err := http.Post(identityURL+"/v1/mandates", "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != 201 {
		t.Fatalf("create mandate: %v status=%d", err, resp.StatusCode)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var m struct{ ID string `json:"id"` }
	json.Unmarshal(respBody, &m)
	return m.ID
}
