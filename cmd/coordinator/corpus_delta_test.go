package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/poolfuzz"
	"hackme/internal/store"
)

func corpusDeltaMux(t *testing.T) (*http.ServeMux, *poolfuzz.Service, string, int64) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "corpus-delta.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	pf := &poolfuzz.Service{DB: db}
	ctx := context.Background()
	cfg := map[string]any{
		"pool_distributed": true, "work_kind": "hunt_shard", "campaign_type": "hunt",
		"upstream_target_id": "jsmn", "harness_hash": "abcdef0123456789",
		"iterations_per_shard": 2, "max_input_bytes": 256,
	}
	id := "cd-camp"
	if err := pf.RegisterCampaign(ctx, poolfuzz.Campaign{
		ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 2, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	_ = pf.EnsureWorkItems(ctx, id, time.Now().Unix())
	var itemID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM fuzz_work_items WHERE campaign_id=? LIMIT 1`, id).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	_, _ = db.ExecContext(ctx,
		`UPDATE fuzz_work_items SET status='leased', lease_owner=?, lease_until=?, updated_at=? WHERE id=?`,
		"w1", now+120, now, itemID)

	wm := &workManager{
		hybridSignerEnabled: true,
		claimRequirePubKey:  true,
		claimPerMin:         60,
		submitPerMin:        60,
		abuse:               make(map[string]workerAbuseState),
		ipAbuse:             make(map[string]workerAbuseState),
		worker:              map[string]workerPayoutStat{},
		dropReasonCount:     make(map[string]uint64),
	}
	mux := http.NewServeMux()
	addFuzzPoolRoutes(mux, "admin-tok", "worker-tok", false, wm, pf)
	addCorpusDeltaRoute(mux, "admin-tok", "worker-tok", false, pf)
	return mux, pf, id, itemID
}

func TestCorpusDeltaUnauthenticatedRejected(t *testing.T) {
	mux, _, id, itemID := corpusDeltaMux(t)
	body, _ := json.Marshal(map[string]any{
		"worker_id": "w1", "campaign_id": id, "item_id": itemID,
		"namespace": "research:jsmn",
		"seeds":     []map[string]any{{"input_bytes": base64.StdEncoding.EncodeToString([]byte("x"))}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/work/corpus_delta", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestCorpusDeltaWorkerTokenAcceptedWithLease(t *testing.T) {
	mux, pf, id, itemID := corpusDeltaMux(t)
	body, _ := json.Marshal(map[string]any{
		"worker_id": "w1", "campaign_id": id, "item_id": itemID,
		"namespace": "research:jsmn",
		"seeds": []map[string]any{{
			"input_bytes": base64.StdEncoding.EncodeToString([]byte("new-seed")),
			"energy":      2,
		}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/work/corpus_delta", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "worker-tok")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	seeds, err := pf.ListNamespaceCorpus(context.Background(), "research:jsmn", 16)
	if err != nil || len(seeds) < 1 {
		t.Fatalf("seeds=%d err=%v", len(seeds), err)
	}
}

func TestCorpusDeltaRejectsPackNamespaceInject(t *testing.T) {
	mux, _, id, itemID := corpusDeltaMux(t)
	body, _ := json.Marshal(map[string]any{
		"worker_id": "w1", "campaign_id": id, "item_id": itemID,
		"namespace": "pack:secrets",
		"seeds":     []map[string]any{{"input_bytes": base64.StdEncoding.EncodeToString([]byte("evil"))}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/work/corpus_delta", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "worker-tok")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("pack namespace inject must fail")
	}
}

func TestCorpusDeltaForeignLeaseForbidden(t *testing.T) {
	mux, _, id, itemID := corpusDeltaMux(t)
	body, _ := json.Marshal(map[string]any{
		"worker_id": "attacker", "campaign_id": id, "item_id": itemID,
		"namespace": "research:jsmn",
		"seeds":     []map[string]any{{"input_bytes": base64.StdEncoding.EncodeToString([]byte("x"))}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/work/corpus_delta", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "worker-tok")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
