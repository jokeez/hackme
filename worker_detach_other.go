//go:build !windows

package main

import "os/exec"

func configurePoolWorkerCmd(cmd *exec.Cmd) {}
