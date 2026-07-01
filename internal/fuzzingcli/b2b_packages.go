package fuzzingcli

import (
	"fmt"
	"strings"

	"hackme/internal/fuzzengine"
)

// B2BPackage is a customer-facing fuzz tier (Scan / Audit / Deep).
type B2BPackage struct {
	Name            string
	DepthTier       fuzzengine.DepthTier
	BudgetHMC       float64
	BudgetRuns      int
	PoolDistributed bool
	CreatePoHOrder  bool
	RewardHMC       float64
}

var b2bPackages = map[string]B2BPackage{
	"scan": {
		Name: "scan", DepthTier: fuzzengine.DepthWasmOnly,
		BudgetHMC: 1.0, BudgetRuns: 64, PoolDistributed: false, CreatePoHOrder: false,
	},
	"audit": {
		Name: "audit", DepthTier: fuzzengine.DepthWasmNative,
		BudgetHMC: 5.0, BudgetRuns: 256, PoolDistributed: true, CreatePoHOrder: true, RewardHMC: 0.05,
	},
	"deep": {
		Name: "deep", DepthTier: fuzzengine.DepthBytesCorpus,
		BudgetHMC: 10.0, BudgetRuns: 1000, PoolDistributed: true, CreatePoHOrder: true, RewardHMC: 0.05,
	},
}

// B2BPackageFor resolves scan|audit|deep (aliases: starter, pro, enterprise).
func B2BPackageFor(name string) (B2BPackage, error) {
	key := strings.TrimSpace(strings.ToLower(name))
	switch key {
	case "starter":
		key = "scan"
	case "pro":
		key = "audit"
	case "enterprise":
		key = "deep"
	}
	p, ok := b2bPackages[key]
	if !ok {
		return B2BPackage{}, fmt.Errorf("unknown package %q (use scan, audit, or deep)", name)
	}
	if preset, ok := fuzzengine.DepthPresetFor(p.DepthTier); ok {
		if p.BudgetHMC <= 0 {
			p.BudgetHMC = preset.BudgetHMC
		}
		if p.BudgetRuns < 8 {
			p.BudgetRuns = preset.BudgetRuns
		}
	}
	return p, nil
}

// IsLoopbackBase returns true when API base is safe for order/escrow creation.
func IsLoopbackBase(base string) bool {
	b := strings.TrimSpace(strings.ToLower(base))
	b = strings.TrimPrefix(b, "http://")
	b = strings.TrimPrefix(b, "https://")
	b = strings.TrimSuffix(b, "/")
	if i := strings.IndexByte(b, '/'); i >= 0 {
		b = b[:i]
	}
	host := b
	if j := strings.LastIndexByte(b, ':'); j >= 0 {
		host = b[:j]
	}
	switch host {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}
