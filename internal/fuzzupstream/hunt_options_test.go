package fuzzupstream

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTargetIDsFromFlags(t *testing.T) {
	if TargetIDsFromFlags("all") != nil {
		t.Fatal("all => nil")
	}
	got := TargetIDsFromFlags("mpack, tinycbor")
	if len(got) != 2 || got[0] != "mpack" || got[1] != "tinycbor" {
		t.Fatalf("%v", got)
	}
}

func TestHuntWithOptionsRecordsDictAndCorpus(t *testing.T) {
	// Use a trivial echo-like binary that always exits 0 (no crash).
	// On Linux /bin/true ignores stdin.
	bin := "/bin/true"
	if _, err := os.Stat(bin); err != nil {
		t.Skip("no /bin/true")
	}
	dir := t.TempDir()
	corpus := filepath.Join(dir, "corpus")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rep, err := HuntWithOptions(ctx, ".", Target{ID: "mpack", Title: "t"}, bin,
		[][]byte{[]byte("{}"), []byte("[]")}, 80, 256, 3, HuntRunOptions{
			MutatorDict: []byte(`nulltrue`),
			CorpusDir:   corpus,
			MaxCorpus:   32,
		})
	if err != nil {
		t.Fatal(err)
	}
	if rep.DictBytes == 0 {
		t.Fatal("expected dict bytes recorded")
	}
	if rep.Iterations == 0 {
		t.Fatal("expected iterations")
	}
	if rep.Verdict != "CLEAN" {
		t.Fatalf("verdict=%s", rep.Verdict)
	}
}
