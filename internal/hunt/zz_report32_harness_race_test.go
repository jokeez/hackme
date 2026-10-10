package hunt

// Report #32: concurrent PutHarnessArtifact / WriteHarnessObject must not
// rename-overwrite a harness_hash with a different binary.

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"hackme/internal/store"
)

func TestReport32PutHarnessConcurrentOverwriteRefused(t *testing.T) {
	dir := t.TempDir()
	obj := filepath.Join(dir, "harness-obj")
	SetHarnessObjectDir(obj)
	t.Cleanup(func() { SetHarnessObjectDir("") })

	db, err := store.Open(filepath.Join(dir, "r32.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	const hash = "aabbccddeeff0011"
	a := []byte("report32-binary-AAAA")
	b := []byte("report32-binary-BBBB")

	var wg sync.WaitGroup
	var okA, okB, bindErrs atomic.Int32
	wg.Add(2)
	go func() {
		defer wg.Done()
		err := PutHarnessArtifact(ctx, db, hash, a, "a.c")
		if err == nil {
			okA.Add(1)
			return
		}
		if containsAlreadyBound(err) {
			bindErrs.Add(1)
			return
		}
		t.Errorf("A: unexpected err %v", err)
	}()
	go func() {
		defer wg.Done()
		err := PutHarnessArtifact(ctx, db, hash, b, "b.c")
		if err == nil {
			okB.Add(1)
			return
		}
		if containsAlreadyBound(err) {
			bindErrs.Add(1)
			return
		}
		t.Errorf("B: unexpected err %v", err)
	}()
	wg.Wait()

	if okA.Load()+okB.Load() != 1 {
		t.Fatalf("want exactly one successful publish, okA=%d okB=%d bindErrs=%d", okA.Load(), okB.Load(), bindErrs.Load())
	}
	if bindErrs.Load() < 1 {
		t.Fatalf("want at least one already-bound error, bindErrs=%d", bindErrs.Load())
	}
	got, err := GetHarnessArtifact(ctx, db, hash)
	if err != nil {
		t.Fatal(err)
	}
	if !bytesEqual(got, a) && !bytesEqual(got, b) {
		t.Fatalf("stored bytes match neither winner: %q", got)
	}
	// Loser must still be refused after winner settled.
	loser := b
	if bytesEqual(got, b) {
		loser = a
	}
	if err := PutHarnessArtifact(ctx, db, hash, loser, "lose.c"); err == nil || !containsAlreadyBound(err) {
		t.Fatalf("post-race overwrite want already bound, got %v", err)
	}
}

func TestReport32WriteHarnessObjectExclusive(t *testing.T) {
	dir := t.TempDir()
	const hash = "1122334455667788"
	if _, err := WriteHarnessObject(dir, hash, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteHarnessObject(dir, hash, []byte("first")); err != nil {
		t.Fatalf("identical rewrite must be ok: %v", err)
	}
	if _, err := WriteHarnessObject(dir, hash, []byte("second")); err == nil || !containsAlreadyBound(err) {
		t.Fatalf("different rewrite want already bound, got %v", err)
	}
}

func containsAlreadyBound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "already bound")
}
