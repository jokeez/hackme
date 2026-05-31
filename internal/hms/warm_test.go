package hms

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStratumWorkWarmVsSeal(t *testing.T) {
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
	_, err = db.Exec(`INSERT INTO hms_epochs(epoch_id, started_unix, freeze_unix, seal_end_unix, manifest_root, seal_target, sealed, payouts_enabled)
		VALUES(1, ?, ?, ?, ?, ?, 0, 0)`, now-120, now+300, now+900, []byte{1}, defaultSealTarget())
	if err != nil {
		t.Fatal(err)
	}
	w, err := coord.StratumWork()
	if err != nil {
		t.Fatal(err)
	}
	if workModeFromPackage(w) != WorkModeWarm {
		t.Fatalf("ingest want warm got %+v", w)
	}
	_, err = db.Exec(`UPDATE hms_epochs SET freeze_unix=?, seal_end_unix=? WHERE epoch_id=1`, now-60, now+600)
	if err != nil {
		t.Fatal(err)
	}
	w, err = coord.StratumWork()
	if err != nil {
		t.Fatal(err)
	}
	if workModeFromPackage(w) != WorkModeSeal {
		t.Fatalf("seal window want seal got %+v", w)
	}
}

func TestWarmShareAccrual(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "hms.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	coord := NewCoordinator(db, Config{PoolID: "hackme-official", InitialSealTarget: warmTarget()})
	slot := currentWarmSlot(time.Now().Unix())
	var nonce uint64
	for n := uint64(0); n < 5000000; n++ {
		root := warmManifestRoot(slot)
		h := SealHash(slot, root, "hackme-official", n)
		if HashBelowTarget(h[:], warmTarget()) {
			nonce = n
			break
		}
	}
	if nonce == 0 {
		t.Fatal("no warm nonce")
	}
	if err := coord.SubmitWarmShare("asic-farm.rig1", slot, nonce); err != nil {
		t.Fatal(err)
	}
	u := coord.warmAccrualUnits("asic-farm.rig1")
	if u < WarmShareAccrualUnits {
		t.Fatalf("accrual=%d want >= %d", u, WarmShareAccrualUnits)
	}
}
