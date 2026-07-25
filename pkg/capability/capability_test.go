package capability

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func mustGenerateKey(t *testing.T) (ed25519.PrivateKey, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return priv, pub
}

func sampleTokenFields() TokenSignedFields {
	return TokenSignedFields{
		TokenID:   "550e8400-e29b-41d4-a716-446655440000",
		AgentID:   "550e8400-e29b-41d4-a716-446655440001",
		MandateID: "550e8400-e29b-41d4-a716-446655440002",
		Caveats: []Caveat{
			{Type: "amount_max_minor", Value: 200000},
			{Type: "counterparty_allowlist", Value: []string{"vendor_x"}},
		},
		Chain:        nil,
		EpochFleet:   0,
		EpochGroup:   0,
		EpochAgent:   0,
		EpochMandate: 0,
		IAT:          time.Now().Unix(),
		EXP:          time.Now().Unix() + 45,
	}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	priv, pub := mustGenerateKey(t)
	fields := sampleTokenFields()

	sig, err := Sign(fields, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// Build full token JSON.
	token := Token{TokenSignedFields: fields, Sig: sig}
	tokenJSON, err := json.Marshal(token)
	if err != nil {
		t.Fatalf("Marshal token: %v", err)
	}

	parsed, err := VerifyAndParse(string(tokenJSON), pub)
	if err != nil {
		t.Fatalf("VerifyAndParse: %v", err)
	}

	if parsed.TokenID != fields.TokenID {
		t.Errorf("TokenID mismatch: got %s, want %s", parsed.TokenID, fields.TokenID)
	}
	if parsed.AgentID != fields.AgentID {
		t.Errorf("AgentID mismatch: got %s, want %s", parsed.AgentID, fields.AgentID)
	}
}

func TestVerifyFailsOnAlteredSignature(t *testing.T) {
	priv, pub := mustGenerateKey(t)
	fields := sampleTokenFields()

	sig, err := Sign(fields, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	sigBytes, _ := base64.StdEncoding.DecodeString(sig)
	sigBytes[0] ^= 0x01
	alteredSig := base64.StdEncoding.EncodeToString(sigBytes)

	token := Token{TokenSignedFields: fields, Sig: alteredSig}
	tokenJSON, _ := json.Marshal(token)

	_, err = VerifyAndParse(string(tokenJSON), pub)
	if err == nil {
		t.Fatal("VerifyAndParse should fail with altered signature")
	}
	if !strings.Contains(err.Error(), "signature verification failed") {
		t.Errorf("expected signature verification error, got: %v", err)
	}
}

func TestVerifyFailsOnAlteredField(t *testing.T) {
	priv, pub := mustGenerateKey(t)
	fields := sampleTokenFields()

	sig, err := Sign(fields, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	token := Token{TokenSignedFields: fields, Sig: sig}
	tokenJSON, _ := json.Marshal(token)

	// Alter a field in the JSON.
	alteredJSON := strings.Replace(string(tokenJSON), fields.TokenID, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", 1)

	_, err = VerifyAndParse(alteredJSON, pub)
	if err == nil {
		t.Fatal("VerifyAndParse should fail with altered field")
	}
}

func TestVerifyFailsOnExpiredToken(t *testing.T) {
	priv, pub := mustGenerateKey(t)
	fields := sampleTokenFields()
	fields.IAT = time.Now().Unix() - 100
	fields.EXP = time.Now().Unix() - 1

	sig, err := Sign(fields, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	token := Token{TokenSignedFields: fields, Sig: sig}
	tokenJSON, _ := json.Marshal(token)

	_, err = VerifyAndParse(string(tokenJSON), pub)
	if err == nil {
		t.Fatal("VerifyAndParse should fail for expired token")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("expected expiry error, got: %v", err)
	}
}

func TestVerifyFailsWithWrongKey(t *testing.T) {
	priv1, _ := mustGenerateKey(t)
	_, pub2 := mustGenerateKey(t)

	fields := sampleTokenFields()
	sig, err := Sign(fields, priv1)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	token := Token{TokenSignedFields: fields, Sig: sig}
	tokenJSON, _ := json.Marshal(token)

	_, err = VerifyAndParse(string(tokenJSON), pub2)
	if err == nil {
		t.Fatal("VerifyAndParse should fail with wrong public key")
	}
}

func TestSignatureCoversExactStruct(t *testing.T) {
	priv, pub := mustGenerateKey(t)
	fields := sampleTokenFields()

	sig, err := Sign(fields, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	token := Token{TokenSignedFields: fields, Sig: sig}
	tokenJSON, _ := json.Marshal(token)

	// Verify that adding an extra (unsigned) field to the JSON does NOT break
	// verification — the signature covers exactly the declared struct fields,
	// and json.Unmarshal ignores unknown fields. This is correct: the security
	// property is that tampering with any signed field breaks verification,
	// not that the JSON can't carry extra metadata.
	var rawMap map[string]interface{}
	_ = json.Unmarshal(tokenJSON, &rawMap)
	rawMap["extra_field"] = "ignored"
	extraJSON, _ := json.Marshal(rawMap)

	parsed, err := VerifyAndParse(string(extraJSON), pub)
	if err != nil {
		t.Fatalf("VerifyAndParse should succeed when extra unsigned field is added: %v", err)
	}
	if parsed.TokenID != fields.TokenID {
		t.Error("TokenID should match despite extra field")
	}

	// But changing any signed field MUST break verification.
	alteredJSON := strings.Replace(string(tokenJSON), `"amount_max_minor"`, `"tampered"`, 1)
	_, err = VerifyAndParse(alteredJSON, pub)
	if err == nil {
		t.Fatal("VerifyAndParse should fail when a signed field is corrupted")
	}
}

func TestMandateSignVerifyRoundTrip(t *testing.T) {
	priv, pub := mustGenerateKey(t)

	scope, _ := json.Marshal(map[string]interface{}{
		"type":   "allowlist",
		"values": []string{"vendor_x", "vendor_y"},
	})

	fields := MandateSignedFields{
		ID:                "550e8400-e29b-41d4-a716-446655440003",
		Issuer:            "priya.n@bank",
		PurposeCode:       "VENDOR_PAYMENT",
		CounterpartyScope: scope,
		CeilingMinor:      2500000,
		Currency:          "USD",
		ValidFrom:         "2026-07-25T09:00:00.000Z",
		ValidTo:           "2026-08-01T09:00:00.000Z",
		MaxDepth:          2,
		Recurrence:        nil,
		Nonce:             "7f3a91c4e0",
	}

	sig, err := SignMandate(fields, priv)
	if err != nil {
		t.Fatalf("SignMandate: %v", err)
	}

	err = VerifyMandate(fields, sig, pub)
	if err != nil {
		t.Fatalf("VerifyMandate: %v", err)
	}
}

func TestMandateVerifyFailsWithAlteredScope(t *testing.T) {
	priv, pub := mustGenerateKey(t)

	scope, _ := json.Marshal(map[string]interface{}{
		"type":   "allowlist",
		"values": []string{"vendor_x"},
	})

	fields := MandateSignedFields{
		ID:                "test-mandate-id",
		Issuer:            "test@bank",
		PurposeCode:       "VENDOR_PAYMENT",
		CounterpartyScope: scope,
		CeilingMinor:      2500000,
		Currency:          "USD",
		ValidFrom:         "2026-07-25T09:00:00.000Z",
		ValidTo:           "2026-08-01T09:00:00.000Z",
		MaxDepth:          2,
		Recurrence:        nil,
		Nonce:             "abc123",
	}

	sig, err := SignMandate(fields, priv)
	if err != nil {
		t.Fatalf("SignMandate: %v", err)
	}

	// Alter the scope.
	alteredScope, _ := json.Marshal(map[string]interface{}{
		"type":   "allowlist",
		"values": []string{"vendor_x", "vendor_evil"},
	})
	fields.CounterpartyScope = alteredScope

	err = VerifyMandate(fields, sig, pub)
	if err == nil {
		t.Fatal("VerifyMandate should fail with altered scope")
	}
}

func TestHasCaveat(t *testing.T) {
	tok := &Token{
		TokenSignedFields: TokenSignedFields{
			Caveats: []Caveat{
				{Type: "amount_max_minor", Value: 100},
				{Type: "requires_approval", Value: true},
			},
		},
	}

	if !tok.HasCaveat("amount_max_minor") {
		t.Error("HasCaveat should return true for amount_max_minor")
	}
	if !tok.HasCaveat("requires_approval") {
		t.Error("HasCaveat should return true for requires_approval")
	}
	if tok.HasCaveat("counterparty_allowlist") {
		t.Error("HasCaveat should return false for counterparty_allowlist")
	}
}
