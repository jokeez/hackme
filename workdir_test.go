package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureStableWorkingDirUsesHackmeWorkingDir(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	d := t.TempDir()
	t.Setenv("HACKME_WORKING_DIR", d)
	t.Setenv("HACKME_NO_EXE_CHDIR", "") // clear if inherited
	ensureStableWorkingDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(wd) != filepath.Clean(d) {
		t.Fatalf("cwd=%q want %q", wd, d)
	}
}
