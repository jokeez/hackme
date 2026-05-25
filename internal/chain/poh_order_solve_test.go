package chain

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"hackme/internal/sandbox"
	"hackme/internal/store"
)

func TestSubmitOrderPoHSolveCreditsMinerNotPrimary(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "order_solve.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	payer := "HMC-payerwallet"
	solver := "HMC-poolworker1"
	if _, _, err := svc.InitGenesis(ctx, payer); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, payer, 50.0)
	manifest, _ := json.Marshal(map[string]any{
		"id":             "ord-solve-1",
		"kind":           "synthetic_poh_v1",
		"artifact_hash":  "ab",
		"wasm_check_hex": sandbox.MinimalGateWasmHex,
		"reward_hmc":     0.05,
		"target_solves":  1,
		"timeout_ms":     5000,
		"payer_ref":      "test",
	})
	if _, err := svc.InsertOrderTask(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	m, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var n, ev uint64
	for x := uint64(0); x < 5_000_000; x++ {
		ev = PohEval(x)
		if m > 0 && ev%m == 0 {
			ok, err := sandbox.InvokeCheck(ctx, mustWasm(t, manifest), x)
			if err != nil {
				t.Fatal(err)
			}
			if ok {
				n = x
				break
			}
		}
	}
	if n == 0 {
		t.Fatal("no wasm-passing poh hit")
	}
	b, err := svc.SubmitOrderPoHSolve(ctx, solver, n, m, "ord-solve-1")
	if err != nil {
		t.Fatal(err)
	}
	if b == nil {
		t.Fatal("nil block")
	}
	var payload map[string]any
	_ = json.Unmarshal(b.Task.Payload, &payload)
	if payload["order_task_id"] != "ord-solve-1" {
		t.Fatalf("order_task_id in payload: %v", payload["order_task_id"])
	}
	var solverBal uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address = ?`, solver).Scan(&solverBal); err != nil {
		t.Fatal(err)
	}
	if solverBal == 0 {
		t.Fatal("expected solver account credited from order escrow")
	}
	var payerBal uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address = ?`, payer).Scan(&payerBal); err != nil {
		t.Fatal(err)
	}
	// Payer funded escrow; solver received per-solve reward (not full prepaid back to payer wallet).
	if solverBal < HMCToUnits(0.04) {
		t.Fatalf("solver balance low: %d", solverBal)
	}
}

func mustWasm(t *testing.T, manifest []byte) []byte {
	t.Helper()
	wb, err := ResolveWasmCheckFromManifest(manifest, DefaultArtifactRoot())
	if err != nil {
		t.Fatal(err)
	}
	return wb
}
