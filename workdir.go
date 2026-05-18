package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// ensureStableWorkingDir sets the process working directory so relative paths
// (./data, ./tasks, ./logs) resolve next to the shipped binary on Windows when
// the user starts hackme.exe from a shortcut (cwd is often not the install dir).
// Skipped for `go run` / `go test` temp binaries (path contains "go-build").
// Override: HACKME_WORKING_DIR=/path, or HACKME_NO_EXE_CHDIR=1 to keep cwd.
func ensureStableWorkingDir() {
	if strings.TrimSpace(os.Getenv("HACKME_NO_EXE_CHDIR")) == "1" {
		return
	}
	if wd := strings.TrimSpace(os.Getenv("HACKME_WORKING_DIR")); wd != "" {
		abs, err := filepath.Abs(wd)
		if err != nil {
			log.Printf("hackme: HACKME_WORKING_DIR abs: %v", err)
			return
		}
		if err := os.Chdir(abs); err != nil {
			log.Printf("hackme: chdir HACKME_WORKING_DIR %s: %v", abs, err)
			return
		}
		log.Printf("hackme: working directory (HACKME_WORKING_DIR): %s", abs)
		return
	}

	exe, err := os.Executable()
	if err != nil {
		return
	}
	if sym, err := filepath.EvalSymlinks(exe); err == nil && sym != "" {
		exe = sym
	}
	low := strings.ToLower(exe)
	if strings.Contains(low, "go-build") {
		return
	}

	dir := filepath.Clean(filepath.Dir(exe))
	if err := os.Chdir(dir); err != nil {
		log.Printf("hackme: chdir to executable directory %s: %v", dir, err)
		return
	}
	log.Printf("hackme: working directory set to executable directory: %s", dir)
}
