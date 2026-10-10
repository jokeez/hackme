package fuzzingcli

import (
	"fmt"
	"strings"

	"hackme/internal/fuzzengine"
	"hackme/internal/netutil"
)

// B2BPackage is a customer-facing fuzz tier (Scan / Audit / Deep).
type B2BPackage struct {
	Name            string
	DepthTier       fuzzengine.DepthTier
	BudgetHMC       float64
	BudgetRuns      int
	BudgetSeconds   int
	PoolDistributed bool
	CreatePoHOrder  bool
	RewardHMC       float64
	// Summary is the honest one-line depth claim for CLI output.
	Summary string
	// SignalTypes are the capability signals that differ across packages (not just budgets).
	SignalTypes []string
	// MutationRounds overrides engine default when > 0 (Deep uses heavier mutation).
	MutationRounds int
	// CoverageGuided is explicit for Deep corpus campaigns.
	CoverageGuided bool
}

var b2bPackages = map[string]B2BPackage{
	"scan": {
		Name: "scan", DepthTier: fuzzengine.DepthWasmOnly,
		BudgetHMC: 1.0, BudgetRuns: 64, BudgetSeconds: 900,
		PoolDistributed: false, CreatePoHOrder: false,
		Summary:     "Smoke · local WASM quick check + attested report — not OSS-Fuzz/CVE replacement",
		SignalTypes: []string{"wasm_smoke"},
	},
	"audit": {
		Name: "audit", DepthTier: fuzzengine.DepthWasmNative,
		BudgetHMC: 5.0, BudgetRuns: 256, BudgetSeconds: 28800, // ~8h SLA window
		PoolDistributed: true, CreatePoHOrder: true, RewardHMC: 0.05,
		Summary:     "Smoke · distributed Dig + native/ASAN repro + attested report — not OSS-Fuzz/CVE replacement",
		SignalTypes: []string{"wasm_check", "native_repro"},
	},
	"deep": {
		Name: "deep", DepthTier: fuzzengine.DepthBytesCorpus,
		BudgetHMC: 25.0, BudgetRuns: 2048, BudgetSeconds: 86400, // 24h hours-scale
		PoolDistributed: true, CreatePoHOrder: true, RewardHMC: 0.05,
		Summary:        "Deep · paid depth with seed merge + stricter verify — not OSS-Fuzz/CVE replacement",
		SignalTypes:    []string{"byte_corpus", "structured_mutation", "corpus_scheduling", "segment_exec", "native_repro"},
		MutationRounds: 12,
		CoverageGuided: true,
	},
}

// B2BPackageDisplayName maps internal package key to customer SKU label.
func B2BPackageDisplayName(name string) string {
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "scan", "starter":
		return "Scan"
	case "audit", "pro":
		return "Dig · Audit"
	case "deep", "enterprise":
		return "Dig · Deep"
	default:
		return name
	}
}
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
// Delegates to netutil.IsLoopbackURL (real URL parse) — never slice on ':' /
// userinfo, which previously let http://127.0.0.1:8080@evil.example pass (report #24).
func IsLoopbackBase(base string) bool {
	return netutil.IsLoopbackURL(base)
}
