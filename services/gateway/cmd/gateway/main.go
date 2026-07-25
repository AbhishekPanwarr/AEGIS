package main

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"aegis/pkg/telemetry"
	"aegis/services/gateway/internal/clients"
	"aegis/services/gateway/internal/epoch"
	"aegis/services/gateway/internal/server"

	"github.com/redis/go-redis/v9"
)

func main() {
	logger := telemetry.New("gateway")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	railsAddr := getEnv("RAILS_ADDR", "http://mock-rails:8090")
	auditAddr := getEnv("AUDIT_ADDR", "http://audit-chain:8084")
	identityAddr := getEnv("IDENTITY_ADDR", "http://identity-service:8081")
	policyAddr := getEnv("POLICY_ADDR", "http://cedar-agent:8180")
	budgetAddr := getEnv("BUDGET_ADDR", "http://budget-ledger:8082")

	identityClient := clients.NewIdentityClient(identityAddr)

	// Fetch identity-service public key at startup.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	pubkeyBytes, err := identityClient.GetPubkey(ctx)
	cancel()
	if err != nil {
		logger.Error("pubkey_fetch_failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}
	pubkey := ed25519.PublicKey(pubkeyBytes)
	logger.Info("pubkey_fetched", map[string]interface{}{"len": len(pubkeyBytes)})

	railClient := clients.NewRailClient(railsAddr)
	auditClient := clients.NewAuditClient(auditAddr)
	policyClient := clients.NewPolicyClient(policyAddr)
	budgetClient := clients.NewBudgetClient(budgetAddr)

	containmentAddr := getEnv("CONTAINMENT_ADDR", "http://containment-controller:8083")
	containmentClient := clients.NewContainmentClient(containmentAddr)

	redisAddr := getEnv("REDIS_ADDR", "redis:6379")
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	rctx, rcancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := rdb.Ping(rctx).Err(); err != nil {
		logger.Error("redis_connect_failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}
	rcancel()
	logger.Info("redis_connected", map[string]interface{}{"addr": redisAddr})

	// Start epoch cache
	stalenessBound := 2
	if v := os.Getenv("EPOCH_STALENESS_BOUND_SECONDS"); v != "" {
		stalenessBound, _ = strconv.Atoi(v)
	}
	epochCache := epoch.NewCache(rdb, stalenessBound)
	epochCtx, epochCancel := context.WithCancel(context.Background())
	epochCache.Start(epochCtx)
	logger.Info("epoch_cache_started", map[string]interface{}{"staleness_bound_sec": stalenessBound})

	handler := server.New(identityClient, policyClient, budgetClient, auditClient, railClient, containmentClient, rdb, epochCache, logger, pubkey)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      handler.Routes(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 20 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("startup", map[string]interface{}{
			"port":          port,
			"rails_addr":    railsAddr,
			"audit_addr":    auditAddr,
			"identity_addr": identityAddr,
			"policy_addr":   policyAddr,
			"budget_addr":   budgetAddr,
		})
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

	epochCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown_failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}

	logger.Info("shutdown_complete")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
