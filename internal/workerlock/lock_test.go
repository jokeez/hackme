package workerlock

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestAcquireExclusive(t *testing.T) {
	dir := t.TempDir()
	g1, err := Acquire("workerpoh", "rig-a", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer g1.Release()
	if g1.Path() != filepath.Join(dir, "workerlock-workerpoh-rig-a.pid") {
		t.Fatalf("path=%s", g1.Path())
	}
	_, err = Acquire("workerpoh", "rig-a", dir)
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("want ErrAlreadyRunning, got %v", err)
	}
	// Different kind or id can run in parallel.
	g2, err := Acquire("workerfuzz", "rig-a", dir)
	if err != nil {
		t.Fatal(err)
	}
	g2.Release()
	g1.Release()
	g3, err := Acquire("workerpoh", "rig-a", dir)
	if err != nil {
		t.Fatal(err)
	}
	g3.Release()
}

func TestHeld(t *testing.T) {
	dir := t.TempDir()
	if Held("workerfuzz", "rig-b", dir) {
		t.Fatal("empty lock should not be held")
	}
	g, err := Acquire("workerfuzz", "rig-b", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Release()
	if !Held("workerfuzz", "rig-b", dir) {
		t.Fatal("expected held while Acquire live")
	}
	g.Release()
	if Held("workerfuzz", "rig-b", dir) {
		t.Fatal("expected free after Release")
	}
}
