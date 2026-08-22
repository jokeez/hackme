package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

// TestUniqueFindingsPlateau documents diminishing returns: more runs ≠ linear unique growth.
func TestUniqueFindingsPlateau(t *testing.T) {
	wasmHex := mustReadWasmHex(t, "../../tasks/artifacts/security/rust_script_push_bounds_guard.wasm")
	ctx := context.Background()

	countAt := func(budget int) int {
		dir := t.TempDir()
		db, err := store.Open(filepath.Join(dir, "plateau.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		id := fmtCampaign("plateau", budget)
		cfg := PilotScriptPushGuidedConfig(wasmHex)
		svc := &Service{DB: db}
		if err := svc.RegisterCampaign(ctx, Campaign{
			ID: id, CampaignType: "property", Title: "plateau", Status: "running",
			BudgetRuns: budget, BudgetSeconds: 600, Config: cfg,
		}); err != nil {
			t.Fatal(err)
		}
		now := time.Now().Unix()
		done, err := svc.LocalDrainCampaign(ctx, id, budget, []string{"plateau-w"}, now, func(wid string, w ClaimedWork) error {
			cr, _, trap, err := ExecuteLocally(ctx, w.WasmCheckHex, w.ActualInput, 800)
			if err != nil {
				return err
			}
			return svc.Submit(ctx, SubmitRequest{
				WorkerID: wid, WorkID: w.WorkID, CampaignID: w.CampaignID,
				ItemID: w.ItemID, InputN: w.InputN, ActualInput: w.ActualInput,
				CheckResult: cr, DurationMS: 1, Trap: trap,
			})
		})
		if err != nil {
			t.Fatal(err)
		}
		if done != budget {
			t.Fatalf("budget=%d done=%d", budget, done)
		}
		var n int
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, id).Scan(&n)
		return n
	}

	f64 := countAt(64)
	f256 := countAt(256)
	t.Logf("guided script_push findings: 64→%d  256→%d", f64, f256)
	if f256 < f64 {
		t.Fatalf("more runs should not lose findings: 64=%d 256=%d", f64, f256)
	}
	// Diminishing returns: 4× runs should not yield 4× findings for this guard.
	if f256 > f64*4 && f64 > 0 {
		t.Fatalf("unexpected linear explosion: 64=%d 256=%d (want diminishing returns)", f64, f256)
	}
}

func fmtCampaign(prefix string, n int) string {
	return prefix + "-" + time.Now().Format("150405") + "-" + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
