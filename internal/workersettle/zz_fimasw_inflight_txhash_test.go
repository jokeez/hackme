package workersettle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fimasw 2026-11-18: HMC in-flight attach must consult TxHashKnown like stranded-resolve.
func TestFimaswSettlerInflightAttachGuardsTxHashKnown(t *testing.T) {
	src, err := os.ReadFile("settler.go")
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(src), "findInflightTransfer(")
	if i < 0 {
		t.Fatal("findInflightTransfer call site missing")
	}
	chunk := string(src)[i:]
	if j := strings.Index(chunk, "\n\t\t} else {"); j > 0 && j < 1200 {
		chunk = chunk[:j]
	}
	if !strings.Contains(chunk, "TxHashKnown") {
		t.Fatal("in-flight AttachTxHash path missing TxHashKnown gate")
	}
	if !strings.Contains(chunk, "AttachTxHash") {
		t.Fatal("expected AttachTxHash after gate")
	}
}

func TestAttachTxHashRejectsForeignWorkerHash(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "settle.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if _, err := s.SetPendingUnits(ctx, "w-a", "HMC-aaaa", 1_000_000, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachTxHash(ctx, "w-a", "shared-hmc-hash"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetPendingUnits(ctx, "w-b", "HMC-aaaa", 1_000_000, 2); err != nil {
		t.Fatal(err)
	}
	err = s.AttachTxHash(ctx, "w-b", "shared-hmc-hash")
	if err == nil || !strings.Contains(err.Error(), "already recorded") {
		t.Fatalf("want foreign-hash reject, got %v", err)
	}
}
