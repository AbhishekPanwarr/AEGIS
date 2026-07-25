package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"aegis/pkg/telemetry"
	"aegis/services/identity-service/internal/redis"
	"aegis/services/identity-service/internal/server"
	"aegis/services/identity-service/internal/store"
	"aegis/services/identity-service/internal/tokens"
)

func loadOrCreateKey(keyPath string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	if err := os.MkdirAll(filepath.Dir(keyPath), 0755); err != nil {
		return nil, nil, fmt.Errorf("create key dir: %w", err)
	}

	if data, err := os.ReadFile(keyPath); err == nil {
		if len(data) == ed25519.PrivateKeySize {
			priv := ed25519.PrivateKey(data)
			pub := priv.Public().(ed25519.PublicKey)
			return priv, pub, nil
		}
	}

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %w", err)
	}

	if err := os.WriteFile(keyPath, priv, 0600); err != nil {
		return nil, nil, fmt.Errorf("write key file: %w", err)
	}

	return priv, pub, nil
}

func main() {
	logger := telemetry.New("identity-service")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	keyPath := os.Getenv("SIGNING_KEY_PATH")
	if keyPath == "" {
		keyPath = "/data/identity_signing_key"
	}

	priv, pub, err := loadOrCreateKey(keyPath)
	if err != nil {
		logger.Error("key_setup_failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}

	logger.Info("key_loaded", map[string]interface{}{"path": keyPath, "pubkey": base64.StdEncoding.EncodeToString(pub)})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	st, err := store.New(ctx, logger)
	cancel()
	if err != nil {
		logger.Error("store_init_failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}

	rctx, rcancel := context.WithTimeout(context.Background(), 10*time.Second)
	rdb, err := redis.New(rctx)
	rcancel()
	if err != nil {
		logger.Error("redis_init_failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}

	containmentAddr := ""
	if v := os.Getenv("CONTAINMENT_ADDR"); v != "" {
		containmentAddr = v
	}

	tokSvc := tokens.New(priv, pub, rdb, st, rdb.RawClient(), containmentAddr)
	handler := server.New(st, rdb, tokSvc, logger, priv, pub)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      handler.Routes(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
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
	_ = st.Close(shutdownCtx)
	_ = rdb.Close()

	logger.Info("shutdown_complete")

	_ = json.Marshal
	_ = fmt.Sprintf
}
