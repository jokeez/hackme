package chain

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"hackme/internal/store"
)

func firstBaselineHit(t *testing.T, svc *Service, miner string) (nonce, eval, mod uint64, tip string) {
	t.Helper()
	ctx := context.Background()
	mod, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var tipHash string
	if err := svc.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'tip_hash'`).Scan(&tipHash); err != nil {
		t.Fatal(err)
	}
	var maxIdx int64
	if err := svc.db.QueryRowContext(ctx, `SELECT MAX(block_index) FROM blocks`).Scan(&maxIdx); err != nil {
		t.Fatal(err)
	}
	nextIdx := uint64(maxIdx) + 1
	for n := uint64(0); n < 80_000_000; n++ {
		ev := PohWorkForIndex(nextIdx, n, tipHash, miner)
		if mod > 0 && ev%mod == 0 {
			return n, ev, mod, tipHash
		}
	}
	t.Fatal("no baseline poh hit")
	return 0, 0, 0, ""
}

func TestSubmitBaselinePoHSolveCreditsPrimaryNotMiner(t *testing.T) {
	// A6: baseline subsidy → primary wallet, not miner_address.
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "baseline.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	primary := "HMC-primarywal01"
	miner := "HMC-poolworker99"
	if _, _, err := svc.InitGenesis(ctx, primary); err != nil {
		t.Fatal(err)
	}
	var primaryBefore uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address = ?`, primary).Scan(&primaryBefore); err != nil {
		t.Fatal(err)
	}
	n, _, mod, _ := firstBaselineHit(t, svc, miner)
	b, err := svc.SubmitBaselinePoHSolve(ctx, miner, n, mod)
	if err != nil {
		t.Fatal(err)
	}
	if b == nil || b.MinerAddress != miner {
		t.Fatalf("block miner=%v", b)
	}
	var primaryAfter uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address = ?`, primary).Scan(&primaryAfter); err != nil {
		t.Fatal(err)
	}
	if primaryAfter <= primaryBefore {
		t.Fatalf("primary did not grow: before=%d after=%d", primaryBefore, primaryAfter)
	}
	var minerBal uint64
	_ = db.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address = ?`, miner).Scan(&minerBal)
	if minerBal != 0 {
		t.Fatalf("miner must not receive baseline subsidy, got %d", minerBal)
	}
}

func TestSubmitBaselinePoHSolveRejectsBadNonce(t *testing.T) {
	// A2
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "baseline_bad.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	primary := "HMC-primarywal02"
	if _, _, err := svc.InitGenesis(ctx, primary); err != nil {
		t.Fatal(err)
	}
	mod, _ := svc.PoHTargetMod(ctx)
	_, err = svc.SubmitBaselinePoHSolve(ctx, "HMC-poolworker99", 1, mod) // almost never a hit
	if err == nil {
		// If 1 happened to hit, try a range
		for n := uint64(2); n < 1000; n++ {
			_, err = svc.SubmitBaselinePoHSolve(ctx, "HMC-poolworker99", n, mod)
			if err != nil {
				break
			}
		}
	}
	if err == nil {
		t.Fatal("expected invalid poh rejection")
	}
}

func TestSubmitBaselinePoHSolveRejectsBadMinerPrefix(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "baseline_addr.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	if _, _, err := svc.InitGenesis(ctx, "HMC-primarywal03"); err != nil {
		t.Fatal(err)
	}
	_, err = svc.SubmitBaselinePoHSolve(ctx, "not-an-address", 1, 100)
	if err == nil {
		t.Fatal("expected invalid miner_address")
	}
}

func TestSubmitBaselinePoHSolveReplayOnlyOneReward(t *testing.T) {
	// A3: second submit against new tip fails; primary +1× reward only.
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "baseline_replay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	primary := "HMC-primarywal04"
	miner := "HMC-poolworker88"
	if _, _, err := svc.InitGenesis(ctx, primary); err != nil {
		t.Fatal(err)
	}
	var before uint64
	_ = db.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address = ?`, primary).Scan(&before)
	n, _, mod, _ := firstBaselineHit(t, svc, miner)
	if _, err := svc.SubmitBaselinePoHSolve(ctx, miner, n, mod); err != nil {
		t.Fatal(err)
	}
	var mid uint64
	_ = db.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address = ?`, primary).Scan(&mid)
	reward := mid - before
	if reward == 0 {
		t.Fatal("expected reward")
	}
	_, err = svc.SubmitBaselinePoHSolve(ctx, miner, n, mod)
	if err == nil {
		t.Fatal("expected replay against new tip to fail")
	}
	var after uint64
	_ = db.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address = ?`, primary).Scan(&after)
	if after != mid {
		t.Fatalf("replay must not mint again: mid=%d after=%d", mid, after)
	}
}

func TestSubmitBaselinePoHSolveStaleTargetModForced(t *testing.T) {
	// A4: caller-supplied easy target_mod is ignored; wrong nonce for chain mod fails.
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "baseline_mod.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	primary := "HMC-primarywal05"
	miner := "HMC-poolworker77"
	if _, _, err := svc.InitGenesis(ctx, primary); err != nil {
		t.Fatal(err)
	}
	// Easy mod that would accept almost anything if trusted — must still force chain mod.
	_, err = svc.SubmitBaselinePoHSolve(ctx, miner, 12345, 2)
	if err == nil {
		// Unlucky hit under chain mod; try another nonce known-bad for chain
		_, err = svc.SubmitBaselinePoHSolve(ctx, miner, 12346, 2)
	}
	if err == nil {
		t.Fatal("expected reject when nonce invalid under forced chain mod")
	}
}

func TestSubmitBaselinePoHSolveConcurrentSingleWinner(t *testing.T) {
	// A13
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "baseline_race.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	primary := "HMC-primarywal06"
	miner := "HMC-poolworker66"
	if _, _, err := svc.InitGenesis(ctx, primary); err != nil {
		t.Fatal(err)
	}
	n, _, mod, _ := firstBaselineHit(t, svc, miner)
	var before uint64
	_ = db.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address = ?`, primary).Scan(&before)

	var okCount atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.SubmitBaselinePoHSolve(ctx, miner, n, mod); err == nil {
				okCount.Add(1)
			}
		}()
	}
	wg.Wait()
	if okCount.Load() != 1 {
		t.Fatalf("want exactly 1 successful mint, got %d", okCount.Load())
	}
	var after uint64
	_ = db.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address = ?`, primary).Scan(&after)
	delta := after - before
	want := HMCToUnits(InitialBaseRewardHMC)
	if delta != want {
		t.Fatalf("reward units=%d want %d (one block)", delta, want)
	}
}
