package workercoord

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func mockCoordinator(t *testing.T, hybridStrict bool) *httptest.Server {
	t.Helper()
	var claims atomic.Uint64
	var submits atomic.Uint64
	mux := http.NewServeMux()
	mux.HandleFunc("/api/work/claim", func(w http.ResponseWriter, r *http.Request) {
		claims.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":          true,
			"base_nonce":  1000,
			"batch_size":  1000,
			"work_id":     "w-net-test:1000:1000",
			"target_mod":  1_000_000,
			"lease_until": time.Now().Add(90 * time.Second).Unix(),
		})
	})
	mux.HandleFunc("/api/work/submit", func(w http.ResponseWriter, r *http.Request) {
		submits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":         true,
			"accepted":   true,
			"payout_hmc": 0.001,
		})
	})
	_ = hybridStrict
	return httptest.NewServer(mux)
}

func TestClient_ClaimWithRetry_SurvivesHighLatencyAndLoss(t *testing.T) {
	srv := mockCoordinator(t, false)
	defer srv.Close()

	ft := NewFaultTransport(http.DefaultTransport, FaultConfig{
		MinLatency: 500 * time.Millisecond,
		MaxLatency: 800 * time.Millisecond,
		DropRate:   0.30,
	})
	cl := &Client{
		BaseURL: srv.URL,
		Token:   "test-token",
		HTTP: &http.Client{
			Timeout:   5 * time.Second,
			Transport: ft,
		},
		Backoff: 2 * time.Millisecond, // speed up test; still exercises doubling
	}

	start := time.Now()
	cr, attempts, err := cl.ClaimWithRetry("worker-net-fault", 1000, 25)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("claim never succeeded after retries: %v (attempts=%d calls=%d dropped=%d)",
			err, attempts, ft.Calls, ft.Dropped)
	}
	if cr.BatchSize != 1000 {
		t.Fatalf("unexpected claim: %+v", cr)
	}
	if attempts < 2 {
		t.Logf("succeeded quickly attempts=%d (network got lucky)", attempts)
	}
	if elapsed > 90*time.Second {
		t.Fatalf("retry loop too slow: %v", elapsed)
	}
	if cl.Backoff > 45*time.Second {
		t.Fatalf("backoff exceeded cap: %v", cl.Backoff)
	}
	calls, dropped, delivered := ft.Snapshot()
	if calls < attempts {
		t.Fatalf("expected at least one transport call per attempt")
	}
	if dropped == 0 && delivered == 0 {
		t.Fatal("fault transport did not run")
	}
	t.Logf("net fault: attempts=%d calls=%d dropped=%d delivered=%d elapsed=%v",
		attempts, calls, dropped, delivered, elapsed)
}

func TestClient_SubmitRetryDoesNotDuplicateAcceptedWork(t *testing.T) {
	var submitCount atomic.Uint64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/work/claim":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "base_nonce": 1, "batch_size": 500, "work_id": "w:1:500", "target_mod": 1000,
			})
		case "/api/work/submit":
			n := submitCount.Add(1)
			if n <= 2 {
				// First two transport tries fail at HTTP layer (miner retries same payload).
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"ok":false,"reason":"temp_unavailable"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "accepted": true, "payout_hmc": 0.01})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Claim on clean transport; fault only on submit retries.
	clClean := &Client{
		BaseURL: srv.URL,
		Token:   "tok",
		HTTP:    NewHTTPClient(3 * time.Second),
	}
	cr, err := clClean.Claim("w-dedup", 500)
	if err != nil {
		t.Fatal(err)
	}
	ft := NewFaultTransport(http.DefaultTransport, FaultConfig{
		MinLatency: 50 * time.Millisecond,
		MaxLatency: 120 * time.Millisecond,
		DropRate:   0.20,
	})
	cl := &Client{
		BaseURL: srv.URL,
		Token:   "tok",
		HTTP:    &http.Client{Timeout: 3 * time.Second, Transport: ft},
		Backoff: 5 * time.Millisecond,
	}
	req := SubmitRequest{
		WorkerID:  "w-dedup",
		BaseNonce: cr.BaseNonce,
		BatchSize: cr.BatchSize,
		WorkID:    cr.WorkID,
		Attempts:  500,
	}
	var last SubmitResponse
	cl.ResetBackoff()
	for i := 0; i < 8; i++ {
		last, err = cl.Submit(req)
		if err == nil && last.OK {
			break
		}
		time.Sleep(cl.SleepBackoff("submit"))
	}
	if !last.OK || !last.Accepted {
		t.Fatalf("submit never accepted: %+v err=%v submits=%d", last, err, submitCount.Load())
	}
	if submitCount.Load() > 6 {
		t.Fatalf("too many submit HTTP calls (%d) — possible runaway retry", submitCount.Load())
	}
}

func TestClient_BackoffCapsAt45Seconds(t *testing.T) {
	cl := &Client{Backoff: 2 * time.Second}
	var waits []time.Duration
	for i := 0; i < 10; i++ {
		waits = append(waits, cl.SleepBackoff("claim"))
	}
	if cl.Backoff != 45*time.Second {
		t.Fatalf("backoff cap: got %v want 45s", cl.Backoff)
	}
	if waits[len(waits)-1] != 45*time.Second {
		t.Fatalf("last wait should be capped: %v", waits[len(waits)-1])
	}
}
