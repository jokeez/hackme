package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/chain"
	"hackme/internal/poolfuzz"
	"hackme/internal/store"
)

// Admin-token registration on a minting hub must not create a paid pool campaign whose
// escrow is not on the hub ledger: escrows on other nodes only credit that node's local
// accounts, so miners would do the work and never receive HMC (bootstrap/lab, 2026-11-12).
func TestAdminRegisterRequiresHubEscrowOnMintingHub(t *testing.T) {
	t.Setenv("HACKME_CHAIN_MINTING_HUB", "1")
	t.Setenv("HACKME_ORDERS_PREPAID_HUB_SECRET", "test-hub-prepaid-secret")
	t.Setenv("HACKME_ORDERS_PREPAID_ALLOW_ALGEBRAIC_MINT", "")
	t.Setenv("HACKME_COORDINATOR_ORDERS_URL", "")

	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		camp := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/fuzz/campaigns/"), "/escrow")
		switch camp {
		case "camp-hub-funded":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "escrow": chain.FuzzEscrowRow{CampaignID: camp, BudgetHMC: 5, BudgetRuns: 16, Status: "open"}})
		case "camp-dust":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "escrow": chain.FuzzEscrowRow{CampaignID: camp, BudgetHMC: 0.001, BudgetRuns: 1, Status: "open"}})
		case "camp-few-runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "escrow": chain.FuzzEscrowRow{CampaignID: camp, BudgetHMC: 5, BudgetRuns: 4, Status: "open"}})
		case "camp-closed":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "escrow": chain.FuzzEscrowRow{CampaignID: camp, BudgetHMC: 5, BudgetRuns: 16, Status: "closed"}})
		default:
			http.Error(w, `{"ok":false,"error":"fuzz escrow not found"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(hub.Close)

	db, err := store.Open(filepath.Join(t.TempDir(), "admin-reg.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	wm := &workManager{
		ordersProbeURL: hub.URL, hybridSignerEnabled: true, claimRequirePubKey: true, claimPerMin: 60, submitPerMin: 60,
		abuse: make(map[string]workerAbuseState), ipAbuse: make(map[string]workerAbuseState),
		worker: map[string]workerPayoutStat{}, dropReasonCount: make(map[string]uint64),
	}
	mux := http.NewServeMux()
	addFuzzPoolRoutes(mux, "admin-tok", "worker-tok", "publish-tok", false, wm, &poolfuzz.Service{DB: db})

	register := func(id string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]any{
			"id": id, "campaign_type": "hunt", "budget_runs": 16, "budget_hmc": 5.0,
			"config": map[string]any{"campaign_type": "hunt", "work_kind": "hunt_shard", "upstream_target_id": "jsmn"},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/campaigns", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hackme-Admin-Token", "admin-tok")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	if rec := register("camp-escrow-elsewhere"); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "hub_escrow_required") {
		t.Fatalf("no hub escrow: want 409 hub_escrow_required, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := register("camp-closed"); rec.Code != http.StatusConflict {
		t.Fatalf("closed hub escrow: want 409, got %d %s", rec.Code, rec.Body.String())
	}
	// Escrow open but smaller than the registration (budget or runs): refused.
	for _, id := range []string{"camp-dust", "camp-few-runs"} {
		if rec := register(id); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "covers") {
			t.Fatalf("%s: undercollateralized registration must be 409, got %d %s", id, rec.Code, rec.Body.String())
		}
	}
	if rec := register("camp-hub-funded"); rec.Code == http.StatusConflict {
		t.Fatalf("open hub escrow must register, got 409 %s", rec.Body.String())
	}

	// Not a minting hub (lab / local Orders node): admin registration is unchanged.
	t.Setenv("HACKME_CHAIN_MINTING_HUB", "")
	t.Setenv("HACKME_ORDERS_PREPAID_HUB_SECRET", "")
	if rec := register("camp-local-lab"); rec.Code == http.StatusConflict {
		t.Fatalf("non-hub coordinator must not require a hub escrow, got 409 %s", rec.Body.String())
	}
}
