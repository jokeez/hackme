package workersettle

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// AZ #2: ClearPending must drop a stranded empty-hash HMC intent (FORCE path uses it).
func TestAZClearPendingForceClearsStrandedHMC(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "az-force.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	if _, err := s.SetPendingUnits(ctx, "w1", "HMC-aaaaaaaaaaaaaaaa", 40_000_000, 7); err != nil {
		t.Fatal(err)
	}
	row, err := s.GetWorker(ctx, "w1")
	if err != nil || strings.TrimSpace(row.PendingJSON) == "" {
		t.Fatalf("pending missing: %v %#v", err, row)
	}
	if err := s.ClearPending(ctx, "w1"); err != nil {
		t.Fatal(err)
	}
	row2, err := s.GetWorker(ctx, "w1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(row2.PendingJSON) != "" {
		t.Fatalf("force clear left pending=%q", row2.PendingJSON)
	}
}

// AZ #3 residual: SUPTxHashKnown must block attributing one hash to two workers.
func TestAZSUPTxHashKnownBlocksSharedAttach(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "az-shared.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	if err := s.SetPendingSUPUnits(ctx, "w-a", "HMC-sharedpay00001", 40_000_000, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSUPTxHash(ctx, "w-a", "shared-tx-hash"); err != nil {
		t.Fatal(err)
	}
	used, err := s.SUPTxHashKnown(ctx, "shared-tx-hash")
	if err != nil || !used {
		t.Fatalf("SUPTxHashKnown after attach: used=%v err=%v", used, err)
	}
	if err := s.SetPendingSUPUnits(ctx, "w-b", "HMC-sharedpay00001", 40_000_000, 2); err != nil {
		t.Fatal(err)
	}
	// Attach path in RunSUP now refuses when known; store itself may still allow overwrite —
	// assert the known gate the settler consults.
	used2, err := s.SUPTxHashKnown(ctx, "shared-tx-hash")
	if err != nil || !used2 {
		t.Fatalf("second worker must see hash as known: used=%v err=%v", used2, err)
	}
}
