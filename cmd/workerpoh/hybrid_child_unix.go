//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

func configureHybridChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
