//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// Win32 creation flags (numeric so cross-compilation from Linux works).
const (
	winCreateNewProcessGroup = 0x00000200
	winDetachedProcess       = 0x00000008
)

// Detach pool worker from the node console so closing the dashboard window does not kill mining.
func configurePoolWorkerCmd(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: winCreateNewProcessGroup | winDetachedProcess,
	}
}
