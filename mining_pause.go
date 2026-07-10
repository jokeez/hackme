package main

import (
	"os"
	"path/filepath"
	"strings"
)

func miningRepoRoot() string {
	for _, k := range []string{"HACKME_REPO_ROOT", "HACKME_WORKING_DIR"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

// miningPaused is true when operator paused pool workers (desktop_mode_stop / stop_pool_workers).
func miningPaused() bool {
	if envBool("HACKME_MINING_PAUSED", false) {
		return true
	}
	root := miningRepoRoot()
	candidates := []string{
		strings.TrimSpace(os.Getenv("HACKME_MINING_PAUSED_FILE")),
	}
	if root != "" {
		candidates = append(candidates,
			filepath.Join(root, "logs", "desktop", "mining_paused"),
			filepath.Join(root, "logs", "mining_paused"),
		)
	}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}
