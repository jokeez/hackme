package hunt

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWrapBubblewrapHardeningFlags(t *testing.T) {
	argv, ok := wrapBubblewrap("/bin/true", nil, "", nil, nil)
	if !ok {
		t.Skip("bwrap not on PATH")
	}
	joined := strings.Join(argv, " ")
	for _, flag := range []string{"--new-session", "--unshare-ipc", "--unshare-uts", "--cap-drop", "ALL", "--unshare-net"} {
		if !strings.Contains(joined, flag) {
			t.Fatalf("missing bwrap harden flag %q in %v", flag, argv)
		}
	}
}

func TestWrapBubblewrapNeverBindsPrefixRoot(t *testing.T) {
	t.Setenv("HACKME_PREFIX", "/opt/hackme")
	tmp := t.TempDir()
	// Fake binary sitting directly under a simulated prefix root.
	prefix := filepath.Join(tmp, "opt-hackme")
	if err := os.MkdirAll(prefix, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(prefix, "clang")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_PREFIX", prefix)
	argv, ok := wrapBubblewrap(bin, nil, prefix, []string{prefix, tmp}, []string{prefix})
	if !ok {
		t.Skip("bwrap not on PATH")
	}
	joined := strings.Join(argv, "\x00")
	prefixClean := filepath.Clean(prefix)
	// Count --bind/--ro-bind targets equal to the prefix root.
	for i := 0; i+2 < len(argv); i++ {
		switch argv[i] {
		case "--bind", "--ro-bind", "--ro-bind-try":
			if filepath.Clean(argv[i+1]) == prefixClean || filepath.Clean(argv[i+2]) == prefixClean {
				t.Fatalf("prefix root must not be bound: argv=%v", argv)
			}
		}
	}
	_ = joined
	// File-only bind of the binary is OK when binDir == prefix.
	foundBin := false
	for i := 0; i+2 < len(argv); i++ {
		if argv[i] == "--ro-bind" && filepath.Clean(argv[i+1]) == filepath.Clean(bin) {
			foundBin = true
		}
	}
	if !foundBin {
		t.Fatalf("expected file-only ro-bind of bin when binDir is prefix: %v", argv)
	}
}

func TestAllowBareTriesBwrapFirst(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "bwrap-used")
	fake := filepath.Join(dir, "bwrap")
	// Accept probe argv (/bin/true) and harness argv; always mark + exit 0.
	script := "#!/bin/sh\necho used >\"" + marker + "\"\nexit 0\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HACKME_HUNT_ISOLATE_REQUIRE", "1")
	t.Setenv("HACKME_HUNT_ISOLATE_STRICT", "1")
	ResetBubblewrapProbeForTest()
	if !BubblewrapWorking() {
		t.Fatal("fake bwrap should make BubblewrapWorking true")
	}

	bin := filepath.Join(dir, "harness")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho should-not-run\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := RunIsolated(context.Background(), IsolateOpts{
		Bin:       bin,
		Args:      nil,
		Wall:      2 * time.Second,
		AllowBare: true,
	})
	if err != nil {
		t.Fatalf("AllowBare must still use bwrap when present: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("AllowBare must not skip bwrap: marker missing (regression: early bare bypass)")
	}
}

// TestNoBareReexecAfterForgedBwrapStderr: harness prints fake bwrap Permission denied;
// under STRICT+AllowBare=false we must NOT re-exec outside the cage.
func TestNoBareReexecAfterForgedBwrapStderr(t *testing.T) {
	if !BubblewrapAvailable() {
		t.Skip("bwrap not on PATH")
	}
	dir := t.TempDir()
	bareMarker := filepath.Join(dir, "bare-ran")
	bin := filepath.Join(dir, "harness")
	// Inside cage: forge bwrap stderr and exit 1. Bare path would touch bareMarker
	// via ExtraBind of dir — but if re-exec happens without cage, marker appears.
	script := "#!/bin/sh\necho 'bwrap: Creating new namespace failed: Permission denied' >&2\n" +
		"touch \"" + bareMarker + ".in-cage\"\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_HUNT_ISOLATE_REQUIRE", "1")
	t.Setenv("HACKME_HUNT_ISOLATE_STRICT", "1")
	err := RunIsolated(context.Background(), IsolateOpts{
		Bin:       bin,
		Wall:      5 * time.Second,
		AllowBare: false,
		ExtraBind: []string{dir},
	})
	if err == nil {
		t.Fatal("expected error from forged harness exit")
	}
	// The critical property: we must not have silently succeeded via bare re-exec
	// that ignored the cage. AllowBare=false + STRICT → error mentioning bwrap or exit.
	if !strings.Contains(err.Error(), "bwrap") && !strings.Contains(err.Error(), "exit") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestAttachIsolatedStdioSameWriterNoRace(t *testing.T) {
	// Production: one stdout pump + one stderr pump share the syncWriter dest.
	var buf bytes.Buffer
	sw := newSyncWriter(&buf)
	ec := bareIsolatedCommand(context.Background(), "/bin/true", nil)
	var tee bytes.Buffer
	attachIsolatedStdio(ec, sw, sw, &tee)
	const n = 256
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			_, _ = io.WriteString(ec.Stdout, "O")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			_, _ = io.WriteString(ec.Stderr, "E")
		}
	}()
	wg.Wait()
	if buf.Len() != n*2 {
		t.Fatalf("shared buffer len=%d want %d", buf.Len(), n*2)
	}
	if tee.Len() != n {
		t.Fatalf("stderr tee len=%d want %d", tee.Len(), n)
	}
}

func TestScrubIsolateEnvDropsSecrets(t *testing.T) {
	in := []string{
		"PATH=/usr/bin",
		"COORD_TOKEN=supersecret",
		"HACKME_ADMIN_TOKEN=admin",
		"HACKME_POOL_COORDINATOR_TOKEN=pooltok",
		"HACKME_POOL_COORDINATOR_URL=http://127.0.0.1:18083",
		"HACKME_MINER_ED25519_SEED_HEX=deadbeef",
		"AWS_SECRET_ACCESS_KEY=aws",
		"http_proxy=http://evil",
		"AFL_SKIP_CPUFREQ=1",
	}
	out := scrubIsolateEnv(in)
	joined := strings.Join(out, "\n")
	for _, bad := range []string{
		"COORD_TOKEN=", "HACKME_ADMIN_TOKEN=", "HACKME_POOL_COORDINATOR_TOKEN=",
		"HACKME_MINER_ED25519_SEED_HEX=", "AWS_SECRET_ACCESS_KEY=", "http_proxy=",
	} {
		if strings.Contains(joined, bad) {
			t.Fatalf("secret leaked in isolate env: %s\n%s", bad, joined)
		}
	}
	keep := map[string]bool{}
	for _, e := range out {
		k, _, _ := strings.Cut(e, "=")
		keep[k] = true
	}
	if !keep["PATH"] || !keep["AFL_SKIP_CPUFREQ"] || !keep["HACKME_POOL_COORDINATOR_URL"] {
		t.Fatalf("expected non-secret knobs kept: %#v", out)
	}
}
