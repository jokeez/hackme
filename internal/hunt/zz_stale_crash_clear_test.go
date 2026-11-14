package hunt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResetHuntSessionCrashDirWipesPriorArtifacts(t *testing.T) {
	corpus := t.TempDir()
	crashDir := filepath.Join(corpus, ".hunt-run", "crashes")
	if err := os.MkdirAll(crashDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(crashDir, "crash-stale")
	if err := os.WriteFile(stale, []byte("old-crash"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := resetHuntSessionCrashDir(corpus, crashDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale crash must be gone, err=%v", err)
	}
	// Dir must exist again for libFuzzer artifact_prefix.
	if st, err := os.Stat(crashDir); err != nil || !st.IsDir() {
		t.Fatalf("crash dir must be recreated: %v", err)
	}
	if got := collectCrashArtifacts(corpus, filepath.Join(corpus, ".hunt-run"), 16); len(got) != 0 {
		t.Fatalf("collector must see empty crash dir, got %v", got)
	}
}

func TestResetHuntSessionCrashDirRejectsEscape(t *testing.T) {
	corpus := t.TempDir()
	outside := filepath.Join(t.TempDir(), "crashes")
	if err := resetHuntSessionCrashDir(corpus, outside); err == nil {
		t.Fatal("escape path must fail")
	}
}
