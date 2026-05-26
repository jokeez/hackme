package poolfuzz

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/store"
)

func TestPoolFuzzClaimSubmitDetector(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "co.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	violation := uint64(0x4c | (521 << 8))
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"check_semantics":  "detector",
		"wasm_check_hex":   mustReadWasmHex(t, "../../tasks/artifacts/security/rust_script_push_bounds_guard.wasm"),
		"seed_corpus":      []any{violation, uint64(0)},
		"mutation_rounds":  1,
	}, "property")
	id := "pool-test-detector"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "property", Title: "test", Status: "running",
		BudgetRuns: 4, BudgetSeconds: 60, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureWorkItems(ctx, id, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	wasmHex := mustReadWasmHex(t, "../../tasks/artifacts/security/rust_script_push_bounds_guard.wasm")
	cr, _, trap, err := ExecuteLocally(ctx, wasmHex, violation, 800)
	if err != nil {
		t.Fatal(err)
	}
	if cr != 1 {
		t.Fatalf("expected violation check_ret=1, got %d trap=%q", cr, trap)
	}
	if err := svc.Submit(ctx, SubmitRequest{
		WorkerID: "worker-test", WorkID: id + ":1", CampaignID: id,
		ItemID: 1, InputN: 1, ActualInput: violation, CheckResult: cr, DurationMS: 1, Trap: trap,
	}); err != nil {
		t.Fatal(err)
	}
	var findings int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, id).Scan(&findings)
	if findings < 1 {
		t.Fatalf("expected detector findings, got %d", findings)
	}
	// Claim/submit loop smoke
	done := 0
	for i := 0; i < 6 && done < 3; i++ {
		w, ok, err := svc.Claim(ctx, "worker-test", time.Now().Unix())
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
			WorkerID: "worker-test", WorkID: w.WorkID, CampaignID: w.CampaignID,
			ItemID: w.ItemID, InputN: w.InputN, ActualInput: w.ActualInput,
			CheckResult: cr, DurationMS: 1, Trap: trap,
		}); err != nil {
			t.Fatal(err)
		}
		done++
	}
}

func mustReadWasmHex(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}
