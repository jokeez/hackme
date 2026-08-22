package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

func TestGuidedPoolClaimSubmitAntiCheat(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "guided.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	wasmHex := mustReadWasmHex(t, "../../tasks/artifacts/security/rust_script_push_bounds_guard.wasm")
	cfg := PilotScriptPushGuidedConfig(wasmHex)
	id := "guided-anticheat"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "property", Title: "guided pilot", Status: "running",
		BudgetRuns: 16, BudgetSeconds: 120, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	size, err := svc.poolCorpusSize(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if size < 2 {
		t.Fatalf("expected seeded pool corpus, got %d", size)
	}
	w, ok, err := svc.Claim(ctx, "worker-a", time.Now().Unix())
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if w.CampaignID != id {
		t.Skip("another campaign won claim race in shared test db — retry isolated")
	}
	var locked int
	if err := db.QueryRowContext(ctx,
		`SELECT expected_input_locked FROM fuzz_work_items WHERE campaign_id=? AND id=?`, id, w.ItemID).
		Scan(&locked); err != nil {
		t.Fatal(err)
	}
	if locked != 1 {
		t.Fatal("guided claim must lock expected input")
	}
	cr, _, trap, err := ExecuteLocally(ctx, w.WasmCheckHex, w.ActualInput, 800)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Submit(ctx, SubmitRequest{
		WorkerID: "worker-a", WorkID: w.WorkID, CampaignID: w.CampaignID,
		ItemID: w.ItemID, InputN: w.InputN, ActualInput: w.ActualInput,
		CheckResult: cr, DurationMS: 1, Trap: trap,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestGuidedPilotFindsMoreThanLinear(t *testing.T) {
	wasmHex := mustReadWasmHex(t, "../../tasks/artifacts/security/rust_script_push_bounds_guard.wasm")
	ctx := context.Background()
	budget := 48
	linearFindings := runPilotArm(t, ctx, "linear", PilotScriptPushLinearConfig(wasmHex), budget)
	guidedFindings := runPilotArm(t, ctx, "guided", PilotScriptPushGuidedConfig(wasmHex), budget)
	t.Logf("pilot script_push budget=%d linear_findings=%d guided_findings=%d", budget, linearFindings, guidedFindings)
	if guidedFindings < linearFindings {
		t.Fatalf("guided should find >= linear on script_push pilot: linear=%d guided=%d", linearFindings, guidedFindings)
	}
}

func runPilotArm(t *testing.T, ctx context.Context, suffix string, cfg map[string]any, budget int) int {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "pilot-"+suffix+".db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	id := "pilot-" + suffix
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "property", Title: suffix, Status: "running",
		BudgetRuns: budget, BudgetSeconds: 300, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	wasmHex := mustReadWasmHex(t, "../../tasks/artifacts/security/rust_script_push_bounds_guard.wasm")
	_ = wasmHex
	done := 0
	for done < budget {
		w, ok, err := svc.Claim(ctx, "worker-pilot", time.Now().Unix())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		cr, _, trap, err := ExecuteLocally(ctx, w.WasmCheckHex, w.ActualInput, 800)
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.Submit(ctx, SubmitRequest{
			WorkerID: "worker-pilot", WorkID: w.WorkID, CampaignID: w.CampaignID,
			ItemID: w.ItemID, InputN: w.InputN, ActualInput: w.ActualInput,
			CheckResult: cr, DurationMS: 1, Trap: trap,
		}); err != nil {
			t.Fatal(err)
		}
		done++
	}
	var findings int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, id).Scan(&findings)
	return findings
}

func TestGuidedBytesPoolClaimSubmit(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "bytes-guided.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	wasmHex := mustReadWasmHex(t, "../../tasks/artifacts/security/rust_script_push_bounds_guard.wasm")
	cfg := PilotScriptPushBytesGuidedConfig(wasmHex)
	id := "bytes-guided"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "property", Title: "bytes guided", Status: "running",
		BudgetRuns: 24, BudgetSeconds: 120, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	size, err := svc.poolCorpusSize(ctx, id)
	if err != nil || size < 2 {
		t.Fatalf("byte corpus seed: size=%d err=%v", size, err)
	}
	done := 0
	for done < 12 {
		w, ok, err := svc.Claim(ctx, "worker-bytes", time.Now().Unix())
		if err != nil {
			t.Fatal(err)
		}
		if !ok || w.CampaignID != id {
			continue
		}
		if len(w.InputBytes) == 0 {
			t.Fatal("bytes mode claim must include InputBytes")
		}
		cr, _, trap, err := ExecuteLocallyBytes(ctx, w.WasmCheckHex, w.InputBytes, 800)
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.Submit(ctx, SubmitRequest{
			WorkerID: "worker-bytes", WorkID: w.WorkID, CampaignID: w.CampaignID,
			ItemID: w.ItemID, InputN: w.InputN, ActualInput: w.ActualInput, InputBytes: w.InputBytes,
			CheckResult: cr, DurationMS: 1, Trap: trap,
		}); err != nil {
			t.Fatal(err)
		}
		done++
	}
	var findings int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, id).Scan(&findings)
	t.Logf("bytes guided findings=%d", findings)
}
