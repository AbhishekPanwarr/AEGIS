package ladder

import (
	"context"
	"fmt"

	"aegis/pkg/idgen"
	"aegis/pkg/telemetry"
	"aegis/services/containment-controller/internal/epoch"
	"aegis/services/containment-controller/internal/store"

	"github.com/redis/go-redis/v9"
)

var levelOrder = map[string]int{
	"OBSERVE": 0, "THROTTLE": 1, "QUARANTINE": 2, "HALT": 3,
}

func MostRestrictive(levels ...string) string {
	maxLevel := "OBSERVE"
	maxVal := 0
	for _, l := range levels {
		if v, ok := levelOrder[l]; ok && v > maxVal {
			maxVal = v
			maxLevel = l
		}
	}
	return maxLevel
}

type Service struct {
	store  *store.Store
	epoch  *epoch.Manager
	rdb    *redis.Client
	logger *telemetry.Logger
}

func New(st *store.Store, em *epoch.Manager, rdb *redis.Client, logger *telemetry.Logger) *Service {
	return &Service{store: st, epoch: em, rdb: rdb, logger: logger}
}

func (s *Service) GetLevel(ctx context.Context, agentID, groupID, mandateID string) (string, error) {
	scopes := []struct{ st, sid string }{
		{"fleet", "ALL"},
		{"group", groupID},
		{"agent", agentID},
		{"mandate", mandateID},
	}

	levels := make([]string, 0, 4)
	for _, sc := range scopes {
		if sc.sid == "" {
			levels = append(levels, "OBSERVE")
			continue
		}
		cs, err := s.store.GetContainmentState(ctx, sc.st, sc.sid)
		if err != nil {
			levels = append(levels, "OBSERVE")
			continue
		}
		levels = append(levels, cs.Level)
	}

	return MostRestrictive(levels...), nil
}

type SetLevelResult struct {
	Level          string `json:"level"`
	EpochBumpedTo  *int64 `json:"epoch_bumped_to,omitempty"`
}

func (s *Service) SetLevel(ctx context.Context, scopeType, scopeID, level, reason, actor string) (*SetLevelResult, error) {
	if _, ok := levelOrder[level]; !ok {
		return nil, fmt.Errorf("invalid level: %s", level)
	}

	// Get previous level
	prevLevel := "OBSERVE"
	if prevCS, err := s.store.GetContainmentState(ctx, scopeType, scopeID); err == nil {
		prevLevel = prevCS.Level
	}

	// Upsert containment state
	cs := store.ContainmentState{
		ScopeType: scopeType, ScopeID: scopeID, Level: level, Reason: reason, Actor: actor,
	}
	if err := s.store.UpsertContainmentState(ctx, cs); err != nil {
		return nil, fmt.Errorf("upsert containment state: %w", err)
	}

	// Record event
	ev := store.ContainmentEvent{
		ScopeType: scopeType, ScopeID: scopeID, FromLevel: prevLevel, ToLevel: level,
		Reason: reason, Actor: actor,
	}

	result := &SetLevelResult{Level: level}

	// If HALT: bump epoch and sweep sagas
	if level == "HALT" {
		newEpoch, err := s.epoch.Bump(ctx, scopeType, scopeID)
		if err != nil {
			return nil, fmt.Errorf("bump epoch: %w", err)
		}
		ev.EpochBumpedTo = newEpoch
		result.EpochBumpedTo = &newEpoch
		s.logger.Info("epoch_bumped", map[string]interface{}{
			"scope": scopeType, "scope_id": scopeID, "epoch": newEpoch,
		})
	}

	// If QUARANTINE: add agents to quarantine set in Redis
	if level == "QUARANTINE" {
		s.addQuarantineFlag(ctx, scopeType, scopeID)
	}

	eventID, _ := s.store.InsertContainmentEvent(ctx, ev)

	if level == "HALT" && scopeType == "agent" {
		caseID := idgen.New()
		replayResult := []byte(`{}`)
		driftTrend := []byte(`{}`)
		
		_ = s.store.CreateRecoveryCase(ctx, caseID, scopeID, eventID, replayResult, driftTrend)
		
		s.logger.Info("recovery_case_created", map[string]interface{}{
			"case_id": caseID, "agent_id": scopeID, "event_id": eventID,
		})
		
		go s.assembleRecoveryCase(caseID, scopeID)
	}

	s.logger.Info("level_set", map[string]interface{}{
		"scope": scopeType, "scope_id": scopeID, "level": level, "from": prevLevel,
	})

	return result, nil
}

func (s *Service) assembleRecoveryCase(caseID, agentID string) {
	ctx := context.Background()
	
	// Mock agent-sim and behaviour-analytics for demo
	replayRes := []byte(`{"sandbox": "agent-sim", "replay": "success", "violations": 0}`)
	advRes := []byte(`{"sandbox": "agent-sim", "adversarial": "passed", "score": 95}`)
	driftRes := []byte(`{"trend": "stable", "jsd": 0.05}`)
	
	_ = s.store.UpdateRecoveryCaseAssembly(ctx, caseID, replayRes, advRes, driftRes)
	s.logger.Info("recovery_case_assembled", map[string]interface{}{"case_id": caseID})
}

func (s *Service) addQuarantineFlag(ctx context.Context, scopeType, scopeID string) {
	// For agent scope, add to quarantine_agents set
	// For group/fleet scope, we'd need to look up all agents — for Phase 2, just add the scope_id
	if scopeType == "agent" {
		s.rdb.SAdd(ctx, "quarantine_agents", scopeID)
	}
}

func (s *Service) Resume(ctx context.Context, scopeType, scopeID, actor string) (string, error) {
	result, err := s.SetLevel(ctx, scopeType, scopeID, "THROTTLE", "resume", actor)
	if err != nil {
		return "", err
	}
	// Remove from quarantine if present
	if scopeType == "agent" {
		s.rdb.SRem(ctx, "quarantine_agents", scopeID)
	}
	return result.Level, nil
}

func (s *Service) EmergencyStop(ctx context.Context, scopeType, scopeID, reason, actor string, sweepFn func(context.Context, string, string) int) (int64, int, error) {
	result, err := s.SetLevel(ctx, scopeType, scopeID, "HALT", reason, actor)
	if err != nil {
		return 0, 0, err
	}

	swept := 0
	if sweepFn != nil {
		swept = sweepFn(ctx, scopeType, scopeID)
	}

	var epoch int64
	if result.EpochBumpedTo != nil {
		epoch = *result.EpochBumpedTo
	}
	return epoch, swept, nil
}

func (s *Service) GetAllStates(ctx context.Context) ([]store.ContainmentState, error) {
	return s.store.GetAllContainmentStates(ctx)
}

func (s *Service) GetRecentEvents(ctx context.Context, limit int) ([]store.ContainmentEvent, error) {
	return s.store.GetRecentEvents(ctx, limit)
}

func (s *Service) GetOrphanedFunds() int64 {
	// For prototype, return 0 or fetch from sagas
	return 0
}

func _unused() {
	_ = idgen.New()
}
