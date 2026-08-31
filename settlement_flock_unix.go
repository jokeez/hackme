//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func withSettlementStateLock(path string, fn func() error) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fn()
	}
	lockPath := path + ".flock"
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fn()
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return fn()
	}
	defer func() { _ = syscall.Flock(int(lf.Fd()), syscall.LOCK_UN) }()
	return fn()
}
