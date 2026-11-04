package workersettle

import (
	"os"
	"path/filepath"
	"testing"
)

// Two settlers with different ledgers (main + named-fleet STATE_FILE) but the same payer
// must exclude each other: they share one nonce sequence on chain.
func TestPayerLockSpansLedgers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKME_SETTLE_PAYER_LOCK_WAIT_SEC", "0")
	main := Config{StateDB: filepath.Join(dir, "worker_settlement.db"), StateJSON: filepath.Join(dir, "worker_settlement_state.json")}
	named := Config{StateDB: filepath.Join(dir, "worker_settlement_state.named-fleet.db"), StateJSON: filepath.Join(dir, "worker_settlement_state.named-fleet.json")}
	payer := "HMC-dfc143732a1fc3bc"
	if payerLockPath(main, payer) != payerLockPath(named, payer) {
		t.Fatalf("lock paths differ: %s vs %s", payerLockPath(main, payer), payerLockPath(named, payer))
	}
	unlock, held, err := lockPayer(main, payer)
	if err != nil || !held {
		t.Fatalf("first lock: held=%v err=%v", held, err)
	}
	if _, held2, err := lockPayer(named, payer); err != nil || held2 {
		t.Fatalf("second settler on same payer got the lock: held=%v err=%v", held2, err)
	}
	if _, heldOther, err := lockPayer(named, "HMC-0000000000000001"); err != nil || !heldOther {
		t.Fatalf("different payer must not be blocked: held=%v err=%v", heldOther, err)
	}
	unlock()
	u3, held3, err := lockPayer(named, payer)
	if err != nil || !held3 {
		t.Fatalf("lock after release: held=%v err=%v", held3, err)
	}
	u3()
	st, err := os.Stat(payerLockPath(main, payer))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o066 != 0o066 {
		t.Fatalf("payer lock perms %o: a root-run settler would lock out the hackme unit", st.Mode().Perm())
	}
}
