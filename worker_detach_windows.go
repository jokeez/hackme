//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// Detach pool worker from the node console so closing the dashboard window does not kill mining.
func configurePoolWorkerCmd(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | syscall.DETACHED_PROCESS,
	}
}
