package hms

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Regression (fixed): epochPrepaidUnits must count each paid order once — in the first epoch
// that holds any of its chunks (MIN(epoch_id)). Counting per-epoch with a chunk let one
// 1-byte upload per epoch re-emit prepaid as HMS forever.
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
	// Pilot path stamps payment_id=pilot-*; rebind to a non-pilot id so this test still
	// exercises the paid-order once-per-first-epoch counting rule (not the pilot filter).
	if _, err := db.Exec(`UPDATE hms_orders SET payment_id=? WHERE order_id=?`, "paid-audit9-"+created.Order.OrderID, created.Order.OrderID); err != nil {
		t.Fatal(err)
	}
	chunkA := bytes.Repeat([]byte("a"), minMarketChunkBytes)
	chunkB := bytes.Repeat([]byte("b"), minMarketChunkBytes)
	if _, err := coord.UploadOrderChunk(created.Order.OrderID, created.UploadToken, 0, chunkA); err != nil {
		t.Fatal(err)
	}
	ep1, _ := coord.CurrentEpoch()
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO hms_epochs(epoch_id, started_unix, freeze_unix, seal_end_unix, seal_target, sealed, payouts_enabled)
		VALUES(?, ?, ?, ?, ?, 0, 0)`, ep1.EpochID+1, now, now+3000, now+3600, defaultSealTarget()); err != nil {
		t.Fatal(err)
	}
	if _, err := coord.UploadOrderChunk(created.Order.OrderID, created.UploadToken, 1, chunkB); err != nil {
		t.Fatal(err)
	}
	u1, _ := coord.epochPrepaidUnits(ep1.EpochID)
	u2, _ := coord.epochPrepaidUnits(ep1.EpochID + 1)
	prepaid := uint64(created.Order.PrepaidHMC*1e8 + 0.5)
	if u1+u2 > prepaid {
		t.Fatalf("FINDING: one order's prepaid %d counted %d+%d=%d across two epochs (seal bonus paid twice)", prepaid, u1, u2, u1+u2)
	}
	if u1 != prepaid {
		t.Fatalf("first epoch prepaid=%d want %d", u1, prepaid)
	}
}

// Pilot CreateStorageOrder stamps payment_id=pilot-<hex> under insecure skip. Those rows
// must not inflate epochPrepaidUnits / seal bonus (audit M2 — no chain debit).
func TestPilotOrderDoesNotInflateEpochSealBudget(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "hms-pilot.db"))
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

	created, err := coord.CreateStorageOrder("pilot-test", "client-1", 10<<30, 30, "", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if created.Order.PaymentID == "" || len(created.Order.PaymentID) < 6 || created.Order.PaymentID[:6] != "pilot-" {
		t.Fatalf("expected pilot- payment_id, got %q", created.Order.PaymentID)
	}
	if _, err := coord.UploadOrderChunk(created.Order.OrderID, created.UploadToken, 0, bytes.Repeat([]byte("d"), minMarketChunkBytes)); err != nil {
		t.Fatal(err)
	}
	ep, err := coord.CurrentEpoch()
	if err != nil {
		t.Fatal(err)
	}
	prepaidUnits, err := coord.epochPrepaidUnits(ep.EpochID)
	if err != nil {
		t.Fatal(err)
	}
	if prepaidUnits != 0 {
		t.Fatalf("pilot order must not inflate epochPrepaidUnits, got %d (payment_id=%q prepaid_hmc=%v)",
			prepaidUnits, created.Order.PaymentID, created.Order.PrepaidHMC)
	}
}
