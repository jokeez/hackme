// Package gitutil provides safe git subprocess helpers for operator-supplied remotes.
package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
)

// IsolateCmd disables repo/global hooks and config so clone/checkout of untrusted
// remotes cannot run shipped hooks as the host user (audit H3).
func IsolateCmd(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	base := cmd.Env
	if base == nil {
		base = os.Environ()
	}
	cmd.Env = append(base,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
	)
	nullHooks := filepath.Join(os.TempDir(), "hackme-git-hooks-empty")
	_ = os.MkdirAll(nullHooks, 0o700)
	bin := cmd.Path
	if bin == "" && len(cmd.Args) > 0 {
		bin = cmd.Args[0]
	}
	if bin == "" {
		bin = "git"
	}
	rest := []string(nil)
	if len(cmd.Args) > 1 {
		rest = append(rest, cmd.Args[1:]...)
	} else if len(cmd.Args) == 1 {
		// Path-only command with empty Args — keep as git with no subcommand (invalid; caller bug).
	}
	cmd.Path = bin
	cmd.Args = append([]string{bin,
		"-c", "core.hooksPath=" + nullHooks,
		"-c", "protocol.file.allow=never",
		"-c", "submodule.recurse=false",
		"-c", "core.symlinks=false",
	}, rest...)
}
