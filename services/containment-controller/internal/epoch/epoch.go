package epoch

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type Manager struct {
	rdb *redis.Client
}

type Update struct {
	Scope   string `json:"scope"`
	ScopeID string `json:"scope_id"`
	Value   int64  `json:"value"`
}

func New(rdb *redis.Client) *Manager {
	return &Manager{rdb: rdb}
}

func (m *Manager) InitKeys(ctx context.Context) error {
	keys := []string{"epoch:fleet:ALL"}
	for _, key := range keys {
		m.rdb.SetNX(ctx, key, 0, 0)
	}
	return nil
}

func epochKey(scopeType, scopeID string) string {
	if scopeType == "fleet" {
		return "epoch:fleet:ALL"
	}
	return fmt.Sprintf("epoch:%s:%s", scopeType, scopeID)
}

func (m *Manager) Bump(ctx context.Context, scopeType, scopeID string) (int64, error) {
	key := epochKey(scopeType, scopeID)
	val, err := m.rdb.Incr(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("incr epoch %s: %w", key, err)
	}

	update := Update{Scope: scopeType, ScopeID: scopeID, Value: val}
	msg, _ := json.Marshal(update)
	m.rdb.Publish(ctx, "epoch:updates", string(msg))

	return val, nil
}

func (m *Manager) Read(ctx context.Context, scopeType, scopeID string) (int64, error) {
	key := epochKey(scopeType, scopeID)
	val, err := m.rdb.Get(ctx, key).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return val, err
}

func (m *Manager) ReadAll(ctx context.Context, scopeTypes []string, scopeIDs []string) ([]int64, error) {
	if len(scopeTypes) != len(scopeIDs) {
		return nil, fmt.Errorf("mismatched lengths")
	}
	result := make([]int64, len(scopeTypes))
	for i := range scopeTypes {
		v, err := m.Read(ctx, scopeTypes[i], scopeIDs[i])
		if err != nil {
			return nil, err
		}
		result[i] = v
	}
	return result, nil
}

// Cache is an in-memory epoch cache that subscribes to updates and periodically refreshes.
type Cache struct {
	rdb            *redis.Client
	mu             sync.RWMutex
	epochs         map[string]int64
	lastUpdate     time.Time
	stalenessBound time.Duration
}

func NewCache(rdb *redis.Client, stalenessBoundSec int) *Cache {
	return &Cache{
		rdb:            rdb,
		epochs:         make(map[string]int64),
		stalenessBound: time.Duration(stalenessBoundSec) * time.Second,
	}
}

func (c *Cache) Start(ctx context.Context) {
	go c.subscribe(ctx)
	go c.periodicRefresh(ctx)
}

func (c *Cache) subscribe(ctx context.Context) {
	sub := c.rdb.Subscribe(ctx, "epoch:updates")
	defer sub.Close()

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			if msg == nil {
				continue
			}
			var update Update
			if err := json.Unmarshal([]byte(msg.Payload), &update); err != nil {
				continue
			}
			key := update.Scope + ":" + update.ScopeID
			if update.Scope == "fleet" {
				key = "fleet:ALL"
			}
			c.mu.Lock()
			c.epochs[key] = update.Value
			c.lastUpdate = time.Now()
			c.mu.Unlock()
		}
	}
}

func (c *Cache) periodicRefresh(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refresh(ctx)
		}
	}
}

func (c *Cache) refresh(ctx context.Context) {
	keys := []string{"epoch:fleet:ALL"}
	vals, err := c.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, v := range vals {
		if v == nil {
			continue
		}
		var n int64
		switch val := v.(type) {
		case string:
			fmt.Sscanf(val, "%d", &n)
		}
		c.epochs["fleet:ALL"] = n
	}
	c.lastUpdate = time.Now()
}

func (c *Cache) GetEpoch(scopeType, scopeID string) int64 {
	key := scopeType + ":" + scopeID
	if scopeType == "fleet" {
		key = "fleet:ALL"
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.epochs[key]
}

func (c *Cache) IsStale() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Since(c.lastUpdate) > c.stalenessBound
}

func (c *Cache) LastUpdate() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastUpdate
}
