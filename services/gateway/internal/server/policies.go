package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
	"os"
    
	"aegis/pkg/apierr"
    "github.com/jackc/pgx/v5"
)

func getPgConn(ctx context.Context) (*pgx.Conn, error) {
    dsn := os.Getenv("POSTGRES_DSN")
    if dsn == "" {
        dsn = "postgresql://aegis:aegis@postgres:5432/aegis"
    }
    return pgx.Connect(ctx, dsn)
}

func (h *Handler) HandlePromotePolicy(w http.ResponseWriter, r *http.Request) {
    var req struct {
        CedarSource string `json:"cedar_source"`
        State       string `json:"state"`
        Author      string `json:"author"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        apierr.WriteError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
        return
    }
    
    hash := sha256.Sum256([]byte(req.CedarSource))
    versionHash := hex.EncodeToString(hash[:])
    
    ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
    defer cancel()
    
    conn, err := getPgConn(ctx)
    if err != nil {
        apierr.WriteError(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
        return
    }
    defer conn.Close(ctx)
    
    _, err = conn.Exec(ctx, `
        INSERT INTO policy_versions (version_hash, cedar_source, state, author)
        VALUES ($1, $2, $3, $4)
        ON CONFLICT (version_hash) DO UPDATE SET state = $3, author = $4
    `, versionHash, req.CedarSource, req.State, req.Author)
    
    if err != nil {
        apierr.WriteError(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
        return
    }
    
    if req.State == "ACTIVE" || req.State == "SHADOW" {
        agentURL := "http://cedar-agent:8180"
        if req.State == "SHADOW" {
            agentURL = "http://cedar-agent-shadow:8181"
        }
        
        httpReq, _ := http.NewRequest(http.MethodPut, agentURL+"/v1/policies/policy0", bytes.NewBufferString(req.CedarSource))
        httpReq.Header.Set("Content-Type", "text/plain")
        resp, err := http.DefaultClient.Do(httpReq)
        if err == nil {
            resp.Body.Close()
        }
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"version_hash": versionHash, "state": req.State})
}

func (h *Handler) HandleRetroPolicy(w http.ResponseWriter, r *http.Request) {
    hash := r.PathValue("hash")
    
    var req struct {
        From string `json:"from"`
        To   string `json:"to"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        apierr.WriteError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
        return
    }
    
    ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
    defer cancel()
    
    conn, err := getPgConn(ctx)
    if err != nil {
        apierr.WriteError(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
        return
    }
    defer conn.Close(ctx)
    
    // Check if the policy hash exists
    var policyState string
    err = conn.QueryRow(ctx, "SELECT state FROM policy_versions WHERE version_hash = $1", hash).Scan(&policyState)
    if err != nil {
        apierr.WriteError(w, http.StatusNotFound, "POLICY_NOT_FOUND", "policy version not found")
        return
    }
    
    rows, err := conn.Query(ctx, `
        SELECT decision, context_snapshot 
        FROM decision_records 
        WHERE created_at >= $1::timestamptz AND created_at <= $2::timestamptz
          AND context_snapshot IS NOT NULL
    `, req.From, req.To)
    
    if err != nil {
        apierr.WriteError(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
        return
    }
    defer rows.Close()
    
    changed := 0
    unchanged := 0
    samples := []map[string]interface{}{}
    
    for rows.Next() {
        var originalDecision string
        var contextSnapshot json.RawMessage
        if err := rows.Scan(&originalDecision, &contextSnapshot); err != nil {
            continue
        }
        
        var event struct {
            AmountMinor    int64  `json:"amount_minor"`
            ActionType     string `json:"action_type"`
            CounterpartyID string `json:"counterparty_id"`
            AgentID        string `json:"agent_id"`
            MandateID      string `json:"mandate_id"`
        }
        _ = json.Unmarshal(contextSnapshot, &event)
        
        cedarReq := map[string]interface{}{
            "principal": fmt.Sprintf(`Agent::"%s"`, event.AgentID),
            "action":    fmt.Sprintf(`Action::"%s"`, event.ActionType),
            "resource":  fmt.Sprintf(`Counterparty::"%s"`, event.CounterpartyID),
            "context": map[string]interface{}{
                "amount_minor":      event.AmountMinor,
                "mandate_id":        event.MandateID,
            },
        }
        body, _ := json.Marshal(cedarReq)
        
        // Replay against shadow agent
        rReq, _ := http.NewRequest(http.MethodPost, "http://cedar-agent-shadow:8181/v1/is_authorized", bytes.NewBuffer(body))
        rReq.Header.Set("Content-Type", "application/json")
        rResp, rErr := http.DefaultClient.Do(rReq)
        
        replayedDecision := "DENY"
        if rErr == nil {
            var cResp map[string]interface{}
            json.NewDecoder(rResp.Body).Decode(&cResp)
            if d, ok := cResp["decision"].(string); ok {
                if d == "Allow" {
                    replayedDecision = "ALLOW"
                }
            }
            rResp.Body.Close()
        }
        
        if originalDecision != replayedDecision {
            changed++
            if len(samples) < 10 {
                samples = append(samples, map[string]interface{}{
                    "agent_id": event.AgentID,
                    "original": originalDecision,
                    "replayed": replayedDecision,
                })
            }
        } else {
            unchanged++
        }
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "policy_hash": hash,
        "changed": changed,
        "unchanged": unchanged,
        "samples": samples,
    })
}
