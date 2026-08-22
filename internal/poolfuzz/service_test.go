package poolfuzz

import (
	"context"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/sandbox"
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
		"seed_corpus":      []any{violation},
		"mutation_rounds":  0,
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
	inN, actual := actualInputForWorkItem(t, ctx, svc, id, 1, cfg)
	wasmHex := mustReadWasmHex(t, "../../tasks/artifacts/security/rust_script_push_bounds_guard.wasm")
	cr, _, trap, err := ExecuteLocally(ctx, wasmHex, actual, 800)
	if err != nil {
		t.Fatal(err)
	}
	if cr != 1 {
		t.Fatalf("expected violation check_ret=1, got %d trap=%q", cr, trap)
	}
	leaseWorkItemForTest(t, ctx, db, id, 1, "worker-test")
	if err := svc.Submit(ctx, SubmitRequest{
		WorkerID: "worker-test", WorkID: id + ":1", CampaignID: id,
		ItemID: 1, InputN: inN, ActualInput: actual, CheckResult: cr, DurationMS: 1, Trap: trap,
	}); err != nil {
		t.Fatal(err)
	}
	var findings int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, id).Scan(&findings)
	if findings < 1 {
		t.Fatalf("expected detector findings, got %d", findings)
	}
	var repro, artifact string
	if err := db.QueryRowContext(ctx,
		`SELECT repro_cmd, artifact_path FROM fuzz_findings WHERE campaign_id=? LIMIT 1`, id).
		Scan(&repro, &artifact); err != nil {
		t.Fatal(err)
	}
	if repro == "" || !strings.Contains(repro, "check_repro") {
		t.Fatalf("expected repro_cmd with check_repro, got %q", repro)
	}
	if artifact == "" {
		t.Fatal("expected artifact_path on pool finding")
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

func TestSubmitRejectsPendingWithoutLease(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "h03.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"check_semantics":  "pow_gate",
		"wasm_check_hex":   sandbox.MinimalGateWasmHex,
	}, "property")
	id := "h03-pending"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureWorkItems(ctx, id, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	inN, actual := actualInputForWorkItem(t, ctx, svc, id, 1, cfg)
	err = svc.Submit(ctx, SubmitRequest{
		WorkerID: "attacker", MinerAddress: "HMC-aaaaaaaaaaaaaaaa",
		CampaignID: id, ItemID: 1, InputN: inN, ActualInput: actual, CheckResult: 1, DurationMS: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "not leased") {
		t.Fatalf("want not-leased error, got %v", err)
	}
	var st string
	_ = db.QueryRowContext(ctx, `SELECT status FROM fuzz_work_items WHERE campaign_id=? AND id=1`, id).Scan(&st)
	if st != "pending" {
		t.Fatalf("pending must stay pending, got %q", st)
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

func leaseWorkItemForTest(t *testing.T, ctx context.Context, db *sql.DB, campaignID string, itemID int64, workerID string) {
	t.Helper()
	now := time.Now().Unix()
	res, err := db.ExecContext(ctx,
		`UPDATE fuzz_work_items SET status='leased', lease_owner=?, lease_until=?, updated_at=?
		 WHERE campaign_id=? AND id=? AND status IN ('pending','leased')`,
		workerID, now+120, now, campaignID, itemID)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		t.Fatalf("leaseWorkItemForTest: rows=%d", n)
	}
}

func actualInputForWorkItem(t *testing.T, ctx context.Context, svc *Service, campaignID string, itemID int64, cfg map[string]any) (inputN, actual uint64) {
	t.Helper()
	if err := svc.DB.QueryRowContext(ctx,
		`SELECT input_n FROM fuzz_work_items WHERE campaign_id=? AND id=?`, campaignID, itemID).Scan(&inputN); err != nil {
		t.Fatal(err)
	}
	actual, _ = derivePoolInputs(inputN, cfg)
	return inputN, actual
}
