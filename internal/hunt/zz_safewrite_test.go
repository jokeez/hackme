//go:build !windows

package hunt

import (
	"os"
	"path/filepath"
	"testing"
)

// fimasw write-side report: host writes into cage-bound dirs followed planted
// symlinks. Each case plants a link and asserts the victim outside root is untouched.
func TestWriteBeneathNeverFollowsPlantedLinks(t *testing.T) {
	victimDir := t.TempDir()
	victim := filepath.Join(victimDir, "authorized_keys")
	mustW := func(p string, b string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(b), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	check := func(label string) {
		t.Helper()
		b, err := os.ReadFile(victim)
		if err != nil || string(b) != "ssh-ed25519 KEEP" {
			t.Fatalf("%s: victim changed: %q %v", label, b, err)
		}
	}
	mustW(victim, "ssh-ed25519 KEEP")

	// A: final component symlink (merge_stats.json).
	parent := t.TempDir()
	if err := os.Symlink(victim, filepath.Join(parent, "merge_stats.json")); err != nil {
		t.Fatal(err)
	}
	if err := writeFileBeneath(parent, filepath.Join(parent, "merge_stats.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write should replace the link with a regular file: %v", err)
	}
	check("final-component link")
	if st, _ := os.Lstat(filepath.Join(parent, "merge_stats.json")); st == nil || !st.Mode().IsRegular() {
		t.Fatal("merge_stats.json should now be a regular file")
	}

	// B: session.stderr symlink → os.Create used to truncate the victim.
	corpus := t.TempDir()
	if err := os.MkdirAll(filepath.Join(corpus, ".hunt-run"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(corpus, ".hunt-run", "session.stderr")); err != nil {
		t.Fatal(err)
	}
	f, err := createFileBeneath(corpus, filepath.Join(corpus, ".hunt-run", "session.stderr"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("#1 done")
	_ = f.Close()
	check("session.stderr link")

	// C: intermediate directory swapped for a symlink into the victim dir.
	corpus2 := t.TempDir()
	if err := os.Symlink(victimDir, filepath.Join(corpus2, ".hunt-run")); err != nil {
		t.Fatal(err)
	}
	if err := writeFileBeneath(corpus2, filepath.Join(corpus2, ".hunt-run", "authorized_keys"), []byte("pwn"), 0o600); err == nil {
		t.Fatal("write through a symlinked directory must fail")
	}
	if err := mkdirAllBeneath(corpus2, filepath.Join(corpus2, ".hunt-run", "crashes"), 0o755); err == nil {
		t.Fatal("mkdir through a symlinked directory must fail")
	}
	if _, err := os.Stat(filepath.Join(victimDir, "crashes")); !os.IsNotExist(err) {
		t.Fatal("mkdir escaped into the victim dir")
	}
	check("intermediate dir link")

	// D: path outside root is refused outright.
	if err := writeFileBeneath(corpus, victim, []byte("x"), 0o600); err == nil {
		t.Fatal("write outside root must be refused")
	}
	check("outside root")
}

// The root's own path may legitimately contain a symlink (macOS /tmp, symlinked data
// disk). Containment checks must still accept children of such a root.
func TestCorpusPathConfinedAcceptsSymlinkedRoot(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "corpus-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "seed.bin"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !corpusPathConfined(link, filepath.Join(link, "seed.bin")) {
		t.Fatal("child of a symlinked root rejected — Hunt would see an empty corpus")
	}
	if n, _ := countCorpusFiles(link); n != 1 {
		t.Fatalf("countCorpusFiles via symlinked root = %d, want 1", n)
	}
}
