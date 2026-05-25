package chain

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/sandbox"
	"hackme/internal/store"
)

func preFundEscrow(t *testing.T, ctx context.Context, db *sql.DB, addr string, hmc float64) {
	t.Helper()
	units := HMCToUnits(hmc)
	if _, err := db.ExecContext(ctx, `UPDATE wallet SET balance_hmc = ?, balance_units = ? WHERE id = 1`, hmc, units); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE accounts SET balance_units = ? WHERE address = ?`, units, addr); err != nil {
		t.Fatal(err)
	}
	// Keep economic meta consistent with test pre-funding to satisfy invariants.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO meta(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = CASE
		   WHEN CAST(value AS REAL) < CAST(excluded.value AS REAL) THEN excluded.value
		   ELSE value END`,
		metaTotalMintedHMC, hmc); err != nil {
		t.Fatal(err)
	}
}

func TestInsertOrderTaskAndStoreTaskProvider(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := New(db)
	addr := "HMC-test"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	// Genesis mints to DevFeeAddress; pre-fund primary wallet+accounts for escrow tests.
	preFundEscrow(t, ctx, db, addr, 10.0)
	manifest := []byte(`{"id":"ord-1","kind":"synthetic_poh_v1","reward_hmc":0.03,"target_solves":2,"payer_ref":"audit:acme-9f2a"}`)
	res, err := svc.InsertOrderTask(ctx, manifest)
	if err != nil || res == nil || res.ID != "ord-1" {
		t.Fatalf("insert: res=%+v err=%v", res, err)
	}
	if res.PrepaidHMC != 0.06 {
		t.Fatalf("prepaid: %v", res.PrepaidHMC)
	}
	if res.OrderFeeHMC < 0.003-1e-9 || res.OrderFeeHMC > 0.003+1e-9 {
		t.Fatalf("order fee: %v", res.OrderFeeHMC)
	}
	if res.TotalDebitHMC < 0.063-1e-9 || res.TotalDebitHMC > 0.063+1e-9 {
		t.Fatalf("total debit: %v", res.TotalDebitHMC)
	}
	_, bal, _ := svc.Wallet(ctx)
	want := 10.0 - 0.063
	if bal < want-1e-6 || bal > want+1e-6 {
		t.Fatalf("balance after escrow want %v got %v", want, bal)
	}
	var devUnits uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address = ?`, DevFeeAddress).Scan(&devUnits); err != nil {
		t.Fatalf("dev fee account missing: %v", err)
	}
	if devUnits == 0 {
		t.Fatal("expected non-zero dev fee balance")
	}
	ec, err := svc.Economics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ec.TotalBurned < 0.006-1e-9 || ec.TotalBurned > 0.006+1e-9 {
		t.Fatalf("burned want 0.006 got %v", ec.TotalBurned)
	}
	var mintedBefore, escrowBefore uint64
	if err := db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, metaTotalMintedUnits).Scan(&mintedBefore); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, metaOrderEscrowUnits).Scan(&escrowBefore); err != nil {
		t.Fatal(err)
	}
	if escrowBefore == 0 {
		t.Fatal("expected non-zero escrow reserve after prepaid order")
	}

	prov := NewStoreTaskProvider(svc, InternalTaskProvider{})
	spec, err := prov.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if spec.ID != "ord-1" || spec.Source != TaskSourceOrder || spec.RewardHMC != 0.03 {
		t.Fatalf("snapshot spec: %+v", spec)
	}
	t.Setenv("HACKME_CHAIN_LEADER_ORDERS_VIA_POOL_ONLY", "1")
	specPool, err := prov.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if specPool.Source == TaskSourceOrder {
		t.Fatalf("pool-only leader must not load open orders locally: %+v", specPool)
	}
	t.Setenv("HACKME_CHAIN_LEADER_ORDERS_VIA_POOL_ONLY", "")
	m0, _ := svc.PoHTargetMod(ctx)
	n, ev := firstPoHHit(m0)
	if _, err := svc.AppendPoHBlock(ctx, addr, n, ev, 0.03, m0, "ord-1"); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.ListOrderTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ProgressCount != 1 || rows[0].Status != TaskStatusOpen {
		t.Fatalf("after one block: %+v", rows[0])
	}
	m1, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var n2, e2 uint64
	for x := n + 1; x < n+10_000_000; x++ {
		ev := PohEval(x)
		if m1 > 0 && ev%m1 == 0 {
			n2, e2 = x, ev
			break
		}
	}
	if n2 == 0 {
		t.Fatal("no second PoH hit")
	}
	if _, err := svc.AppendPoHBlock(ctx, addr, n2, e2, 0.03, m1, "ord-1"); err != nil {
		t.Fatal(err)
	}
	rows2, _ := svc.ListOrderTasks(ctx)
	if len(rows2) != 1 || rows2[0].ProgressCount != 2 || rows2[0].Status != TaskStatusCompleted {
		t.Fatalf("after two blocks: %+v", rows2[0])
	}
	var mintedAfter, escrowAfter uint64
	if err := db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, metaTotalMintedUnits).Scan(&mintedAfter); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, metaOrderEscrowUnits).Scan(&escrowAfter); err != nil {
		t.Fatal(err)
	}
	if mintedAfter != mintedBefore {
		t.Fatalf("order payouts must not mint new supply: before=%d after=%d", mintedBefore, mintedAfter)
	}
	if escrowAfter >= escrowBefore {
		t.Fatalf("escrow reserve must decrease after order payouts: before=%d after=%d", escrowBefore, escrowAfter)
	}

	spec2, err := prov.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if spec2.Source != "internal" {
		t.Fatalf("expected fallback internal after order done, got %+v", spec2)
	}
}

func TestInsertOrderTaskWasmArtifactPath(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	artRoot := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(artRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := hex.DecodeString(sandbox.MinimalGateWasmHex)
	if err != nil {
		t.Fatal(err)
	}
	wasmPath := filepath.Join(artRoot, "demo.wasm")
	if err := os.WriteFile(wasmPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_TASK_ARTIFACT_DIR", artRoot)

	db, err := store.Open(filepath.Join(dir, "ord.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := New(db)
	addr := "HMC-art"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	// Genesis mints to DevFeeAddress; pre-fund primary wallet+accounts for escrow tests.
	preFundEscrow(t, ctx, db, addr, 10.0)
	sum := sha256.Sum256(raw)
	h := hex.EncodeToString(sum[:])
	manifest := []byte(`{"id":"ord-wasm","kind":"synthetic_poh_v1","reward_hmc":0.02,"target_solves":1,"payer_ref":"t",
		"wasm_artifact_path":"demo.wasm","artifact_hash":"` + h + `"}`)
	res, err := svc.InsertOrderTask(ctx, manifest)
	if err != nil || res == nil || res.ID != "ord-wasm" {
		t.Fatalf("insert: %+v err=%v", res, err)
	}
	prov := NewStoreTaskProvider(svc, InternalTaskProvider{})
	spec, err := prov.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.WasmCheck) == 0 {
		t.Fatal("expected WasmCheck from file")
	}
}

func TestInsertOrderTaskDifficultyMinReward(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "diff.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := New(db)
	addr := "HMC-diff"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	// Genesis mints to DevFeeAddress; pre-fund primary wallet+accounts for escrow tests.
	preFundEscrow(t, ctx, db, addr, 10.0)
	manifestBad := []byte(`{"id":"ord-diff-bad","kind":"synthetic_poh_v1","reward_hmc":0.001,"difficulty_score":10,"target_solves":1}`)
	if _, err := svc.InsertOrderTask(ctx, manifestBad); err == nil {
		t.Fatal("expected min reward validation error")
	}
	manifestOK := []byte(`{"id":"ord-diff-ok","kind":"synthetic_poh_v1","reward_hmc":0.005,"difficulty_score":10,"target_solves":1}`)
	if _, err := svc.InsertOrderTask(ctx, manifestOK); err != nil {
		t.Fatalf("insert diff ok: %v", err)
	}
	rows, err := svc.ListOrderTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || rows[0].DifficultyScore != 10 {
		t.Fatalf("difficulty row: %+v", rows)
	}
}

func TestInsertOrderTaskRejectsWhenAccountUnitsInsufficient(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "units.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := New(db)
	addr := "HMC-units"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE wallet SET balance_hmc = ?, balance_units = ? WHERE id = 1`, 10.0, HMCToUnits(10.0)); err != nil {
		t.Fatal(err)
	}
	// Simulate stale mismatch: wallet says enough, account_units does not.
	if _, err := db.ExecContext(ctx, `UPDATE accounts SET balance_units = ? WHERE address = ?`, uint64(1000), addr); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"id":"ord-units-low","kind":"synthetic_poh_v1","reward_hmc":0.02,"target_solves":1}`)
	if _, err := svc.InsertOrderTask(ctx, manifest); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}
	var units uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address = ?`, addr).Scan(&units); err != nil {
		t.Fatal(err)
	}
	if units != 1000 {
		t.Fatalf("account units changed unexpectedly: %d", units)
	}
}

func TestInsertOrderTaskRejectsTooLargeTargetSolves(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "target_cap.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := New(db)
	addr := "HMC-target-cap"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, addr, 100.0)
	manifest := []byte(`{"id":"ord-target-cap","kind":"synthetic_poh_v1","reward_hmc":0.01,"target_solves":10001}`)
	if _, err := svc.InsertOrderTask(ctx, manifest); err == nil {
		t.Fatal("expected target_solves max validation error")
	}
}

func TestInsertOrderTaskEscrowHourlyCap(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "hour_cap.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	t.Setenv("HACKME_MAX_ORDER_ESCROW_PER_HOUR_HMC", "0.05")

	svc := New(db)
	addr := "HMC-hour-cap"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, addr, 10.0)
	okManifest := []byte(`{"id":"ord-hour-ok","kind":"synthetic_poh_v1","reward_hmc":0.03,"target_solves":1}`)
	if _, err := svc.InsertOrderTask(ctx, okManifest); err != nil {
		t.Fatalf("first order should pass: %v", err)
	}
	blockedManifest := []byte(`{"id":"ord-hour-block","kind":"synthetic_poh_v1","reward_hmc":0.03,"target_solves":1}`)
	if _, err := svc.InsertOrderTask(ctx, blockedManifest); !errors.Is(err, ErrOrderEscrowRateLimited) {
		t.Fatalf("expected ErrOrderEscrowRateLimited, got %v", err)
	}
}

func TestAppendPoHBlockRejectsOrderRewardMismatch(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "order_reward_mismatch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := New(db)
	addr := "HMC-order-reward-check"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, addr, 10.0)
	manifest := []byte(`{"id":"ord-reward-check","kind":"synthetic_poh_v1","reward_hmc":0.01,"target_solves":1}`)
	if _, err := svc.InsertOrderTask(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	m0, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n, ev := firstPoHHit(m0)
	if _, err := svc.AppendPoHBlock(ctx, addr, n, ev, 1.0, m0, "ord-reward-check"); err == nil {
		t.Fatal("expected order reward mismatch error")
	}
}

func TestOrderAutoExpireRefundsRemainingEscrow(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "order_expire_refund.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := New(db)
	addr := "HMC-expire"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, addr, 10.0)
	t.Setenv("HACKME_ORDER_MIN_TTL_SEC", "2")
	t.Setenv("HACKME_ORDER_MAX_TTL_SEC", "10")

	manifest := []byte(`{"id":"ord-expire","kind":"synthetic_poh_v1","reward_hmc":0.03,"target_solves":2,"ttl_sec":2}`)
	if _, err := svc.InsertOrderTask(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	_, balAfterInsert, err := svc.Wallet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if balAfterInsert < 9.9369 || balAfterInsert > 9.9371 {
		t.Fatalf("wallet after insert: %v", balAfterInsert)
	}

	m0, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n1, e1 := firstPoHHit(m0)
	if _, err := svc.AppendPoHBlock(ctx, addr, n1, e1, 0.03, m0, "ord-expire"); err != nil {
		t.Fatal(err)
	}
	_, balAfterOneSolve, err := svc.Wallet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if balAfterOneSolve < 9.9669 || balAfterOneSolve > 9.9671 {
		t.Fatalf("wallet after one solve: %v", balAfterOneSolve)
	}

	time.Sleep(2200 * time.Millisecond)
	rows, err := svc.ListOrderTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows len: %d", len(rows))
	}
	if rows[0].Status != TaskStatusCancelled {
		t.Fatalf("status after ttl: %q", rows[0].Status)
	}
	if rows[0].CancelReason != "timeout" {
		t.Fatalf("cancel reason: %q", rows[0].CancelReason)
	}
	if rows[0].RefundedHMC < 0.0299 || rows[0].RefundedHMC > 0.0301 {
		t.Fatalf("refunded hmc: %v", rows[0].RefundedHMC)
	}

	_, balAfterExpire, err := svc.Wallet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if balAfterExpire < 9.9969 || balAfterExpire > 9.9971 {
		t.Fatalf("wallet after expire refund: %v", balAfterExpire)
	}

	var escrowUnits uint64
	if err := db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, metaOrderEscrowUnits).Scan(&escrowUnits); err != nil {
		t.Fatal(err)
	}
	if escrowUnits != 0 {
		t.Fatalf("escrow units should be 0 after refund, got %d", escrowUnits)
	}
	prov := NewStoreTaskProvider(svc, InternalTaskProvider{})
	spec, err := prov.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Source != "internal" {
		t.Fatalf("expected fallback internal after timeout cancel, got %+v", spec)
	}
}

func TestExpiredOrderCannotBeMinedEvenBeforeListSweep(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "order_expire_guard.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := New(db)
	addr := "HMC-expire-guard"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, addr, 5.0)
	t.Setenv("HACKME_ORDER_MIN_TTL_SEC", "1")
	t.Setenv("HACKME_ORDER_MAX_TTL_SEC", "10")
	manifest := []byte(`{"id":"ord-expired-guard","kind":"synthetic_poh_v1","reward_hmc":0.01,"target_solves":1,"ttl_sec":1}`)
	if _, err := svc.InsertOrderTask(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1200 * time.Millisecond)

	m, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n, ev := firstPoHHit(m)
	if _, err := svc.AppendPoHBlock(ctx, addr, n, ev, 0.01, m, "ord-expired-guard"); err == nil {
		t.Fatal("expected expired order to be rejected")
	}
	rows, err := svc.ListOrderTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Status != TaskStatusCancelled {
		t.Fatalf("expected cancelled after append-triggered expire, got %+v", rows)
	}
}
