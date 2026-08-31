//go:build windows

package main

func withSettlementStateLock(path string, fn func() error) error {
	return fn()
}
