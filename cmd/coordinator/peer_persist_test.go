package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/lanpool"
	"hackme/internal/store"
)

func TestPeerPersistFlusherMarksAndFlushes(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "peers.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reg := lanpool.NewRegistry()
	share := true
	if err := reg.Upsert("127.0.0.1:9", lanpool.PushWorkBody{
		WorkerID: "w-flush-1", HashrateGHS: 1.5, ShareAccepted: &share, IP: "127.0.0.1",
	}); err != nil {
		t.Fatal(err)
	}
	f := newPeerPersistFlusher(db, reg, time.Second)
	f.mark("w-flush-1")
	if f.dirtyCount() != 1 {
		t.Fatalf("dirty=%d", f.dirtyCount())
	}
	f.flush(context.Background())
	if f.dirtyCount() != 0 {
		t.Fatal("dirty should clear after flush")
	}
	rows, err := store.LoadLANPeerRigs(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.WorkerID == "w-flush-1" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected peer row after flush")
	}
}
