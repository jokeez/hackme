package workersettle

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Two settlers with different ledgers (main + named-fleet STATE_FILE) but the same payer
// must exclude each other: they share one nonce sequence on chain.
func TestPayerLockSpansLedgers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKME_SETTLE_PAYER_LOCK_WAIT_SEC", "0")
	ctx := context.Background()
	t.Setenv("HACKME_SETTLE_PAYER_LOCK_DIR", filepath.Join(dir, "locks"))
	// Ledgers in different directories must still share the host-wide lock.
	main := Config{StateDB: filepath.Join(dir, "a", "worker_settlement.db"), StateJSON: filepath.Join(dir, "worker_settlement_state.json")}
	named := Config{StateDB: filepath.Join(dir, "b", "worker_settlement_state.named-fleet.db"), StateJSON: filepath.Join(dir, "worker_settlement_state.named-fleet.json")}
	payer := "HMC-dfc143732a1fc3bc"
	if payerLockPath(main, payer, "hmc") != payerLockPath(named, payer, "hmc") {
		t.Fatalf("lock paths differ: %s vs %s", payerLockPath(main, payer, "hmc"), payerLockPath(named, payer, "hmc"))
	}
	unlock, held, err := lockPayer(ctx, main, payer, "hmc")
	if err != nil || !held {
		t.Fatalf("first lock: held=%v err=%v", held, err)
	}
	if _, held2, err := lockPayer(ctx, named, payer, "hmc"); err != nil || held2 {
		t.Fatalf("second settler on same payer got the lock: held=%v err=%v", held2, err)
	}
	if _, heldOther, err := lockPayer(ctx, named, "HMC-0000000000000001", "hmc"); err != nil || !heldOther {
		t.Fatalf("different payer must not be blocked: held=%v err=%v", heldOther, err)
	}
	// SUP has its own nonce column: its lane must not wait on an HMC run.
	uSup, heldSup, err := lockPayer(ctx, named, payer, "sup")
	if err != nil || !heldSup {
		t.Fatalf("sup lane blocked by hmc lane: held=%v err=%v", heldSup, err)
	}
	uSup()
	unlock()
	u3, held3, err := lockPayer(ctx, named, payer, "hmc")
	if err != nil || !held3 {
		t.Fatalf("lock after release: held=%v err=%v", held3, err)
	}
	u3()
	st, err := os.Stat(payerLockPath(main, payer, "hmc"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o066 != 0o066 {
		t.Fatalf("payer lock perms %o: a root-run settler would lock out the hackme unit", st.Mode().Perm())
	}
}

func TestPayerLockRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKME_SETTLE_PAYER_LOCK_DIR", dir)
	target := filepath.Join(dir, "secret")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{StateDB: filepath.Join(dir, "w.db")}
	if err := os.Symlink(target, payerLockPath(cfg, "HMC-aa", "hmc")); err != nil {
		t.Fatal(err)
	}
	if _, held, err := lockPayer(context.Background(), cfg, "HMC-aa", "hmc"); err == nil || held {
		t.Fatalf("symlinked lock accepted: held=%v err=%v", held, err)
	}
	if st, _ := os.Stat(target); st.Mode().Perm() != 0o600 {
		t.Fatalf("symlink target mode changed to %o", st.Mode().Perm())
	}
}

func TestPayerLockHonorsContext(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKME_SETTLE_PAYER_LOCK_DIR", dir)
	t.Setenv("HACKME_SETTLE_PAYER_LOCK_WAIT_SEC", "600")
	cfg := Config{StateDB: filepath.Join(dir, "w.db")}
	u, held, err := lockPayer(context.Background(), cfg, "HMC-bb", "hmc")
	if err != nil || !held {
		t.Fatal(held, err)
	}
	defer u()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, held, err := lockPayer(ctx, cfg, "HMC-bb", "hmc"); err == nil || held {
		t.Fatalf("cancelled ctx kept waiting/held: held=%v err=%v", held, err)
	}
}
