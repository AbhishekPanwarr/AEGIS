package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"aegis/pkg/telemetry"
	"aegis/services/audit-chain/internal/store"
)

func main() {
	logger := telemetry.New("audit-chain")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8084"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	s, err := store.New(ctx, logger)
	cancel()
	if err != nil {
		logger.Error("startup_failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("POST /v1/decisions", s.HandleCreateDecision)
	mux.HandleFunc("PATCH /v1/decisions/{id}/finalize", s.HandleFinalize)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := s.HealthCheck(ctx); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "unavailable"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
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
	_ = s.Close(shutdownCtx)

	logger.Info("shutdown_complete")
}
