package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"aegis/pkg/telemetry"
	"aegis/services/containment-controller/internal/approvals"
	"aegis/services/containment-controller/internal/clients"
	"aegis/services/containment-controller/internal/epoch"
	"aegis/services/containment-controller/internal/ladder"
	"aegis/services/containment-controller/internal/sagas"
	"aegis/services/containment-controller/internal/server"
	"aegis/services/containment-controller/internal/store"

	"github.com/redis/go-redis/v9"
)

func main() {
	logger := telemetry.New("containment-controller")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8083"
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "redis:6379"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	st, err := store.New(ctx, logger)
	cancel()
	if err != nil {
		logger.Error("store_init_failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	rctx, rcancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := rdb.Ping(rctx).Err(); err != nil {
		logger.Error("redis_init_failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}
	rcancel()

	// Initialize epoch keys
	initCtx, initCancel := context.WithTimeout(context.Background(), 5*time.Second)
	em := epoch.New(rdb)
	_ = em.InitKeys(initCtx)
	initCancel()

	logger.Info("redis_connected", map[string]interface{}{"addr": redisAddr})

	// Create HTTP clients for downstream services
	railsAddr := getEnv("RAILS_ADDR", "http://mock-rails:8090")
	budgetAddr := getEnv("BUDGET_ADDR", "http://budget-ledger:8082")
	auditAddr := getEnv("AUDIT_ADDR", "http://audit-chain:8084")
	cl := clients.New(railsAddr, budgetAddr, auditAddr)

	// Create services
	ladderSvc := ladder.New(st, em, rdb, logger)
	sagasSvc := sagas.New(st, rdb, cl, logger)
	approvalsSvc := approvals.New(st, logger)

	handler := server.New(ladderSvc, sagasSvc, approvalsSvc, st, logger)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      handler.Routes(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("startup", map[string]interface{}{"port": port})
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("listen_failed", map[string]interface{}{"error": err.Error()})
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutdown", map[string]interface{}{"reason": "signal_received"})

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	_ = srv.Shutdown(shutdownCtx)
	st.Close()
	_ = rdb.Close()

	logger.Info("shutdown_complete")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
