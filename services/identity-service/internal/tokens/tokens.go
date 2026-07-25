package tokens

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"

	"aegis/pkg/capability"
	"aegis/pkg/idgen"
	"aegis/services/identity-service/internal/redis"
	"aegis/services/identity-service/internal/store"

	goredis "github.com/redis/go-redis/v9"
)

type Service struct {
	signingKey    ed25519.PrivateKey
	publicKey     ed25519.PublicKey
	redis         *redis.Client
	store         *store.Store
	rdb           *goredis.Client
	containmentURL string
}

func New(signingKey ed25519.PrivateKey, publicKey ed25519.PublicKey, rdb *redis.Client, st *store.Store, directRedis *goredis.Client, containmentURL string) *Service {
	return &Service{
		signingKey:    signingKey,
		publicKey:     publicKey,
		redis:         rdb,
		store:         st,
		rdb:           directRedis,
		containmentURL: containmentURL,
	}
}

func (s *Service) PubkeyB64() string {
	return base64.StdEncoding.EncodeToString(s.publicKey)
}

func jitter(baseTTL int, jitterPct float64) int {
	if jitterPct <= 0 {
		return baseTTL
	}
	delta := float64(baseTTL) * jitterPct
	offset := (rand.Float64()*2 - 1) * delta
	return baseTTL + int(offset)
}

func (s *Service) Mint(ctx context.Context, agentID, mandateID string, caveats []capability.Caveat) (string, error) {
	mandate, err := s.store.GetMandate(ctx, mandateID)
	if err != nil {
		return "", fmt.Errorf("get mandate: %w", err)
	}

	if mandate.Status != "live" {
		return "", &MandateError{Code: "MANDATE_REVOKED", Message: "mandate is not live"}
	}
	if time.Now().After(mandate.ValidTo) {
		return "", &MandateError{Code: "MANDATE_EXPIRED", Message: "mandate has expired"}
	}

	agent, err := s.store.GetAgent(ctx, agentID)
	if err != nil {
		return "", fmt.Errorf("get agent: %w", err)
	}

	// Check containment level to determine lease length
	baseLease := 45
	if v := os.Getenv("BASE_LEASE_SECONDS"); v != "" {
		baseLease, _ = strconv.Atoi(v)
	}

	// Check if agent is under THROTTLE
	level := "OBSERVE"
	if s.containmentURL != "" {
		level = s.checkContainmentLevel(ctx, agentID, agent.GroupID, mandateID)
	}
	if level == "THROTTLE" || level == "QUARANTINE" || level == "HALT" {
		baseLease = 10
		if v := os.Getenv("THROTTLE_LEASE_SECONDS"); v != "" {
			baseLease, _ = strconv.Atoi(v)
		}
	}

	jitterPct := 0.20
	if v := os.Getenv("LEASE_JITTER_PCT"); v != "" {
		jitterPct, _ = strconv.ParseFloat(v, 64)
	}

	ttl := jitter(baseLease, jitterPct)

	// Check if agent is quarantined — add requires_approval caveat
	if s.rdb != nil {
		isQuarantined, _ := s.rdb.SIsMember(ctx, "quarantine_agents", agentID).Result()
		if isQuarantined {
			hasApprovalCaveat := false
			for _, c := range caveats {
				if c.Type == capability.CaveatRequiresApproval {
					hasApprovalCaveat = true
					break
				}
			}
			if !hasApprovalCaveat {
				caveats = append(caveats, capability.Caveat{
					Type: capability.CaveatRequiresApproval, Value: true,
				})
			}
		}
	}

	epochs, _ := s.redis.ReadEpochs(ctx, agentID, agent.GroupID, mandateID)
	if epochs == nil {
		epochs = &redis.EpochValues{}
	}

	now := time.Now().Unix()
	fields := capability.TokenSignedFields{
		TokenID:      idgen.New(),
		AgentID:      agentID,
		MandateID:    mandateID,
		Caveats:      caveats,
		Chain:        nil,
		EpochFleet:   epochs.Fleet,
		EpochGroup:   epochs.Group,
		EpochAgent:   epochs.Agent,
		EpochMandate: epochs.Mandate,
		IAT:          now,
		EXP:          now + int64(ttl),
	}

	sig, err := capability.Sign(fields, s.signingKey)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}

	token := capability.Token{TokenSignedFields: fields, Sig: sig}
	tokenJSON, err := json.Marshal(token)
	if err != nil {
		return "", fmt.Errorf("marshal token: %w", err)
	}

	return string(tokenJSON), nil
}

func (s *Service) Attenuate(ctx context.Context, parentTokenJSON string, additionalCaveats []capability.Caveat) (string, error) {
	parent, err := capability.VerifyAndParse(parentTokenJSON, s.publicKey)
	if err != nil {
		return "", fmt.Errorf("verify parent token: %w", err)
	}

	mergedCaveats := make([]capability.Caveat, len(parent.Caveats))
	copy(mergedCaveats, parent.Caveats)

	for _, newCaveat := range additionalCaveats {
		if err := validateCaveatNarrows(newCaveat, parent.Caveats); err != nil {
			return "", err
		}
		mergedCaveats = append(mergedCaveats, newCaveat)
	}

	// Read current epochs for the new token.
	agent, err := s.store.GetAgent(ctx, parent.AgentID)
	if err != nil {
		return "", fmt.Errorf("get agent: %w", err)
	}
	epochs, _ := s.redis.ReadEpochs(ctx, parent.AgentID, agent.GroupID, parent.MandateID)
	if epochs == nil {
		epochs = &redis.EpochValues{}
	}

	baseLease := 45
	if v := os.Getenv("BASE_LEASE_SECONDS"); v != "" {
		baseLease, _ = strconv.Atoi(v)
	}
	jitterPct := 0.20
	if v := os.Getenv("LEASE_JITTER_PCT"); v != "" {
		jitterPct, _ = strconv.ParseFloat(v, 64)
	}
	ttl := jitter(baseLease, jitterPct)

	now := time.Now().Unix()
	chain := make([]capability.SigBlock, len(parent.Chain))
	copy(chain, parent.Chain)
	chain = append(chain, capability.SigBlock{
		TokenID: parent.TokenID,
		Sig:     parent.Sig,
	})

	fields := capability.TokenSignedFields{
		TokenID:      idgen.New(),
		AgentID:      parent.AgentID,
		MandateID:    parent.MandateID,
		Caveats:      mergedCaveats,
		Chain:        chain,
		EpochFleet:   epochs.Fleet,
		EpochGroup:   epochs.Group,
		EpochAgent:   epochs.Agent,
		EpochMandate: epochs.Mandate,
		IAT:          now,
		EXP:          now + int64(ttl),
	}

	sig, err := capability.Sign(fields, s.signingKey)
	if err != nil {
		return "", fmt.Errorf("sign child token: %w", err)
	}

	token := capability.Token{TokenSignedFields: fields, Sig: sig}
	tokenJSON, err := json.Marshal(token)
	if err != nil {
		return "", fmt.Errorf("marshal child token: %w", err)
	}

	return string(tokenJSON), nil
}

func (s *Service) Renew(ctx context.Context, oldTokenJSON string) (string, error) {
	oldToken, err := capability.VerifyAndParse(oldTokenJSON, s.publicKey)
	if err != nil {
		return "", fmt.Errorf("verify old token: %w", err)
	}

	mandate, err := s.store.GetMandate(ctx, oldToken.MandateID)
	if err != nil {
		return "", fmt.Errorf("get mandate: %w", err)
	}

	if mandate.Status != "live" {
		return "", &MandateError{Code: "MANDATE_REVOKED", Message: "mandate has been revoked"}
	}
	if time.Now().After(mandate.ValidTo) {
		return "", &MandateError{Code: "MANDATE_EXPIRED", Message: "mandate has expired"}
	}

	return s.Mint(ctx, oldToken.AgentID, oldToken.MandateID, oldToken.Caveats)
}

type MandateError struct {
	Code    string
	Message string
}

func (e *MandateError) Error() string {
	return e.Message
}

func validateCaveatNarrows(newCaveat capability.Caveat, parentCaveats []capability.Caveat) error {
	switch newCaveat.Type {
	case capability.CaveatAmountMaxMinor:
		newMax, ok := toInt64(newCaveat.Value)
		if !ok {
			return &MandateError{Code: "CAVEAT_WIDENING", Message: "invalid amount_max_minor value"}
		}
		for _, pc := range parentCaveats {
			if pc.Type == capability.CaveatAmountMaxMinor {
				parentMax, _ := toInt64(pc.Value)
				if newMax > parentMax {
					return &MandateError{Code: "CAVEAT_WIDENING", Message: "amount_max_minor widens parent's limit"}
				}
			}
		}

	case capability.CaveatCounterpartyAllowlist:
		newList, ok := toStringSlice(newCaveat.Value)
		if !ok {
			return &MandateError{Code: "CAVEAT_WIDENING", Message: "invalid counterparty_allowlist value"}
		}
		for _, pc := range parentCaveats {
			if pc.Type == capability.CaveatCounterpartyAllowlist {
				parentList, _ := toStringSlice(pc.Value)
				parentSet := make(map[string]bool)
				for _, p := range parentList {
					parentSet[p] = true
				}
				for _, n := range newList {
					if !parentSet[n] {
						return &MandateError{Code: "CAVEAT_WIDENING", Message: fmt.Sprintf("counterparty %s not in parent's allowlist", n)}
					}
				}
			}
		}

	case capability.CaveatRequiresApproval:
		if v, ok := newCaveat.Value.(bool); ok && !v {
			return &MandateError{Code: "CAVEAT_WIDENING", Message: "cannot remove requires_approval"}
		}
	}

	return nil
}

func toInt64(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	}
	return 0, false
}

func toStringSlice(v interface{}) ([]string, bool) {
	switch s := v.(type) {
	case []interface{}:
		result := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				result = append(result, str)
			} else {
				return nil, false
			}
		}
		return result, true
	case []string:
		return s, true
	}
	return nil, false
}

func (s *Service) checkContainmentLevel(ctx context.Context, agentID, groupID, mandateID string) string {
	url := fmt.Sprintf("%s/v1/containment/level?agent_id=%s&group_id=%s&mandate_id=%s",
		s.containmentURL, agentID, groupID, mandateID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "OBSERVE"
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "OBSERVE"
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "OBSERVE"
	}

	var result struct {
		Level string `json:"level"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "OBSERVE"
	}

	return result.Level
}
