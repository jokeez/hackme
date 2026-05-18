package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFileIntoEnviron(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("ZZZ_FROM_FILE", "")
	dir := t.TempDir()
	p := filepath.Join(dir, "t.env")
	content := "# c\nTELEGRAM_BOT_TOKEN=fromfile\nexport ZZZ_FROM_FILE=42\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := loadEnvFileIntoEnviron(p); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("TELEGRAM_BOT_TOKEN"); got != "fromfile" {
		t.Fatalf("token: %q", got)
	}
	if got := os.Getenv("ZZZ_FROM_FILE"); got != "42" {
		t.Fatalf("zzz: %q", got)
	}
	t.Setenv("TELEGRAM_BOT_TOKEN", "fromenv")
	if err := loadEnvFileIntoEnviron(p); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("TELEGRAM_BOT_TOKEN"); got != "fromenv" {
		t.Fatalf("env should win: %q", got)
	}
}
