package epoch

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type Update struct {
	Scope   string `json:"scope"`
	ScopeID string `json:"scope_id"`
	Value   int64  `json:"value"`
}

type Cache struct {
	rdb            *redis.Client
	mu             sync.RWMutex
	epochs         map[string]int64
	lastUpdate     time.Time
	stalenessBound time.Duration
}

func NewCache(rdb *redis.Client, stalenessBoundSec int) *Cache {
	if stalenessBoundSec <= 0 {
		stalenessBoundSec = 2
	}
	return &Cache{
		rdb:            rdb,
		epochs:         make(map[string]int64),
		stalenessBound: time.Duration(stalenessBoundSec) * time.Second,
	}
}

func (c *Cache) Start(ctx context.Context) {
	// Do an initial full refresh
	c.refresh(ctx)

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

// Check verifies the token's epochs are not behind the cached values.
func (c *Cache) Check(tokenEpochFleet, tokenEpochGroup, tokenEpochAgent, tokenEpochMandate int64, groupID, agentID, mandateID string) error {
	if c.IsStale() {
		return fmt.Errorf("epoch cache is stale")
	}

	cachedFleet := c.GetEpoch("fleet", "ALL")
	if tokenEpochFleet < cachedFleet {
		return fmt.Errorf("token epoch_fleet %d < current %d", tokenEpochFleet, cachedFleet)
	}

	if groupID != "" {
		cachedGroup := c.GetEpoch("group", groupID)
		if tokenEpochGroup < cachedGroup {
			return fmt.Errorf("token epoch_group %d < current %d", tokenEpochGroup, cachedGroup)
		}
	}

	if agentID != "" {
		cachedAgent := c.GetEpoch("agent", agentID)
		if tokenEpochAgent < cachedAgent {
			return fmt.Errorf("token epoch_agent %d < current %d", tokenEpochAgent, cachedAgent)
		}
	}

	if mandateID != "" {
		cachedMandate := c.GetEpoch("mandate", mandateID)
		if tokenEpochMandate < cachedMandate {
			return fmt.Errorf("token epoch_mandate %d < current %d", tokenEpochMandate, cachedMandate)
		}
	}

	return nil
}
