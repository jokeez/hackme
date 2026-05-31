package hms

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFinalizeEpochSealPayoutsIdempotent(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "hms.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := Config{
		PoolID:            "hackme-official",
		EpochDuration:     time.Hour,
		FreezeAfter:       time.Minute,
		SealWindow:        10 * time.Minute,
		InitialSealTarget: defaultSealTarget(),
	}
	coord := NewCoordinator(db, cfg)

	now := time.Now().Unix()
	_, err = db.Exec(`INSERT INTO hms_epochs(epoch_id, started_unix, freeze_unix, seal_end_unix, manifest_root, seal_target, sealed, seal_worker_id, seal_nonce, payouts_enabled, payouts_finalized)
		VALUES(1, ?, ?, ?, ?, ?, 1, 'winner', 42, 1, 0)`,
		now-120, now-60, now+600, []byte{1, 2, 3}, defaultSealTarget())
	if err != nil {
		t.Fatal(err)
	}
	for wid, n := range map[string]uint64{"winner": 100, "peer-a": 200, "peer-b": 100} {
		if err := coord.RecordSealShare(1, wid, true); err != nil {
			t.Fatal(err)
		}
		for i := uint64(1); i < n; i++ {
			if err := coord.RecordSealShare(1, wid, true); err != nil {
				t.Fatal(err)
			}
		}
	}

	first, err := coord.FinalizeEpochSealPayouts(1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := coord.FinalizeEpochSealPayouts(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(second) {
		t.Fatalf("idempotent length mismatch: %d vs %d", len(first), len(second))
	}
	sum := uint64(0)
	for _, line := range second {
		sum += line.TotalUnits
	}
	if sum != SealEpochBudgetUnits(0) {
		t.Fatalf("sum=%d budget=%d", sum, SealEpochBudgetUnits(0))
	}

	settle, err := coord.EpochSealSettlement(1)
	if err != nil {
		t.Fatal(err)
	}
	if settle["payouts_finalized"] != true {
		t.Fatalf("settlement=%+v", settle)
	}
	payouts, ok := settle["payouts"].([]map[string]any)
	if !ok || len(payouts) != 3 {
		t.Fatalf("payouts=%+v", settle["payouts"])
	}
}

func TestSealHybridPayoutIntegration(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "hms.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := Config{
		PoolID:            "hackme-official",
		MinQuotaGB:        10,
		MaxQuotaGB:        1000,
		EpochDuration:     time.Hour,
		FreezeAfter:       50 * time.Minute,
		SealWindow:        10 * time.Minute,
		InitialSealTarget: defaultSealTarget(),
	}
	coord := NewCoordinator(db, cfg)

	var root [32]byte
	root[31] = 1
	target := defaultSealTarget()
	var nonce uint64
	for n := uint64(0); n < 500000; n++ {
		h := SealHash(1, root, cfg.PoolID, n)
		if HashBelowTarget(h[:], target) {
			nonce = n
			break
		}
	}
	if nonce == 0 {
		t.Skip("no nonce in 500k")
	}

	now := time.Now().Unix()
	_, err = db.Exec(`INSERT INTO hms_epochs(epoch_id, started_unix, freeze_unix, seal_end_unix, manifest_root, seal_target, sealed, payouts_enabled)
		VALUES(1, ?, ?, ?, ?, ?, 0, 0)`, now-120, now-60, now+600, root[:], target)
	if err != nil {
		t.Fatal(err)
	}
	for wid, n := range map[string]uint64{"asic-1": 50, "asic-2": 150, "asic-3": 100} {
		for i := uint64(0); i < n; i++ {
			if err := coord.RecordSealShare(1, wid, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := coord.RegisterSealWorker("asic-1", strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	if err := coord.submitSealCore(SealSubmitPayload{WorkerID: "asic-1", EpochID: 1, Nonce: nonce}); err != nil {
		t.Fatal(err)
	}

	lines, err := coord.FinalizeEpochSealPayouts(1)
	if err != nil {
		t.Fatal(err)
	}
	var sum uint64
	for _, line := range lines {
		sum += line.TotalUnits
	}
	if sum != SealEpochBudgetUnits(0) {
		t.Fatalf("sum=%d budget=%d lines=%+v", sum, SealEpochBudgetUnits(0), lines)
	}
	var winnerTotal uint64
	for _, line := range lines {
		if line.WorkerID == "asic-1" {
			winnerTotal = line.TotalUnits
		}
	}
	if winnerTotal < 750_000 {
		t.Fatalf("winner total too low: %d", winnerTotal)
	}
}
