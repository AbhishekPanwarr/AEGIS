package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"aegis/pkg/telemetry"
)

func main() {
	logger := telemetry.New("mock-rails")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8090"
	}

	var armedFailures sync.Map

	mux := http.NewServeMux()

	mux.HandleFunc("POST /v1/execute", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()

		var req map[string]interface{}
		_ = json.Unmarshal(body, &req)

		// Check for armed failure
		if sagaID, ok := req["saga_id"]; ok && sagaID != "" {
			if legSeq, ok := req["leg_seq"]; ok {
				key := fmt.Sprintf("%v:%v", sagaID, legSeq)
				if _, armed := armedFailures.LoadAndDelete(key); armed {
					logger.Info("execute_armed_failure", map[string]interface{}{
						"saga_id": sagaID, "leg_seq": legSeq,
					})
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"error": map[string]string{
							"code":    "RAIL_ERROR",
							"message": "armed failure",
						},
					})
					return
				}
			}
		}

		logger.Info("execute", map[string]interface{}{
			"action_type":     req["action_type"],
			"counterparty_id": req["counterparty_id"],
			"amount_minor":    req["amount_minor"],
		})

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":   "executed",
			"rail_ref": "demo-001",
		})
	})

	mux.HandleFunc("POST /v1/test/arm-failure", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SagaID string `json:"saga_id"`
			LegSeq int    `json:"leg_seq"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
			return
		}

		key := fmt.Sprintf("%s:%d", req.SagaID, req.LegSeq)
		armedFailures.Store(key, true)

		logger.Info("arm_failure", map[string]interface{}{
			"saga_id": req.SagaID, "leg_seq": req.LegSeq,
		})

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"armed": true})
	})

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("shutdown_failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}

	logger.Info("shutdown_complete")
}
