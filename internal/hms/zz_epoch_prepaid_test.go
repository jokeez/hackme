package hms

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A3: epochPrepaidUnits sums the FULL prepaid_hmc of every order that has at least one chunk
// first assigned in the epoch. An order whose chunks are spread over N epochs is counted N
// times, so its 1% seal-budget bonus (minted as HMS by settle_worker_hms.sh) is paid N times.
// A customer holding an upload token uploads one tiny chunk per epoch (≤1e6 chunk indexes,
// bounded only by size_plan_bytes, and the 5 GB price floor makes size_plan almost free).
func TestAudit9_EpochPrepaidCountedPerEpoch(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "hms.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := Config{MinQuotaGB: 10, MaxQuotaGB: 1000, EpochDuration: time.Hour, FreezeAfter: 50 * time.Minute, SealWindow: 10 * time.Minute, InitialSealTarget: defaultSealTarget()}
	coord := NewCoordinator(db, cfg)
	if err := coord.RegisterStorageWorker("w-market", repeatHex(64), 100); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HMS_MARKET_DATA_DIR", filepath.Join(dir, "market"))
	t.Setenv("HMS_MARKET_STORAGE_ROOT", filepath.Join(dir, "storage"))
	_ = os.MkdirAll(filepath.Join(dir, "storage", "w-market"), 0o755)
	t.Setenv("HMS_MARKET_SKIP_PAYMENT", "1")
	t.Setenv("HMS_COORDINATOR_ALLOW_INSECURE", "1")
	created, err := coord.CreateStorageOrder("b", "c", 5<<30, 30, "", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coord.UploadOrderChunk(created.Order.OrderID, created.UploadToken, 0, []byte("a")); err != nil {
		t.Fatal(err)
	}
	ep1, _ := coord.CurrentEpoch()
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO hms_epochs(epoch_id, started_unix, freeze_unix, seal_end_unix, seal_target, sealed, payouts_enabled)
		VALUES(?, ?, ?, ?, ?, 0, 0)`, ep1.EpochID+1, now, now+3000, now+3600, defaultSealTarget()); err != nil {
		t.Fatal(err)
	}
	if _, err := coord.UploadOrderChunk(created.Order.OrderID, created.UploadToken, 1, []byte("b")); err != nil {
		t.Fatal(err)
	}
	u1, _ := coord.epochPrepaidUnits(ep1.EpochID)
	u2, _ := coord.epochPrepaidUnits(ep1.EpochID + 1)
	prepaid := uint64(created.Order.PrepaidHMC*1e8 + 0.5)
	if u1+u2 > prepaid {
		t.Fatalf("FINDING: one order's prepaid %d counted %d+%d=%d across two epochs (seal bonus paid twice)", prepaid, u1, u2, u1+u2)
	}
}
