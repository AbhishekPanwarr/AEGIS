package chain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type CanonicalPayload struct {
	DecisionID        string          `json:"decision_id"`
	AgentID           *string         `json:"agent_id"`
	MandateID         *string         `json:"mandate_id"`
	TokenID           *string         `json:"token_id"`
	ActionType        string          `json:"action_type"`
	PolicyVersionHash string          `json:"policy_version_hash"`
	Decision          string          `json:"decision"`
	ReasonCode        string          `json:"reason_code"`
	Context           json.RawMessage `json:"context"`
}

func Hash(data string) string {
	h := sha256.New()
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func ComputeCommitment(payload CanonicalPayload) string {
	b, _ := json.Marshal(payload)
	return Hash(string(b))
}

func ComputeThisHash(prevHash, payloadCommitment, policyVersionHash string) string {
	return Hash(prevHash + payloadCommitment + policyVersionHash)
}
