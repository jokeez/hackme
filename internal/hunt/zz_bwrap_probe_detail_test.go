package hunt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A host where bwrap exists but cannot cage reported only "bwrap required (probe failed)".
// On this desk the real line is "bwrap: loopback: Failed RTM_NEWADDR: Operation not
// permitted" — the sandbox cannot create a network namespace — and it was discarded by
// `_ = out`, so the message named no cause an operator or a miner could act on.
func TestBubblewrapProbeDetailKeepsTheReason(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "bwrap")
	// A bwrap that fails the way a restricted host does.
	script := "#!/bin/sh\necho 'bwrap: loopback: Failed RTM_NEWADDR: Operation not permitted' >&2\nexit 1\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ResetBubblewrapProbeForTest()
	t.Cleanup(ResetBubblewrapProbeForTest)

	if BubblewrapWorking() {
		t.Fatal("probe passed against a bwrap that exits 1")
	}
	detail := BubblewrapProbeDetail()
	if !strings.Contains(detail, "RTM_NEWADDR") {
		t.Fatalf("probe detail %q lost the reason", detail)
	}
	if strings.ContainsAny(detail, "\n\r") {
		t.Fatalf("probe detail kept more than one line: %q", detail)
	}
	if len([]rune(detail)) > bwrapProbeDetailMax {
		t.Fatalf("probe detail is %d runes, want at most %d", len([]rune(detail)), bwrapProbeDetailMax)
	}
	// The suffix is what reaches the operator-facing error.
	if sfx := bwrapProbeSuffix(); !strings.Contains(sfx, "RTM_NEWADDR") || !strings.HasPrefix(sfx, ": ") {
		t.Fatalf("error suffix %q does not carry the reason", sfx)
	}
}

// A probe that passes must leave no stale reason behind.
func TestBubblewrapProbeDetailEmptyOnSuccess(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "bwrap")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ResetBubblewrapProbeForTest()
	t.Cleanup(ResetBubblewrapProbeForTest)

	if !BubblewrapWorking() {
		t.Skip("probe did not pass against the stub; environment refuses exec")
	}
	if d := BubblewrapProbeDetail(); d != "" {
		t.Fatalf("passing probe left a reason: %q", d)
	}
	if sfx := bwrapProbeSuffix(); sfx != "" {
		t.Fatalf("passing probe produced an error suffix: %q", sfx)
	}
}
