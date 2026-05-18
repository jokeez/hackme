package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseEnvFileIntoOSEnvDoesNotOverrideExisting(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hackme.env")
	if err := os.WriteFile(p, []byte("HACKME_FOO=from_file\n# c\nHACKME_BAR=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_FOO", "preset")
	if err := parseEnvFileIntoOSEnv(p); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("HACKME_FOO"); got != "preset" {
		t.Fatalf("HACKME_FOO=%q want preset", got)
	}
	if got := os.Getenv("HACKME_BAR"); got != "2" {
		t.Fatalf("HACKME_BAR=%q", got)
	}
}

func TestParseEnvFileStripsBOM(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	raw := []byte{0xef, 0xbb, 0xbf}
	raw = append(raw, []byte("HACKME_BOM_K=v\n")...)
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := parseEnvFileIntoOSEnv(p); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("HACKME_BOM_K"); got != "v" {
		t.Fatalf("got %q", got)
	}
}
