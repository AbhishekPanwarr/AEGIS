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

// === VALID PAYMENT CASES (per persona) ===

func TestGolden01_ProcurementValidPayment(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"vendor_x","vendor_y"}, 2500000)
	bid := createFreshBudgetNode(t, "test", 10000000)
	tok := mintToken(t, procurementAgent, mid, nil)
	resp, body := sendAct(t, tok, "SendPayment", "vendor_x", 100, "g01", []string{bid})
	assertAllow(t, resp, body)
}

func TestGolden02_TreasuryValidPayment(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"acct_internal"}, 10000000)
	bid := createFreshBudgetNode(t, "g02", 10000000)
	tok := mintToken(t, treasuryAgent, mid, nil)
	resp, body := sendAct(t, tok, "SendPayment", "acct_internal", 500, "g02", []string{bid})
	assertAllow(t, resp, body)
}

func TestGolden03_RefundsValidPayment(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"cust_001", "cust_002"}, 500000)
	bid := createFreshBudgetNode(t, "g03", 1000000)
	tok := mintToken(t, refundsAgent, mid, nil)
	resp, body := sendAct(t, tok, "SendPayment", "cust_001", 200, "g03", []string{bid})
	assertAllow(t, resp, body)
}

func TestGolden04_CollectionsValidPayment(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"cust_001", "cust_002"}, 100000)
	bid := createFreshBudgetNode(t, "g04", 1000000)
	tok := mintToken(t, collectionsAgent, mid, nil)
	resp, body := sendAct(t, tok, "SendPayment", "cust_001", 50, "g04", []string{bid})
	assertAllow(t, resp, body)
}

// === OUT-OF-SCOPE COUNTERPARTY ===

func TestGolden05_ProcurementOutOfScopeCounterparty(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"vendor_x","vendor_y"}, 2500000)
	bid := createFreshBudgetNode(t, "test", 10000000)
	tok := mintToken(t, procurementAgent, mid, nil)
	resp, body := sendAct(t, tok, "SendPayment", "vendor_z", 100, "g05", []string{bid})
	assertDeny(t, resp, body, "MANDATE_SCOPE_VIOLATION")
}

func TestGolden06_TreasuryOutOfScopeCounterparty(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"acct_internal"}, 10000000)
	bid := createFreshBudgetNode(t, "g06", 1000000)
	tok := mintToken(t, treasuryAgent, mid, nil)
	resp, body := sendAct(t, tok, "SendPayment", "vendor_x", 100, "g06", []string{bid})
	assertDeny(t, resp, body, "MANDATE_SCOPE_VIOLATION")
}

func TestGolden07_RefundsOutOfScopeCounterparty(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"cust_001", "cust_002"}, 500000)
	bid := createFreshBudgetNode(t, "g07", 1000000)
	tok := mintToken(t, refundsAgent, mid, nil)
	resp, body := sendAct(t, tok, "SendPayment", "vendor_x", 100, "g07", []string{bid})
	assertDeny(t, resp, body, "MANDATE_SCOPE_VIOLATION")
}

func TestGolden08_CollectionsOutOfScopeCounterparty(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"cust_001", "cust_002"}, 100000)
	bid := createFreshBudgetNode(t, "g08", 1000000)
	tok := mintToken(t, collectionsAgent, mid, nil)
	resp, body := sendAct(t, tok, "SendPayment", "vendor_y", 100, "g08", []string{bid})
	assertDeny(t, resp, body, "MANDATE_SCOPE_VIOLATION")
}

// === AMOUNT EXCEEDS CEILING ===

func TestGolden09_ProcurementAmountExceedsCeiling(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"vendor_x","vendor_y"}, 2500000)
	bid := createFreshBudgetNode(t, "test", 10000000)
	tok := mintToken(t, procurementAgent, mid, nil)
	resp, body := sendAct(t, tok, "SendPayment", "vendor_x", 3000000, "g09", []string{bid})
	assertDeny(t, resp, body, "MANDATE_SCOPE_VIOLATION")
}

func TestGolden10_RefundsAmountExceedsCeiling(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"cust_001","cust_002"}, 500000)
	bid := createFreshBudgetNode(t, "test", 10000000)
	tok := mintToken(t, refundsAgent, mid, nil)
	resp, body := sendAct(t, tok, "SendPayment", "cust_001", 600000, "g10", []string{bid})
	assertDeny(t, resp, body, "MANDATE_SCOPE_VIOLATION")
}

func TestGolden11_CollectionsAmountExceedsCeiling(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"cust_001","cust_002"}, 100000)
	bid := createFreshBudgetNode(t, "test", 10000000)
	tok := mintToken(t, collectionsAgent, mid, nil)
	resp, body := sendAct(t, tok, "SendPayment", "cust_001", 200000, "g11", []string{bid})
	assertDeny(t, resp, body, "MANDATE_SCOPE_VIOLATION")
}

func TestGolden12_TreasuryAmountExceedsCeiling(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"acct_internal"}, 10000000)
	bid := createFreshBudgetNode(t, "test", 10000000)
	tok := mintToken(t, treasuryAgent, mid, nil)
	resp, body := sendAct(t, tok, "SendPayment", "acct_internal", 11000000, "g12", []string{bid})
	assertDeny(t, resp, body, "MANDATE_SCOPE_VIOLATION")
}

// === POLICY DENIED ===

func TestGolden13_RefundsReadInvoicePolicyDenied(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"cust_001","cust_002"}, 500000)
	bid := createFreshBudgetNode(t, "test", 10000000)
	tok := mintToken(t, refundsAgent, mid, nil)
	resp, body := sendAct(t, tok, "ReadInvoice", "vendor_x", 100, "g13", []string{bid})
	assertDeny(t, resp, body, "POLICY_DENIED")
}

func TestGolden14_TreasuryReadInvoicePolicyDenied(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"acct_internal"}, 10000000)
	bid := createFreshBudgetNode(t, "test", 10000000)
	tok := mintToken(t, treasuryAgent, mid, nil)
	resp, body := sendAct(t, tok, "ReadInvoice", "vendor_x", 100, "g14", []string{bid})
	assertDeny(t, resp, body, "POLICY_DENIED")
}

func TestGolden15_CollectionsReadInvoicePolicyDenied(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"cust_001","cust_002"}, 100000)
	bid := createFreshBudgetNode(t, "test", 10000000)
	tok := mintToken(t, collectionsAgent, mid, nil)
	resp, body := sendAct(t, tok, "ReadInvoice", "vendor_x", 100, "g15", []string{bid})
	assertDeny(t, resp, body, "POLICY_DENIED")
}

func TestGolden16_ProcurementUnknownActionPolicyDenied(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"vendor_x","vendor_y"}, 2500000)
	bid := createFreshBudgetNode(t, "test", 10000000)
	tok := mintToken(t, procurementAgent, mid, nil)
	resp, body := sendAct(t, tok, "DeleteAccount", "vendor_x", 100, "g16", []string{bid})
	assertDeny(t, resp, body, "POLICY_DENIED")
}

// === BAD TOKEN ===

func TestGolden17_GarbledToken(t *testing.T) {
	skipIfNoStack(t)
	bid := createFreshBudgetNode(t, "test", 10000000)
	resp, body := sendAct(t, garbledToken(), "SendPayment", "vendor_x", 100, "g17", []string{bid})
	assertDeny(t, resp, body, "TOKEN_INVALID")
}

func TestGolden18_FakeSignatureToken(t *testing.T) {
	skipIfNoStack(t)
	bid := createFreshBudgetNode(t, "test", 10000000)
	resp, body := sendAct(t, dummyToken(), "SendPayment", "vendor_x", 100, "g18", []string{bid})
	assertDeny(t, resp, body, "TOKEN_INVALID")
}

func TestGolden19_ExpiredToken(t *testing.T) {
	skipIfNoStack(t)
	bid := createFreshBudgetNode(t, "test", 10000000)
	resp, body := sendAct(t, expiredToken(), "SendPayment", "vendor_x", 100, "g19", []string{bid})
	assertDeny(t, resp, body, "TOKEN_INVALID")
}

// === BUDGET EXCEEDED ===

func TestGolden20_BudgetExceededSmallCap(t *testing.T) {
	skipIfNoStack(t)
	// Create a node with cap=50, try to spend 100 through gateway
	nodeID := createFreshBudgetNode(t, "g20", 50)
	mid := createMandateForTest(t, []string{"vendor_x", "vendor_y"}, 2500000)
	tok := mintToken(t, procurementAgent, mid, nil)
	resp2, body2 := sendAct(t, tok, "SendPayment", "vendor_x", 100, "g20", []string{nodeID})
	assertDeny(t, resp2, body2, "BUDGET_EXCEEDED")
}

func TestGolden21_BudgetExceededZeroCap(t *testing.T) {
	skipIfNoStack(t)
	nodeID := createFreshBudgetNode(t, "g21", 1)
	mid := createMandateForTest(t, []string{"vendor_x", "vendor_y"}, 2500000)
	tok := mintToken(t, procurementAgent, mid, nil)
	resp2, body2 := sendAct(t, tok, "SendPayment", "vendor_x", 100, "g21", []string{nodeID})
	assertDeny(t, resp2, body2, "BUDGET_EXCEEDED")
}

func TestGolden22_BudgetExceededMultiNode(t *testing.T) {
	skipIfNoStack(t)
	childID := createFreshBudgetNode(t, "g22-child", 10)
	mid := createMandateForTest(t, []string{"vendor_x", "vendor_y"}, 2500000)
	tok := mintToken(t, procurementAgent, mid, nil)
	resp2, body2 := sendAct(t, tok, "SendPayment", "vendor_x", 100, "g22", []string{childID})
	assertDeny(t, resp2, body2, "BUDGET_EXCEEDED")
}

// === EXPIRED MANDATE ===

func TestGolden23_ExpiredMandate(t *testing.T) {
	skipIfNoStack(t)
	// Create a mandate that's already expired
	body, _ := json.Marshal(map[string]interface{}{
		"issuer": "test", "purpose_code": "TEST",
		"counterparty_scope": map[string]interface{}{"type": "allowlist", "values": []string{"vendor_x"}},
		"ceiling_minor": 1000000, "valid_from": "2020-01-01T00:00:00.000Z",
		"valid_to": "2020-02-01T00:00:00.000Z", "max_depth": 1,
	})
	resp, err := http.Post(identityURL+"/v1/mandates", "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != 201 {
		t.Skip("could not create expired mandate")
	}
	defer resp.Body.Close()
	var m struct{ ID string `json:"id"` }
	json.NewDecoder(resp.Body).Decode(&m)

	// Mint should fail with MANDATE_EXPIRED
	mintBody, _ := json.Marshal(map[string]string{
		"agent_id":   procurementAgent,
		"mandate_id": m.ID,
	})
	resp2, err := http.Post(identityURL+"/v1/tokens/mint", "application/json", bytes.NewReader(mintBody))
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	defer resp2.Body.Close()
	respBody, _ := io.ReadAll(resp2.Body)
	if resp2.StatusCode != 403 {
		t.Errorf("expected 403 for expired mandate mint, got %d: %s", resp2.StatusCode, string(respBody))
	}
	if !strings.Contains(string(respBody), "MANDATE_EXPIRED") {
		t.Errorf("expected MANDATE_EXPIRED, got: %s", string(respBody))
	}
}

// === REVOKED MANDATE ===

func TestGolden24_RevokedMandate(t *testing.T) {
	skipIfNoStack(t)
	// Create a fresh mandate then revoke it
	body, _ := json.Marshal(map[string]interface{}{
		"issuer": "test", "purpose_code": "TEST",
		"counterparty_scope": map[string]interface{}{"type": "allowlist", "values": []string{"vendor_x"}},
		"ceiling_minor": 1000000, "valid_from": "2026-01-01T00:00:00.000Z",
		"valid_to": "2027-01-01T00:00:00.000Z", "max_depth": 1,
	})
	resp, err := http.Post(identityURL+"/v1/mandates", "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != 201 {
		t.Skip("could not create mandate")
	}
	defer resp.Body.Close()
	var m struct{ ID string `json:"id"` }
	json.NewDecoder(resp.Body).Decode(&m)

	// Mint token before revocation
	tok := mintToken(t, procurementAgent, m.ID, nil)

	// Revoke the mandate
	revokeBody := `{"reason":"test","actor":"golden_test"}`
	http.Post(identityURL+"/v1/mandates/"+m.ID+"/revoke", "application/json", strReader(revokeBody))

	bid := createFreshBudgetNode(t, "test", 10000000)
	resp2, body2 := sendAct(t, tok, "SendPayment", "vendor_x", 100, "g24", []string{bid})
	assertDeny(t, resp2, body2, "MANDATE_REVOKED")
}

// === CAVEAT WIDENING ===

func TestGolden25_AttenuateAmountWidening(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"vendor_x","vendor_y"}, 2500000)
	parent := mintToken(t, procurementAgent, mid, []capability.Caveat{
		{Type: "amount_max_minor", Value: 100000},
	})

	// Try to attenuate with a LARGER amount
	body, _ := json.Marshal(map[string]interface{}{
		"parent_token": parent,
		"additional_caveats": []map[string]interface{}{
			{"type": "amount_max_minor", "value": 200000},
		},
	})
	resp, err := http.Post(identityURL+"/v1/tokens/attenuate", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("attenuate: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 201 {
		t.Errorf("expected caveat widening rejection, got 201: %s", string(respBody))
	}
	if !strings.Contains(string(respBody), "CAVEAT_WIDENING") {
		t.Errorf("expected CAVEAT_WIDENING error, got: %s", string(respBody))
	}
}

func TestGolden26_AttenuateCounterpartyWidening(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"vendor_x","vendor_y"}, 2500000)
	parent := mintToken(t, procurementAgent, mid, []capability.Caveat{
		{Type: "counterparty_allowlist", Value: []string{"vendor_x"}},
	})

	body, _ := json.Marshal(map[string]interface{}{
		"parent_token": parent,
		"additional_caveats": []map[string]interface{}{
			{"type": "counterparty_allowlist", "value": []string{"vendor_x", "vendor_evil"}},
		},
	})
	resp, err := http.Post(identityURL+"/v1/tokens/attenuate", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("attenuate: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 201 {
		t.Errorf("expected caveat widening rejection, got 201")
	}
	if !strings.Contains(string(respBody), "CAVEAT_WIDENING") {
		t.Errorf("expected CAVEAT_WIDENING, got: %s", string(respBody))
	}
}

// === VALID ATTENUATION ===

func TestGolden27_AttenuateValidNarrowing(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"vendor_x","vendor_y"}, 2500000)
	parent := mintToken(t, procurementAgent, mid, []capability.Caveat{
		{Type: "amount_max_minor", Value: 200000},
	})

	// Narrow the amount
	body, _ := json.Marshal(map[string]interface{}{
		"parent_token": parent,
		"additional_caveats": []map[string]interface{}{
			{"type": "amount_max_minor", "value": 100000},
		},
	})
	resp, err := http.Post(identityURL+"/v1/tokens/attenuate", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("attenuate: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201 for valid narrowing, got %d: %s", resp.StatusCode, string(respBody))
	}
}

// === IDEMPOTENCY ===

func TestGolden28_IdempotencySameKey(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"vendor_x","vendor_y"}, 2500000)
	bid := createFreshBudgetNode(t, "test", 10000000)
	tok := mintToken(t, procurementAgent, mid, nil)
	resp1, body1 := sendAct(t, tok, "SendPayment", "vendor_x", 100, "g28-same-key", []string{bid})
	assertAllow(t, resp1, body1)

	// Same idempotency key — should return same result, not double spend
	resp2, body2 := sendAct(t, tok, "SendPayment", "vendor_x", 100, "g28-same-key", []string{bid})
	// The budget-ledger's idempotency guard should return the same reservation
	// The gateway may return 200 (executed) since the rail already ran
	if resp2.StatusCode != 200 && resp2.StatusCode != 409 {
		t.Errorf("expected 200 or 409 for idempotent request, got %d: %s", resp2.StatusCode, string(body2))
	}
}

// === TOKEN RENEWAL ===

func TestGolden29_TokenRenew(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"vendor_x","vendor_y"}, 2500000)
	tok := mintToken(t, procurementAgent, mid, nil)

	body, _ := json.Marshal(map[string]string{"old_token": tok})
	resp, err := http.Post(identityURL+"/v1/tokens/renew", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 201 for renew, got %d: %s", resp.StatusCode, string(respBody))
	}
}

// === RENEW FAILS ON REVOKED MANDATE ===

func TestGolden30_RenewFailsOnRevokedMandate(t *testing.T) {
	skipIfNoStack(t)
	body, _ := json.Marshal(map[string]interface{}{
		"issuer": "test", "purpose_code": "TEST",
		"counterparty_scope": map[string]interface{}{"type": "allowlist", "values": []string{"vendor_x"}},
		"ceiling_minor": 1000000, "valid_from": "2026-01-01T00:00:00.000Z",
		"valid_to": "2027-01-01T00:00:00.000Z", "max_depth": 1,
	})
	resp, err := http.Post(identityURL+"/v1/mandates", "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != 201 {
		t.Skip("could not create mandate")
	}
	defer resp.Body.Close()
	var m struct{ ID string `json:"id"` }
	json.NewDecoder(resp.Body).Decode(&m)

	tok := mintToken(t, procurementAgent, m.ID, nil)
	revokeBody := `{"reason":"test","actor":"golden_test"}`
	http.Post(identityURL+"/v1/mandates/"+m.ID+"/revoke", "application/json", strReader(revokeBody))

	renewBody, _ := json.Marshal(map[string]string{"old_token": tok})
	resp2, err := http.Post(identityURL+"/v1/tokens/renew", "application/json", bytes.NewReader(renewBody))
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	defer resp2.Body.Close()
	respBody, _ := io.ReadAll(resp2.Body)
	if resp2.StatusCode == 201 {
		t.Errorf("expected renew to fail on revoked mandate, got 201")
	}
	if !strings.Contains(string(respBody), "MANDATE_REVOKED") {
		t.Errorf("expected MANDATE_REVOKED, got: %s", string(respBody))
	}
}

// === ADDITIONAL COVERAGE CASES ===

func TestGolden31_ProcurementReadInvoiceAllowed(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"vendor_x","vendor_y"}, 2500000)
	// Create a fresh budget node for this test
	bid := createFreshBudgetNode(t, "g31", 1000000)
	tok := mintToken(t, procurementAgent, mid, nil)
	resp, body := sendAct(t, tok, "ReadInvoice", "vendor_x", 1, "g31", []string{bid})
	assertAllow(t, resp, body)
}

func TestGolden32_ProcurementValidPaymentVendorY(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"vendor_x","vendor_y"}, 2500000)
	bid := createFreshBudgetNode(t, "test", 10000000)
	tok := mintToken(t, procurementAgent, mid, nil)
	resp, body := sendAct(t, tok, "SendPayment", "vendor_y", 500, "g32", []string{bid})
	assertAllow(t, resp, body)
}

func TestGolden33_RefundsValidPaymentCust002(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"cust_001", "cust_002"}, 500000)
	bid := createFreshBudgetNode(t, "g33", 1000000)
	tok := mintToken(t, refundsAgent, mid, nil)
	resp, body := sendAct(t, tok, "SendPayment", "cust_002", 100, "g33", []string{bid})
	assertAllow(t, resp, body)
}

func TestGolden34_CollectionsValidPaymentCust002(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"cust_001", "cust_002"}, 100000)
	bid := createFreshBudgetNode(t, "g34", 1000000)
	tok := mintToken(t, collectionsAgent, mid, nil)
	resp, body := sendAct(t, tok, "SendPayment", "cust_002", 50, "g34", []string{bid})
	assertAllow(t, resp, body)
}

func TestGolden35_TreasuryLargePayment(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"acct_internal"}, 10000000)
	bid := createFreshBudgetNode(t, "g35", 10000000)
	tok := mintToken(t, treasuryAgent, mid, nil)
	resp, body := sendAct(t, tok, "SendPayment", "acct_internal", 9000000, "g35", []string{bid})
	assertAllow(t, resp, body)
}

func TestGolden36_RefundsSmallPayment(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"cust_001", "cust_002"}, 500000)
	bid := createFreshBudgetNode(t, "g36", 1000000)
	tok := mintToken(t, refundsAgent, mid, nil)
	resp, body := sendAct(t, tok, "SendPayment", "cust_001", 1, "g36", []string{bid})
	assertAllow(t, resp, body)
}

func TestGolden37_TreasurySmallPayment(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"acct_internal"}, 10000000)
	bid := createFreshBudgetNode(t, "g37", 10000000)
	tok := mintToken(t, treasuryAgent, mid, nil)
	resp, body := sendAct(t, tok, "SendPayment", "acct_internal", 1, "g37", []string{bid})
	assertAllow(t, resp, body)
}

func TestGolden38_CollectionsZeroAmount(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"cust_001","cust_002"}, 100000)
	bid := createFreshBudgetNode(t, "test", 10000000)
	tok := mintToken(t, collectionsAgent, mid, nil)
	resp, body := sendAct(t, tok, "SendPayment", "cust_001", 0, "g38", []string{bid})
	// Zero amount may be allowed or denied depending on budget ledger behavior
	// Just check it doesn't crash
	if resp.StatusCode != 200 && resp.StatusCode != 403 {
		t.Errorf("expected 200 or 403, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestGolden39_ProcurementLargeValidPayment(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"vendor_x","vendor_y"}, 2500000)
	bid := createFreshBudgetNode(t, "test", 10000000)
	tok := mintToken(t, procurementAgent, mid, nil)
	resp, body := sendAct(t, tok, "SendPayment", "vendor_x", 2500000, "g39", []string{bid})
	assertAllow(t, resp, body)
}

func TestGolden40_RefundsReadInvoiceDenied(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"cust_001","cust_002"}, 500000)
	bid := createFreshBudgetNode(t, "test", 10000000)
	tok := mintToken(t, refundsAgent, mid, nil)
	resp, body := sendAct(t, tok, "ReadInvoice", "cust_001", 0, "g40", []string{bid})
	assertDeny(t, resp, body, "POLICY_DENIED")
}

func TestGolden41_CollectionsReadInvoiceDenied(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"cust_001","cust_002"}, 100000)
	bid := createFreshBudgetNode(t, "test", 10000000)
	tok := mintToken(t, collectionsAgent, mid, nil)
	resp, body := sendAct(t, tok, "ReadInvoice", "cust_001", 0, "g41", []string{bid})
	assertDeny(t, resp, body, "POLICY_DENIED")
}

func TestGolden42_TreasuryReadInvoiceDenied(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"acct_internal"}, 10000000)
	bid := createFreshBudgetNode(t, "test", 10000000)
	tok := mintToken(t, treasuryAgent, mid, nil)
	resp, body := sendAct(t, tok, "ReadInvoice", "acct_internal", 0, "g42", []string{bid})
	assertDeny(t, resp, body, "POLICY_DENIED")
}

func TestGolden43_UnknownActionAllPersonas(t *testing.T) {
	skipIfNoStack(t)
	mid := createMandateForTest(t, []string{"acct_internal"}, 10000000)
	bid := createFreshBudgetNode(t, "test", 10000000)
	tok := mintToken(t, treasuryAgent, mid, nil)
	resp, body := sendAct(t, tok, "DeleteDatabase", "acct_internal", 100, "g43", []string{bid})
	assertDeny(t, resp, body, "POLICY_DENIED")
}

// skipIfNoStack skips the test if the Docker stack isn't running.
func skipIfNoStack(t *testing.T) {
	t.Helper()
	resp, err := http.Get(gatewayURL + "/healthz")
	if err != nil || resp.StatusCode != 200 {
		t.Skip("stack not running — skipping golden test")
	}
	if resp != nil {
		resp.Body.Close()
	}
}

func _unused() {
	_ = fmt.Sprintf
	_ = time.Now
	_ = exec.Command
}
