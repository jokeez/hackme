package hunt

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

func TestEnsureHarnessBinaryCached(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang required")
	}
	root := RepoRoot()
	hash, err := CatalogHarnessHash(root, "jsmn")
	if err != nil {
		t.Skip(err)
	}
	ctx := context.Background()
	p1, err := EnsureHarnessBinary(ctx, root, "jsmn", hash)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := EnsureHarnessBinary(ctx, root, "jsmn", hash)
	if err != nil || p1 != p2 {
		t.Fatalf("cache miss p1=%q p2=%q err=%v", p1, p2, err)
	}
	if _, err := os.Stat(p1); err != nil {
		t.Fatal(err)
	}
}

func TestReplayShardCleanInput(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang required")
	}
	root := RepoRoot()
	hash, err := CatalogHarnessHash(root, "jsmn")
	if err != nil {
		t.Skip(err)
	}
	input := []byte(`{"a":1}`)
	rep, err := ReplayShard(context.Background(), ReplayShardOpts{
		RepoRoot: root, TargetID: "jsmn", HarnessHash: hash,
		Input: input, MaxInput: 256, ExecPer: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Crash {
		t.Fatalf("unexpected crash trap=%q", rep.Trap)
	}
	if rep.ExecDone != 2 {
		t.Fatalf("execDone=%d", rep.ExecDone)
	}
}
