package redis

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/redis/go-redis/v9"
)

type Client struct {
	rdb *redis.Client
}

func New(ctx context.Context) (*Client, error) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "redis:6379"
	}

	rdb := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return &Client{rdb: rdb}, nil
}

func (c *Client) Close() error {
	return c.rdb.Close()
}

// EpochValues holds the four epoch counters for a token.
type EpochValues struct {
	Fleet   int64
	Group   int64
	Agent   int64
	Mandate int64
}

func (c *Client) ReadEpochs(ctx context.Context, agentID, groupID, mandateID string) (*EpochValues, error) {
	vals, err := c.rdb.MGet(ctx,
		"epoch:fleet",
		fmt.Sprintf("epoch:group:%s", groupID),
		fmt.Sprintf("epoch:agent:%s", agentID),
		fmt.Sprintf("epoch:mandate:%s", mandateID),
	).Result()
	if err != nil {
		return &EpochValues{}, nil // default to 0 on error — fail open for epochs in Phase 1
	}

	ev := &EpochValues{}
	for i, v := range vals {
		if v == nil {
			continue
		}
		n, _ := strconv.ParseInt(v.(string), 10, 64)
		switch i {
		case 0:
			ev.Fleet = n
		case 1:
			ev.Group = n
		case 2:
			ev.Agent = n
		case 3:
			ev.Mandate = n
		}
	}

	return ev, nil
}

func (c *Client) BumpMandateEpoch(ctx context.Context, mandateID string) error {
	return c.rdb.Incr(ctx, fmt.Sprintf("epoch:mandate:%s", mandateID)).Err()
}

func (c *Client) SIsMember(ctx context.Context, key, member string) (bool, error) {
	return c.rdb.SIsMember(ctx, key, member).Result()
}

func (c *Client) RawClient() *redis.Client {
	return c.rdb
}
