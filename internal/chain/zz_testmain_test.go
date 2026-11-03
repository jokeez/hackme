package chain

import (
	"os"
	"testing"
)

// Package tests activate Economics v2 at tiny heights; the producer-side activation guard
// (confirm + 14-day lead) is exercised explicitly in TestV2ActivationGuard*.
func TestMain(m *testing.M) {
	v2ActivationGuardOff = true
	os.Exit(m.Run())
}
