package fuzznative

import (
	"context"
	"testing"

	"hackme/internal/store"
)

func TestEvalReproDupInputsConfirmed(t *testing.T) {
	// duplicate byte at positions 0 and 1
	input := []byte{0x42, 0x42, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	res := EvalRepro("bitcoin", "bitcoin_tx_dup_inputs", input, nil)
	if res.Status != StatusConfirmed {
		t.Fatalf("expected confirmed, got %s note=%s", res.Status, res.Note)
	}
}

func TestEvalReproDupInputsRejected(t *testing.T) {
	input := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	res := EvalRepro("bitcoin", "bitcoin_tx_dup_inputs", input, nil)
	if res.Status != StatusRejected {
		t.Fatalf("expected rejected, got %s", res.Status)
	}
}

func TestNativeQueueAndProcess(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(dir + "/fuzz.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	input := []byte{0x42, 0x42, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	sha := "abc123"
	if err := QueueJob(ctx, db, "finding-test-1", "camp-1", sha, input, "bitcoin", "bitcoin_tx_dup_inputs", 100); err != nil {
		t.Fatal(err)
	}
	n, err := ProcessPending(ctx, db, nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("processed %d", n)
	}
	ok, err := IsFindingNativeConfirmed(ctx, db, "finding-test-1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected native confirmed")
	}
}
