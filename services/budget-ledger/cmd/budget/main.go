package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"aegis/pkg/telemetry"
	"aegis/services/budget-ledger/internal/reaper"
	"aegis/services/budget-ledger/internal/reserve"
	"aegis/services/budget-ledger/internal/server"
	"aegis/services/budget-ledger/internal/tree"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	logger := telemetry.New("budget-ledger")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}

	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://aegis:aegis@postgres:5432/aegis"
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "redis:6379"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		logger.Error("parse_dsn_failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}
	poolConfig.MaxConns = 50
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		logger.Error("postgres_connect_failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}
	if err := pool.Ping(ctx); err != nil {
		logger.Error("postgres_ping_failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}
	cancel()

	logger.Info("postgres_connected")

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	rctx, rcancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := rdb.Ping(rctx).Err(); err != nil {
		logger.Error("redis_connect_failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}
	rcancel()

	logger.Info("redis_connected")

	treeSvc := tree.New(pool, rdb)
	reserveSvc := reserve.New(rdb)

	reaperInterval := 5
	if v := os.Getenv("REAPER_INTERVAL_SECONDS"); v != "" {
		reaperInterval, _ = strconv.Atoi(v)
	}

	reaperCtx, reaperCancel := context.WithCancel(context.Background())
	rp := reaper.New(rdb, reserveSvc, treeSvc, logger, reaperInterval)
	go rp.Start(reaperCtx)

	handler := server.New(treeSvc, reserveSvc, rdb, logger)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      handler.Routes(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	go func() {
		logger.Info("startup", map[string]interface{}{"port": port, "naive_mode": os.Getenv("DEMO_NAIVE_MODE") == "true"})
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("listen_failed", map[string]interface{}{"error": err.Error()})
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutdown", map[string]interface{}{"reason": "signal_received"})

	reaperCancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	_ = srv.Shutdown(shutdownCtx)
	pool.Close()
	_ = rdb.Close()

	logger.Info("shutdown_complete")
}
