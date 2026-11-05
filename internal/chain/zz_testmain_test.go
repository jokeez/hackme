package chain

import (
	"os"
	"testing"
)

// Package tests activate Economics v2 at tiny heights; the producer-side activation guard
// (confirm + 14-day lead) is exercised explicitly in TestV2ActivationGuard*.
func TestMain(m *testing.M) {
	v2ActivationGuardOff = true
	// Most package tests predate the announce: they pick their own activation height and
	// signer. The compiled defaults are covered with v2CompiledDefaultsOff=false explicitly.
	v2CompiledDefaultsOff = true
	os.Exit(m.Run())
}
