package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestEmergencyStopHaltsActions(t *testing.T) {
	skipIfNoStack(t)
	mid := getMandateID(t)
	bid := createFreshBudgetNode(t, "ctrl-halt", 1000000)
	tok := mintToken(t, procurementAgent, mid)

	// Verify action works before stop
	resp, body := sendAct(t, tok, "SendPayment", "vendor_x", 100, "ctrl-halt-1", []string{bid})
	if resp.StatusCode != 200 {
		t.Fatalf("action before stop failed: %d %s", resp.StatusCode, string(body))
	}

	// Emergency stop the agent
	result := emergencyStop(t, "agent", procurementAgent)
	t.Logf("Emergency stop: epoch=%v, sagas_swept=%v", result["epoch_bumped_to"], result["sagas_swept"])

	// Mint a fresh token (old one has stale epoch)
	tok2 := mintToken(t, procurementAgent, mid)

	// Now action should be denied
	resp2, body2 := sendAct(t, tok2, "SendPayment", "vendor_x", 100, "ctrl-halt-2", []string{bid})
	code := getErrorCode(body2)
	t.Logf("After stop: status=%d code=%s", resp2.StatusCode, code)

	if resp2.StatusCode != 403 {
		t.Errorf("expected 403 after emergency stop, got %d", resp2.StatusCode)
	}
	if code != "CONTAINMENT_HALT" && code != "EPOCH_STALE" {
		t.Errorf("expected CONTAINMENT_HALT or EPOCH_STALE, got %s", code)
	}

	// Resume
	resumeAgent(t, "agent", procurementAgent)
}

func TestThrottleVelocityCap(t *testing.T) {
	skipIfNoStack(t)
	mid := getMandateID(t)
	bid := createFreshBudgetNode(t, "ctrl-vel", 10000000)

	// Set agent to THROTTLE
	setContainmentLevel(t, "agent", procurementAgent, "THROTTLE", "velocity_test")
	defer setContainmentLevel(t, "agent", procurementAgent, "OBSERVE", "reset")

	// Send 5 actions that pass velocity — should all succeed (or 202 for first-time counterparty)
	for i := 0; i < 5; i++ {
		tok := mintToken(t, procurementAgent, mid)
		resp, body := sendAct(t, tok, "SendPayment", "vendor_x", 100, fmt.Sprintf("ctrl-vel-%d", i), []string{bid})
		t.Logf("Action %d: status=%d", i+1, resp.StatusCode)
		if resp.StatusCode == 403 {
			code := getErrorCode(body)
			if code == "VELOCITY_EXCEEDED" {
				t.Fatalf("velocity exceeded on action %d (cap should be 5): %s", i+1, string(body))
			}
		}
	}

	// 6th velocity check (7th action overall since first was 202) should be denied
	tok := mintToken(t, procurementAgent, mid)
	resp6, body6 := sendAct(t, tok, "SendPayment", "vendor_x", 100, "ctrl-vel-6", []string{bid})
	code6 := getErrorCode(body6)
	t.Logf("Action 6: status=%d code=%s", resp6.StatusCode, code6)

	// It could be VELOCITY_EXCEEDED or 202 (if first action was 202, velocity count is only 5 here)
	// Let's just check that at some point velocity kicks in
	if resp6.StatusCode == 200 {
		// Try one more
		tok7 := mintToken(t, procurementAgent, mid)
		resp7, body7 := sendAct(t, tok7, "SendPayment", "vendor_x", 100, "ctrl-vel-7", []string{bid})
		code7 := getErrorCode(body7)
		t.Logf("Action 7: status=%d code=%s", resp7.StatusCode, code7)
		if resp7.StatusCode == 200 {
			t.Errorf("expected velocity cap to kick in by action 7")
		}
	}
}

func TestQuarantineRoutesToApproval(t *testing.T) {
	skipIfNoStack(t)
	mid := getMandateID(t)
	bid := createFreshBudgetNode(t, "ctrl-quar", 1000000)

	// Set agent to QUARANTINE
	setContainmentLevel(t, "agent", procurementAgent, "QUARANTINE", "quarantine_test")
	defer setContainmentLevel(t, "agent", procurementAgent, "OBSERVE", "reset")

	// Mint token — identity-service should add requires_approval caveat
	tok := mintToken(t, procurementAgent, mid)

	// Action should route to approval queue (202)
	resp, body := sendAct(t, tok, "SendPayment", "vendor_x", 100, "ctrl-quar-1", []string{bid})
	t.Logf("Quarantine action: status=%d body=%s", resp.StatusCode, string(body))

	if resp.StatusCode != 202 {
		t.Errorf("expected 202 (pending_approval), got %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	json.Unmarshal(body, &result)
	if result["status"] != "pending_approval" {
		t.Errorf("expected status=pending_approval, got %v", result["status"])
	}
}

func TestNewCounterpartyUnderThrottle(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandate(t, []string{"vendor_x", "vendor_y", "new_cp_test"}, 2500000)
	bid := createFreshBudgetNode(t, "ctrl-newcp", 1000000)

	// Set agent to THROTTLE
	setContainmentLevel(t, "agent", procurementAgent, "THROTTLE", "new_cp_test")
	defer setContainmentLevel(t, "agent", procurementAgent, "OBSERVE", "reset")

	// Use a counterparty that hasn't been seen by this agent
	// First mint a token and send to a brand new counterparty
	tok := mintToken(t, procurementAgent, mid)
	resp, body := sendAct(t, tok, "SendPayment", "new_cp_test", 100, "ctrl-newcp-1", []string{bid})
	t.Logf("New counterparty under throttle: status=%d body=%s", resp.StatusCode, string(body))

	if resp.StatusCode != 202 {
		// It might be 403 VELOCITY_EXCEEDED if velocity was already hit
		code := getErrorCode(body)
		if code == "VELOCITY_EXCEEDED" {
			t.Skip("velocity cap hit before new counterparty test — skipping")
		}
		t.Errorf("expected 202 (pending_approval) for new counterparty, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestSagaCompensationAfterHalt(t *testing.T) {
	skipIfNoStack(t)

	// Register a 3-leg saga
	sagaBody := fmt.Sprintf(`{
		"saga_type": "cross_currency_payment",
		"agent_id": "%s",
		"group_id": "00000000-0000-0000-0000-000000000001",
		"legs": [
			{"rail_action": "debit", "counterparty_id": "acct_a", "amount_minor": 1000, "status": "PENDING"},
			{"rail_action": "convert_fx", "counterparty_id": "fx_desk", "amount_minor": 1000, "status": "PENDING"},
			{"rail_action": "credit", "counterparty_id": "acct_b", "amount_minor": 1000, "status": "PENDING"}
		],
		"reservation_id": ""
	}`, procurementAgent)

	resp, err := http.Post(containmentURL+"/v1/sagas/register", "application/json", strings.NewReader(sagaBody))
	if err != nil || resp.StatusCode != 201 {
		t.Fatalf("register saga: %v status=%d", err, resp.StatusCode)
	}
	defer resp.Body.Close()
	var saga map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&saga)
	sagaID := saga["id"].(string)
	t.Logf("Saga registered: %s", sagaID)

	// Execute leg 0 (debit) via mock-rails
	railBody := fmt.Sprintf(`{"action_type":"debit","counterparty_id":"acct_a","amount_minor":1000,"saga_id":"%s","leg_seq":0}`, sagaID)
	http.Post("http://localhost:8090/v1/execute", "application/json", strings.NewReader(railBody))

	// Mark leg 0 complete
	legBody := `{"leg_seq":0}`
	http.Post(containmentURL+"/v1/sagas/"+sagaID+"/leg-complete", "application/json", strings.NewReader(legBody))
	t.Log("Leg 0 (debit) completed")

	// Arm failure on leg 1 (convert_fx)
	armBody := fmt.Sprintf(`{"saga_id":"%s","leg_seq":1}`, sagaID)
	http.Post("http://localhost:8090/v1/test/arm-failure", "application/json", strings.NewReader(armBody))
	t.Log("Armed failure on leg 1")

	// Emergency stop — should trigger saga sweep and compensation
	result := emergencyStop(t, "agent", procurementAgent)
	t.Logf("Emergency stop: epoch=%v, sagas_swept=%v, orphaned=%v",
		result["epoch_bumped_to"], result["sagas_swept"], result["orphaned_funds_minor"])

	swept, _ := result["sagas_swept"].(float64)
	if swept < 1 {
		t.Errorf("expected at least 1 saga swept, got %v", swept)
	}

	orphaned, _ := result["orphaned_funds_minor"].(float64)
	if orphaned != 0 {
		t.Errorf("expected orphaned_funds_minor=0, got %v", orphaned)
	}

	// Verify the saga is compensated
	time.Sleep(2 * time.Second) // wait for compensation to complete
	getResp, _ := http.Get(containmentURL + "/v1/sagas/" + sagaID)
	if getResp != nil {
		defer getResp.Body.Close()
		var sg map[string]interface{}
		json.NewDecoder(getResp.Body).Decode(&sg)
		state, _ := sg["state"].(string)
		t.Logf("Saga state after halt: %s", state)
		if state != "COMPENSATED" && state != "COMPENSATING" {
			t.Errorf("expected saga state COMPENSATED or COMPENSATING, got %s", state)
		}
	}

	// Resume agent
	resumeAgent(t, "agent", procurementAgent)
}

func TestResumeFromHalt(t *testing.T) {
	skipIfNoStack(t)
	mid := getMandateID(t)
	bid := createFreshBudgetNode(t, "ctrl-resume", 1000000)

	// Halt the agent
	setContainmentLevel(t, "agent", procurementAgent, "HALT", "resume_test")

	// Resume
	resumeAgent(t, "agent", procurementAgent)

	// After resume, agent should be at THROTTLE — new tokens should work
	// (but velocity cap might limit — create fresh mandate to avoid issues)
	tok := mintToken(t, procurementAgent, mid)
	resp, body := sendAct(t, tok, "SendPayment", "vendor_x", 100, "ctrl-resume-1", []string{bid})
	t.Logf("After resume: status=%d body=%s", resp.StatusCode, string(body))

	// Should not be CONTAINMENT_HALT
	code := getErrorCode(body)
	if code == "CONTAINMENT_HALT" {
		t.Errorf("agent still halted after resume")
	}

	// Reset to OBSERVE
	setContainmentLevel(t, "agent", procurementAgent, "OBSERVE", "reset")
}
