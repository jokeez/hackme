package hunt

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestReplayShardDetectsIntentionalCrash(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang required")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "fuzz_target.c")
	body := `int LLVMFuzzerTestOneInput(const unsigned char *d, unsigned long n) {
		if (n > 4 && d[0]=='c' && d[1]=='r' && d[2]=='a' && d[3]=='s' && d[4]=='h') {
			*(volatile int*)0 = 1;
		}
		return 0;
	}`
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	root := RepoRoot()
	pin := &RepoPinResult{Path: dir, CommitSHA: "testsha"}
	build, err := BuildInventoryHarness(context.Background(), root, HarnessBuildRequest{
		Pin: pin, SourceRel: "fuzz_target.c",
	})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := ReplayShard(context.Background(), ReplayShardOpts{
		RepoRoot: root, HarnessHash: build.HarnessHash,
		Spec: HarnessSpec{Source: "inventory", HarnessHash: build.HarnessHash, SourceRel: "fuzz_target.c"},
		Input: []byte("crash"), MaxInput: 256, ExecPer: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Crash {
		t.Fatalf("expected ASAN crash trap=%q", rep.Trap)
	}
	if !strings.HasPrefix(rep.Trap, "hunt_crash:") {
		t.Fatalf("trap=%q", rep.Trap)
	}
}
