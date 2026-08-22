package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

func TestLocalDrainCampaignExceedsQueueDepth(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "drain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	wasmHex := mustReadWasmHex(t, "../../tasks/artifacts/security/rust_script_push_bounds_guard.wasm")
	cfg := PilotScriptPushGuidedConfig(wasmHex)
	const budget = 200 // default queue_depth tops up in batches (128)
	id := "local-drain-queue"
	svc := &Service{DB: db}
	ctx := context.Background()
	now := time.Now().Unix()
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "property", Title: "drain test", Status: "running",
		BudgetRuns: budget, BudgetSeconds: 600, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}

	runOne := func(wid string, w ClaimedWork) error {
		cr, _, trap, err := ExecuteLocally(ctx, w.WasmCheckHex, w.ActualInput, 800)
		if err != nil {
			return err
		}
		return svc.Submit(ctx, SubmitRequest{
			WorkerID: wid, WorkID: w.WorkID, CampaignID: w.CampaignID,
			ItemID: w.ItemID, InputN: w.InputN, ActualInput: w.ActualInput,
			CheckResult: cr, DurationMS: 1, Trap: trap,
		})
	}
	done, err := svc.LocalDrainCampaign(ctx, id, budget, []string{"drain-worker"}, now, runOne)
	if err != nil {
		t.Fatal(err)
	}
	if done != budget {
		t.Fatalf("expected %d runs, got %d", budget, done)
	}
}
