package reaper

import (
	"context"
	"strconv"
	"time"

	"aegis/pkg/telemetry"
	"aegis/services/budget-ledger/internal/reserve"
	"aegis/services/budget-ledger/internal/tree"

	"github.com/redis/go-redis/v9"
)

type Reaper struct {
	rdb      *redis.Client
	reserve  *reserve.Service
	tree     *tree.Service
	logger   *telemetry.Logger
	interval time.Duration
}

func New(rdb *redis.Client, rs *reserve.Service, tr *tree.Service, logger *telemetry.Logger, intervalSec int) *Reaper {
	if intervalSec <= 0 {
		intervalSec = 5
	}
	return &Reaper{
		rdb:      rdb,
		reserve:  rs,
		tree:     tr,
		logger:   logger,
		interval: time.Duration(intervalSec) * time.Second,
	}
}

func (r *Reaper) Start(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.sweep(ctx)
		}
	}
}

func (r *Reaper) sweep(ctx context.Context) {
	now := time.Now().Unix()

	result, err := r.rdb.ZRangeByScore(ctx, "reservations:by_expiry", &redis.ZRangeBy{
		Min: "-inf",
		Max: strconv.FormatInt(now, 10),
	}).Result()
	if err != nil {
		r.logger.Error("reaper_zrange_failed", map[string]interface{}{"error": err.Error()})
		return
	}

	for _, reservationID := range result {
		state, err := r.rdb.HGet(ctx, "reservation:"+reservationID, "state").Result()
		if err != nil {
			continue
		}

		if state == "HELD" {
			if err := r.reserve.ReleaseInternal(ctx, reservationID, "EXPIRED_UNCOMMITTED"); err != nil {
				r.logger.Error("reaper_release_failed", map[string]interface{}{
					"reservation_id": reservationID,
					"error":          err.Error(),
				})
				continue
			}

			r.logger.Info("reaper_expired", map[string]interface{}{
				"reservation_id": reservationID,
			})
		}

		r.rdb.ZRem(ctx, "reservations:by_expiry", reservationID)
	}
}
