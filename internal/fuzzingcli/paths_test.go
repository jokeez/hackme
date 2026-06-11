package fuzzingcli

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTokenConfigPathEnvOverride(t *testing.T) {
	t.Setenv("HACKME_DEVELOPER_TOKEN_FILE", "/tmp/custom.token")
	t.Setenv("APPDATA", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	got := TokenConfigPath()
	if got != "/tmp/custom.token" {
		t.Fatalf("got %q want /tmp/custom.token", got)
	}
}

func TestTokenConfigPathWindowsAppData(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	t.Setenv("HACKME_DEVELOPER_TOKEN_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("APPDATA", `C:\Users\test\AppData\Roaming`)
	got := TokenConfigPath()
	want := filepath.Join(`C:\Users\test\AppData\Roaming`, "HackMe", "developer.token")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestBuildHelperCandidatesFromVersionedName(t *testing.T) {
	cands := BuildHelperCandidates()
	if len(cands) == 0 {
		t.Fatal("expected candidates")
	}
	found := false
	for _, c := range cands {
		if strings.Contains(c, "hackme-fuzzing-build") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no build helper candidate in %v", cands)
	}
}
