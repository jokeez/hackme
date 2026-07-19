package chain

import (
	"context"
	"path/filepath"
	"testing"

	"hackme/internal/store"
)

func TestFuzzSettleAppliedIdempotent(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "applied.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	eventID := FuzzSettleEventID(42)
	newly, err := svc.MarkFuzzSettleApplied(ctx, eventID, "camp", "run")
	if err != nil || !newly {
		t.Fatalf("first mark: newly=%v err=%v", newly, err)
	}
	newly, err = svc.MarkFuzzSettleApplied(ctx, eventID, "camp", "run")
	if err != nil || newly {
		t.Fatalf("second mark must be already-applied: newly=%v err=%v", newly, err)
	}
	ok, err := svc.HasFuzzSettleApplied(ctx, eventID)
	if err != nil || !ok {
		t.Fatalf("HasFuzzSettleApplied: ok=%v err=%v", ok, err)
	}
	if err := svc.UnmarkFuzzSettleApplied(ctx, eventID); err != nil {
		t.Fatal(err)
	}
	ok, err = svc.HasFuzzSettleApplied(ctx, eventID)
	if err != nil || ok {
		t.Fatalf("after unmark want absent, ok=%v err=%v", ok, err)
	}
}
