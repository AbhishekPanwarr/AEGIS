// Package capability implements capability token structures and
// Ed25519 sign/verify logic per Section 5.3 & 6.1 of the implementation plan.
//
// Only Go services sign or verify anything (identity-service mints and signs,
// gateway verifies) so there is no cross-language canonicalization risk.
// The signing rule: define the signed fields once as a Go struct with a
// fixed, documented field order. The signature is computed over
// json.Marshal of that struct. Go's standard struct marshalling always
// serializes struct fields in their declared order.
package capability

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// TokenSignedFields is the set of fields that are Ed25519-signed.
// Field order MUST match Section 5.3 exactly — json.Marshal uses struct
// declaration order, and changing the order changes every signature.
type TokenSignedFields struct {
	TokenID      string    `json:"token_id"`
	AgentID      string    `json:"agent_id"`
	MandateID    string    `json:"mandate_id"`
	Caveats      []Caveat  `json:"caveats"`
	Chain        []SigBlock `json:"chain"`
	EpochFleet   int64     `json:"epoch_fleet"`
	EpochGroup   int64     `json:"epoch_group"`
	EpochAgent   int64     `json:"epoch_agent"`
	EpochMandate int64     `json:"epoch_mandate"`
	IAT          int64     `json:"iat"`
	EXP          int64     `json:"exp"`
}

// Token is the full on-wire token including the signature.
type Token struct {
	TokenSignedFields
	Sig string `json:"sig"`
}

// MandateSignedFields is the set of fields signed for a mandate.
// Field order matches Section 5.4.
type MandateSignedFields struct {
	ID                string          `json:"id"`
	Issuer            string          `json:"issuer"`
	PurposeCode       string          `json:"purpose_code"`
	CounterpartyScope json.RawMessage `json:"counterparty_scope"`
	CeilingMinor      int64           `json:"ceiling_minor"`
	Currency          string          `json:"currency"`
	ValidFrom         string          `json:"valid_from"`
	ValidTo           string          `json:"valid_to"`
	MaxDepth          int             `json:"max_depth"`
	Recurrence        json.RawMessage `json:"recurrence"`
	Nonce             string          `json:"nonce"`
}

// Caveat is a restriction on a token.
type Caveat struct {
	Type  string      `json:"type"`
	Value interface{} `json:"value"`
}

// SigBlock is a parent token's signature block in the chain.
type SigBlock struct {
	TokenID string `json:"token_id"`
	Sig     string `json:"sig"`
}

// Common caveat types from Section 5.3.
const (
	CaveatAmountMaxMinor       = "amount_max_minor"
	CaveatCounterpartyAllowlist = "counterparty_allowlist"
	CaveatRequiresApproval     = "requires_approval"
)

// CanonicalBytes returns the canonical JSON bytes of TokenSignedFields.
// This is the byte sequence that gets Ed25519-signed.
func CanonicalBytes(fields TokenSignedFields) ([]byte, error) {
	return json.Marshal(fields)
}

// Sign signs TokenSignedFields with the given Ed25519 private key
// and returns the base64-encoded signature.
func Sign(fields TokenSignedFields, privateKey ed25519.PrivateKey) (string, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("invalid private key size: %d", len(privateKey))
	}
	canonical, err := CanonicalBytes(fields)
	if err != nil {
		return "", fmt.Errorf("marshal token fields: %w", err)
	}
	sig := ed25519.Sign(privateKey, canonical)
	return base64.StdEncoding.EncodeToString(sig), nil
}

// VerifyAndParse parses a token JSON string, verifies the Ed25519 signature
// against the given public key, and checks that the token has not expired.
// Returns the parsed Token on success.
func VerifyAndParse(tokenJSON string, publicKey ed25519.PublicKey) (*Token, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key size: %d", len(publicKey))
	}

	var tok Token
	if err := json.Unmarshal([]byte(tokenJSON), &tok); err != nil {
		return nil, fmt.Errorf("unmarshal token: %w", err)
	}

	if tok.Sig == "" {
		return nil, errors.New("token has no signature")
	}

	// Reconstruct the signed fields from the parsed token (everything except Sig).
	canonical, err := CanonicalBytes(tok.TokenSignedFields)
	if err != nil {
		return nil, fmt.Errorf("marshal token fields for verification: %w", err)
	}

	sigBytes, err := base64.StdEncoding.DecodeString(tok.Sig)
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}

	if !ed25519.Verify(publicKey, canonical, sigBytes) {
		return nil, errors.New("signature verification failed")
	}

	// Check expiry.
	if time.Now().Unix() >= tok.EXP {
		return nil, errors.New("token has expired")
	}

	return &tok, nil
}

// MandateCanonicalBytes returns the canonical JSON bytes of MandateSignedFields.
func MandateCanonicalBytes(fields MandateSignedFields) ([]byte, error) {
	return json.Marshal(fields)
}

// SignMandate signs MandateSignedFields with the given Ed25519 private key
// and returns the base64-encoded signature.
func SignMandate(fields MandateSignedFields, privateKey ed25519.PrivateKey) (string, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("invalid private key size: %d", len(privateKey))
	}
	canonical, err := MandateCanonicalBytes(fields)
	if err != nil {
		return "", fmt.Errorf("marshal mandate fields: %w", err)
	}
	sig := ed25519.Sign(privateKey, canonical)
	return base64.StdEncoding.EncodeToString(sig), nil
}

// VerifyMandate verifies a mandate signature against the given public key.
func VerifyMandate(fields MandateSignedFields, sig string, publicKey ed25519.PublicKey) error {
	if len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key size: %d", len(publicKey))
	}

	canonical, err := MandateCanonicalBytes(fields)
	if err != nil {
		return fmt.Errorf("marshal mandate fields for verification: %w", err)
	}

	sigBytes, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}

	if !ed25519.Verify(publicKey, canonical, sigBytes) {
		return errors.New("mandate signature verification failed")
	}

	return nil
}

// HasCaveat returns true if the token has a caveat of the given type.
func (t *Token) HasCaveat(caveatType string) bool {
	for _, c := range t.Caveats {
		if c.Type == caveatType {
			return true
		}
	}
	return false
}

// GetCaveat returns the caveat of the given type, or nil if not found.
func (t *Token) GetCaveat(caveatType string) *Caveat {
	for i := range t.Caveats {
		if t.Caveats[i].Type == caveatType {
			return &t.Caveats[i]
		}
	}
	return nil
}
