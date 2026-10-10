package fuzzingcli

import (
	"os"
	"strconv"
	"strings"

	"hackme/internal/fuzzengine"
)

// SKUHonesty is customer-facing Dig/Hunt metadata: mode, replay policy, seeds, promise.
// Wired into report dig_depth / sku_honesty cards — not a security certificate.
type SKUHonesty struct {
	Package          string `json:"package"`
	ProductMode      string `json:"product_mode"` // smoke | deep
	DepthTier        string `json:"depth_tier,omitempty"`
	ReplayPolicy     string `json:"replay_policy"` // sampled | full | hygiene_allowed
	ReplaySamplePct  int    `json:"replay_sample_pct"`
	LFBudgetSec      int    `json:"lf_budget_sec"`
	SeedsMerged      bool   `json:"seeds_merged"`
	SeedsMergedN     int    `json:"seeds_merged_count"`
	SeedFromResearch bool   `json:"seed_from_research"`
	ExecConfigured   int    `json:"exec_configured,omitempty"`
	ExecEffective    int    `json:"exec_effective,omitempty"`
	ExecHubCap       int    `json:"exec_hub_cap,omitempty"`
	CappedOnPool     bool   `json:"capped_on_pool,omitempty"`
	PromiseNote      string `json:"promise_note"`
	HonestyNote      string `json:"honesty_note,omitempty"`
}

const skuPromiseNote = "Distributed smoke or paid depth with attested pool report — not an OSS-Fuzz/CVE replacement, not a proof of security."

// DigProductMode maps scan/audit → smoke, deep → deep.
func DigProductMode(pkg string) string {
	switch strings.TrimSpace(strings.ToLower(pkg)) {
	case "deep", "enterprise":
		return "deep"
	default:
		return "smoke"
	}
}

// HuntProductMode maps hunt_heavy → deep; lite/standard → smoke.
func HuntProductMode(pkg string) string {
	switch strings.TrimSpace(strings.ToLower(pkg)) {
	case "hunt_heavy", "heavy":
		return "deep"
	default:
		return "smoke"
	}
}

// IsDeepDigPackage is true for deep/enterprise Dig SKUs (seed merge + stricter verify).
func IsDeepDigPackage(pkg string) bool {
	switch strings.TrimSpace(strings.ToLower(pkg)) {
	case "deep", "enterprise":
		return true
	default:
		return false
	}
}

func envIntDefault(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// BuildDigSKUHonesty documents Dig campaign SKU truth for reports/API.
func BuildDigSKUHonesty(cfg map[string]any, execConfigured, execEffective, hubCap int, capped bool) SKUHonesty {
	pkg := DigPackageFromDepthTier(fuzzengine.ParseDepthTier(cfg))
	if p := strings.TrimSpace(strings.ToLower(cfgString(cfg, "dig_package"))); p != "" {
		switch p {
		case "scan", "starter":
			pkg = "scan"
		case "audit", "pro":
			pkg = "audit"
		case "deep", "enterprise":
			pkg = "deep"
		}
	}
	mode := DigProductMode(pkg)
	samplePct := 5
	switch pkg {
	case "scan":
		samplePct = envIntDefault("HACKME_POOL_REPLAY_SAMPLE_PCT_SCAN", 1)
	case "deep":
		samplePct = envIntDefault("HACKME_POOL_REPLAY_SAMPLE_PCT_DEEP", 10)
	default:
		samplePct = envIntDefault("HACKME_POOL_REPLAY_SAMPLE_PCT", 5)
	}
	policy := "hygiene_allowed"
	if samplePct >= 100 {
		policy = "full"
	} else if samplePct > 0 {
		policy = "sampled"
	}
	seedsN := intFromCfg(cfg, "dig_external_seeds_merged")
	researchN := intFromCfg(cfg, "dig_research_seeds_fed")
	out := SKUHonesty{
		Package:          pkg,
		ProductMode:      mode,
		DepthTier:        string(fuzzengine.ParseDepthTier(cfg)),
		ReplayPolicy:     policy,
		ReplaySamplePct:  samplePct,
		LFBudgetSec:      envIntDefault("HACKME_WORKER_RESEARCH_WINDOW_SEC", 45),
		SeedsMerged:      seedsN > 0,
		SeedsMergedN:     seedsN,
		SeedFromResearch: researchN > 0,
		ExecConfigured:   execConfigured,
		ExecEffective:    execEffective,
		ExecHubCap:       hubCap,
		CappedOnPool:     capped,
		PromiseNote:      skuPromiseNote,
	}
	if mode == "smoke" {
		out.HonestyNote = "Smoke Dig: distributed smoke + attested report; external seed merge off by default; CLEAN uses floor+canary+sampled replay (not full OSS-Fuzz depth)."
	} else {
		out.HonestyNote = "Deep Dig: paid depth — seed merge enabled, higher replay sample, longer budgets; still not a CVE warranty."
	}
	if capped {
		out.HonestyNote += " Hub pool exec_per_unit is capped vs local package depth."
	}
	return out
}

// BuildHuntSKUHonesty documents Hunt campaign SKU truth for reports/API.
func BuildHuntSKUHonesty(cfg map[string]any) SKUHonesty {
	pkg := strings.TrimSpace(strings.ToLower(cfgString(cfg, "hunt_package")))
	if pkg == "" {
		pkg = "hunt_standard"
	}
	mode := HuntProductMode(pkg)
	samplePct := envIntDefault("HACKME_POOL_REPLAY_SAMPLE_PCT_HUNT", 100)
	policy := "full"
	if samplePct < 100 {
		if samplePct <= 0 {
			policy = "hygiene_allowed"
		} else {
			policy = "sampled"
		}
	}
	seedsN := intFromCfg(cfg, "hunt_external_seeds_merged")
	if seedsN == 0 {
		seedsN = intFromCfg(cfg, "external_seeds_merged")
	}
	return SKUHonesty{
		Package:         pkg,
		ProductMode:     mode,
		ReplayPolicy:    policy,
		ReplaySamplePct: samplePct,
		LFBudgetSec:     envIntDefault("HACKME_WORKER_RESEARCH_WINDOW_SEC", 45),
		SeedsMerged:     seedsN > 0,
		SeedsMergedN:    seedsN,
		PromiseNote:     skuPromiseNote,
		HonestyNote:     "Hunt: native ASAN+UBSan+LSan shards with coordinator replay. CLEAN means no qualifying crash in budget — not OSS-Fuzz/CVE replacement.",
	}
}
