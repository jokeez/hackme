package fuzzingcli

import (
	"fmt"
	"strings"

	"hackme/internal/fuzzengine"
)

// ApplyDigMutatorDict sets a rich domain mutator dictionary for a guard pack.
func ApplyDigMutatorDict(cfg map[string]any, packID string) {
	if cfg == nil {
		return
	}
	packID = strings.TrimSpace(strings.ToLower(packID))
	if packID == "" {
		return
	}
	dict, profile := digMutatorDictForPack(packID)
	if len(dict) == 0 {
		return
	}
	cfg["mutator_dict"] = dict
	cfg["dig_mutator_profile"] = profile
}

// ApplyDigPowerScheduling tunes mutation depth for Dig tiers (pool + local).
func ApplyDigPowerScheduling(cfg map[string]any, pkgName string) {
	if cfg == nil {
		return
	}
	pkgName = strings.TrimSpace(strings.ToLower(pkgName))
	minCap := 0
	switch pkgName {
	case "scan", "starter":
		minCap = 2
	case "audit", "pro":
		minCap = 8
	case "deep", "enterprise":
		minCap = 14
	}
	if minCap > 0 {
		cur := fuzzengine.PowerMutCap(cfg)
		if cur < minCap {
			cfg["power_mut_cap"] = minCap
		}
	}
	switch pkgName {
	case "deep", "enterprise":
		if fuzzengine.ParseDepthTier(cfg) == fuzzengine.DepthBytesCorpus {
			if fuzzengine.MutationRounds(cfg) < 12 {
				cfg["mutation_rounds"] = 12
			}
			if !fuzzengine.GuidedSchedulingEnabled(cfg) {
				cfg["guided_scheduling"] = true
				cfg["coverage_guided"] = true
			}
		}
	case "audit", "pro":
		if fuzzengine.GuidedSchedulingEnabled(cfg) && fuzzengine.MutationRounds(cfg) < 6 {
			cfg["mutation_rounds"] = 6
		}
	}
}

// DigPackageFromDepthTier maps engine depth tier to B2B package key.
func DigPackageFromDepthTier(tier fuzzengine.DepthTier) string {
	switch tier {
	case fuzzengine.DepthBytesCorpus, fuzzengine.DepthUpstreamBinary:
		return "deep"
	case fuzzengine.DepthWasmNative:
		return "audit"
	default:
		return "scan"
	}
}

// FinalizeDigCampaignConfig applies Dig depth enhancements after pack + tier merge.
func FinalizeDigCampaignConfig(cfg map[string]any, pkgName, packID, repoRoot string) map[string]any {
	if cfg == nil {
		cfg = map[string]any{}
	}
	packID = strings.TrimSpace(packID)
	if packID == "" {
		packID = strings.TrimSpace(cfgString(cfg, "guard_pack"))
	}
	if packID == "" {
		packID = strings.TrimSpace(cfgString(cfg, "guard_name"))
	}
	if packID != "" {
		ApplyDigMutatorDict(cfg, packID)
	}
	if strings.TrimSpace(pkgName) == "" {
		pkgName = DigPackageFromDepthTier(fuzzengine.ParseDepthTier(cfg))
	}
	ApplyDigPowerScheduling(cfg, pkgName)
	if packID != "" && strings.TrimSpace(repoRoot) != "" {
		if n, err := MergeDigSeedCorpus(cfg, repoRoot, packID); err == nil && n > 0 {
			cfg["dig_external_seeds_merged"] = n
		}
	}
	if fuzzengine.CorpusPersistEnabled(cfg) {
		if _, ok := cfg["corpus_persist_max"]; !ok {
			max := 64
			if strings.EqualFold(pkgName, "deep") || strings.EqualFold(pkgName, "enterprise") {
				max = 128
			}
			cfg["corpus_persist_max"] = max
		}
	}
	cfg["dig_depth_profile"] = DigDepthProfile(cfg, pkgName, packID)
	return cfg
}

// DigDepthProfile returns a customer-facing depth summary string.
func DigDepthProfile(cfg map[string]any, pkgName, packID string) string {
	pkgName = strings.TrimSpace(pkgName)
	if pkgName == "" {
		pkgName = "scan"
	}
	display := B2BPackageDisplayName(pkgName)
	tier := string(fuzzengine.ParseDepthTier(cfg))
	parts := []string{display, "tier=" + tier}
	if packID != "" {
		parts = append(parts, "pack="+packID)
	}
	if fuzzengine.GuidedSchedulingEnabled(cfg) {
		parts = append(parts, "guided")
	}
	if ck := fuzzengine.CoverageKind(cfg); ck != "" {
		parts = append(parts, "coverage="+ck)
	}
	parts = append(parts,
		"mut_cap="+itoa(fuzzengine.PowerMutCap(cfg)),
		"mut_rounds="+itoa(fuzzengine.MutationRounds(cfg)),
		"exec/unit="+itoa(fuzzengine.ExecPerUnit(cfg)),
	)
	if ns := fuzzengine.CorpusPersistNamespace(cfg); ns != "" {
		parts = append(parts, "corpus_ns="+ns)
	}
	if n := intFromCfg(cfg, "dig_external_seeds_merged"); n > 0 {
		parts = append(parts, "ext_seeds="+itoa(n))
	}
	return strings.Join(parts, " · ")
}

func digMutatorDictForPack(packID string) ([]byte, string) {
	switch packID {
	case "secrets":
		return []byte("AKIAASIAghp_github_pat_sk_live_sk_test_xoxb-xoxp-xoxa-:latest|sh|curl|SECRET|TOKEN|PASSWORD|api_key"), "secrets_supply_chain"
	case "filter_utf8":
		return []byte("\xc0\xc1\xc2\xc3\xc7=\x80\xff\xfe\xfd!=<>\"'\\n\\r\\t%00"), "utf8_display_filter"
	case "parser_expat":
		return []byte("<>&lt;&gt;&amp;CDATA<?xml\"'=/!--[]%"), "xml_parser"
	case "script_bounds":
		return []byte("\x4c\x4d\x4e\x4fOP_PUSHDATA520\xff\x00"), "script_push"
	case "bounds_smoke", "overflow_smoke", "state_smoke":
		return []byte("\xff\x00\x7f\x80\x9e3779b9deadbeef"), "numeric_smoke"
	default:
		return nil, ""
	}
}

func cfgString(cfg map[string]any, key string) string {
	if cfg == nil {
		return ""
	}
	v, ok := cfg[key]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func intFromCfg(cfg map[string]any, key string) int {
	if cfg == nil {
		return 0
	}
	v, ok := cfg[key]
	if !ok || v == nil {
		return 0
	}
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	default:
		return 0
	}
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	buf := make([]byte, 0, 12)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}
