package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"
)

const budgetURL = "http://localhost:8082"

type reserveResp struct {
	ReservationID string `json:"reservation_id"`
	State         string `json:"state"`
}

type usageResp struct {
	CapMinor       int64 `json:"cap_minor"`
	CommittedMinor int64 `json:"committed_minor"`
	ReservedMinor  int64 `json:"reserved_minor"`
	HeadroomMinor  int64 `json:"headroom_minor"`
}

func createBudgetNode(t *testing.T, label string, cap int64) string {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"label": label, "cap_minor": cap, "currency": "USD",
	})
	resp, err := http.Post(budgetURL+"/v1/budget-nodes", "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != 201 {
		t.Fatalf("create budget node: %v status=%d", err, resp.StatusCode)
	}
	defer resp.Body.Close()
	var node struct{ ID string `json:"id"` }
	json.NewDecoder(resp.Body).Decode(&node)
	return node.ID
}

func getUsage(t *testing.T, nodeID string) usageResp {
	t.Helper()
	resp, err := http.Get(budgetURL + "/v1/budget-nodes/" + nodeID + "/usage")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("get usage: %v", err)
	}
	defer resp.Body.Close()
	var u usageResp
	json.NewDecoder(resp.Body).Decode(&u)
	return u
}

func commitReservation(t *testing.T, reservationID string) {
	t.Helper()
	resp, err := http.Post(budgetURL+"/v1/reservations/"+reservationID+"/commit", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Errorf("commit %s: %v", reservationID, err)
		return
	}
	resp.Body.Close()
}

func TestConcurrentReservationsExactToCap(t *testing.T) {
	skipIfNoStack(t)

	const numRuns = 10
	const numConcurrent = 100
	const amountEach = 100
	const cap = int64(10000)

	for run := 0; run < numRuns; run++ {
		t.Run(fmt.Sprintf("run_%d", run+1), func(t *testing.T) {
			nodeID := createBudgetNode(t, fmt.Sprintf("race-%d-%d", run, time.Now().UnixNano()), cap)

			var wg sync.WaitGroup
			var mu sync.Mutex
			successCount := 0
			denyCount := 0

			for i := 0; i < numConcurrent; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()

					body, _ := json.Marshal(map[string]interface{}{
						"path":            []string{nodeID},
						"amount_minor":    amountEach,
						"idempotency_key": fmt.Sprintf("race-%d-%d", run, idx),
					})

					resp, err := http.Post(budgetURL+"/v1/reservations", "application/json", bytes.NewReader(body))
					if err != nil {
						t.Errorf("reserve: %v", err)
						return
					}
					defer resp.Body.Close()

					if resp.StatusCode == 201 {
						var r reserveResp
						json.NewDecoder(resp.Body).Decode(&r)

						mu.Lock()
						successCount++
						mu.Unlock()

						commitReservation(t, r.ReservationID)
					} else if resp.StatusCode == 409 {
						mu.Lock()
						denyCount++
						mu.Unlock()
					}
				}(i)
			}

			wg.Wait()

			// Allow a brief moment for all commits to settle
			time.Sleep(500 * time.Millisecond)

			usage := getUsage(t, nodeID)
			total := usage.CommittedMinor + usage.ReservedMinor

			if total != cap {
				t.Errorf("run %d: committed(%d) + reserved(%d) = %d, expected exactly %d",
					run+1, usage.CommittedMinor, usage.ReservedMinor, total, cap)
			} else {
				t.Logf("run %d: OK — committed=%d, reserved=%d, total=%d (exactly at cap)",
					run+1, usage.CommittedMinor, usage.ReservedMinor, total)
			}

			if successCount != int(cap/amountEach) {
				t.Logf("run %d: successCount=%d, denyCount=%d (expected %d successes)",
					run+1, successCount, denyCount, cap/amountEach)
			}
		})
	}
}

func skipIfNoStack(t *testing.T) {
	t.Helper()
	resp, err := http.Get(budgetURL + "/healthz")
	if err != nil || resp.StatusCode != 200 {
		t.Skip("stack not running — skipping race test")
	}
	if resp != nil {
		resp.Body.Close()
	}
}

func TestRaceBenchmark1000x100(t *testing.T) {
	skipIfNoStack(t)

	const numRuns = 10
	const numConcurrent = 1000
	const amountEach = 100
	const cap = int64(100000)

	passCount := 0
	overshootTotal := int64(0)

	for run := 0; run < numRuns; run++ {
		nodeID := createBudgetNode(t, fmt.Sprintf("bench-%d", run), cap)

		// Reset Redis state for this node (committed=0, reserved=0)
		// This avoids Postgres writes on each run
		resetResp, err := http.Post(budgetURL+"/v1/budget-nodes", "application/json",
			bytes.NewReader([]byte(fmt.Sprintf(`{"label":"bench-reset-%d-%d","cap_minor":%d,"currency":"USD"}`, run, time.Now().UnixNano(), cap))))
		if err == nil && resetResp.StatusCode == 201 {
			var resetNode struct{ ID string `json:"id"` }
			json.NewDecoder(resetResp.Body).Decode(&resetNode)
			resetResp.Body.Close()
			nodeID = resetNode.ID
		}

		var wg sync.WaitGroup
		var mu sync.Mutex
		successCount := 0
		denyCount := 0

		for i := 0; i < numConcurrent; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				body, _ := json.Marshal(map[string]interface{}{
					"path":            []string{nodeID},
					"amount_minor":    amountEach,
					"idempotency_key": fmt.Sprintf("bench-%d-%d", run, idx),
				})

				resp, err := http.Post(budgetURL+"/v1/reservations", "application/json", bytes.NewReader(body))
				if err != nil {
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode == 201 {
					mu.Lock()
					successCount++
					mu.Unlock()
				} else if resp.StatusCode == 409 {
					mu.Lock()
					denyCount++
					mu.Unlock()
				}
			}(i)
		}

		wg.Wait()
		time.Sleep(100 * time.Millisecond)

		usage := getUsage(t, nodeID)
		total := usage.CommittedMinor + usage.ReservedMinor

		if total == cap {
			passCount++
		} else {
			overshoot := total - cap
			overshootTotal += overshoot
			if run < 5 { // only log first 5 failures to avoid spam
				t.Errorf("run %d: total=%d, expected %d (overshoot=%d)",
					run+1, total, cap, overshoot)
			}
		}

		if (run+1)%10 == 0 {
			t.Logf("Progress: %d/%d runs passed", passCount, run+1)
		}
	}

	t.Logf("=== RACE BENCHMARK RESULTS ===")
	t.Logf("Real endpoint: %d/%d runs passed (exact cap)", passCount, numRuns)
	if passCount != numRuns {
		t.Errorf("FAIL: %d runs did not hit exact cap", numRuns-passCount)
	}
}

func TestNaiveOvershoot(t *testing.T) {
	skipIfNoStack(t)

	const numConcurrent = 1000
	const amountEach = 100
	const cap = int64(100000)

	nodeID := createBudgetNode(t, fmt.Sprintf("naive-%d", time.Now().UnixNano()), cap)

	var wg sync.WaitGroup

	for i := 0; i < numConcurrent; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			body, _ := json.Marshal(map[string]interface{}{
				"path":            []string{nodeID},
				"amount_minor":    amountEach,
				"idempotency_key": fmt.Sprintf("naive-%d", idx),
			})
			resp, err := http.Post(budgetURL+"/v1/reservations-naive", "application/json", bytes.NewReader(body))
			if err != nil {
				return
			}
			resp.Body.Close()
		}(i)
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	usage := getUsage(t, nodeID)
	total := usage.CommittedMinor + usage.ReservedMinor

	if total <= cap {
		t.Errorf("naive endpoint unexpectedly did not overshoot: total=%d (cap=%d)", total, cap)
	} else {
		t.Logf("Naive endpoint overshot: total=%d, cap=%d, overshoot=%d", total, cap, total-cap)
	}
}

func _unused() {
	_ = strconv.Itoa
	_ = io.ReadAll
}
