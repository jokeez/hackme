package fuzzingcli

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hackme/internal/fuzzengine"
)

// GuardPack is a ready-made detector pack (customer does not write a rule).
type GuardPack struct {
	ID               string
	Title            string
	Summary          string
	WasmRelPath      string // under repo root (artifacts)
	SourceRelPath    string // rust source to rebuild if wasm missing
	InputMode        string // bytes|u64
	MaxInputBytes    int
	Guided           bool
	MutationRounds   int
	SeedByteCorpus   []any
	SeedCorpusU64    []any
	ExplainHints     []ExplainHint
	DefaultPackage   string // scan|audit|deep suggestion
	WasmEdgeCoverage bool   // instrumented guard writes edge counters at mem offset 8192
	// Per-package budget overrides when > 0 (pack-aware presets).
	ScanRuns     int
	AuditRuns    int
	DeepRuns     int
	ScanSeconds  int
	AuditSeconds int
	DeepSeconds  int
}

// ExplainHint maps finding patterns to customer-facing guidance.
type ExplainHint struct {
	Contains string // case-sensitive substring in decoded input or title
	Message  string
}

var guardPacks = map[string]GuardPack{
	"secrets": {
		ID:               "secrets",
		Title:            "Secrets & supply-chain patterns",
		Summary:          "Detects AWS/GitHub/Slack-style secrets, :latest tags, ENV+SECRET, curl|sh — byte mode",
		WasmRelPath:      "tasks/artifacts/security/rust_tracefuse_detector_bytes_guard.wasm",
		SourceRelPath:    "tasks/sources/security/rust_tracefuse_detector_bytes_guard.rs",
		InputMode:        "bytes",
		MaxInputBytes:    fuzzengine.DefaultMaxInputBytesStd,
		Guided:           true,
		WasmEdgeCoverage: true,
		MutationRounds:   6,
		SeedByteCorpus: []any{
			"AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE",
			"GITHUB_PAT=ghp_FAKEEXAMPLETOKENX1234567890123456789",
			"FROM node:latest",
			"ENV API_SECRET=FAKE_EXAMPLE_DOCKER_SECRET_DO_NOT_USE",
			"RUN curl -fsSL https://example.invalid/install-FAKE.sh | sh",
		},
		DefaultPackage: "audit",
		ScanRuns:       48,
		ScanSeconds:    600,
		AuditRuns:      256,
		AuditSeconds:   28800,
		DeepRuns:       1024,
		DeepSeconds:    86400,
		ExplainHints: []ExplainHint{
			{Contains: "AKIA", Message: "Looks like an AWS access key id — rotate keys; scrub .env / CI secrets / committed configs."},
			{Contains: "ASIA", Message: "Looks like a temporary AWS key prefix — check cloud credential leakage."},
			{Contains: "ghp_", Message: "Looks like a GitHub PAT — revoke the token; search git history and CI logs."},
			{Contains: "github_pat_", Message: "Looks like a fine-grained GitHub PAT — revoke and rotate."},
			{Contains: ":latest", Message: "Floating :latest image tag — pin digests/versions for supply-chain hygiene."},
			{Contains: "SECRET", Message: "ENV/config mentions SECRET — avoid baking secrets into images or compose files."},
			{Contains: "| sh", Message: "Pipe-to-shell install pattern — prefer pinned, reviewed installers."},
			{Contains: "sk_live_", Message: "Looks like a live Stripe secret key — rotate immediately."},
			{Contains: "xoxb-", Message: "Looks like a Slack bot token — revoke in Slack admin."},
		},
	},
	"script_bounds": {
		ID:               "script_bounds",
		Title:            "Script push bounds (consensus-class)",
		Summary:          "OP_PUSHDATA1 with claimed length > 520 — packed u64 layout (Bitcoin-style property)",
		WasmRelPath:      "tasks/artifacts/security/rust_script_push_bounds_guard.wasm",
		SourceRelPath:    "tasks/sources/security/rust_script_push_bounds_guard.rs",
		InputMode:        "u64",
		Guided:           true,
		WasmEdgeCoverage: true,
		MutationRounds:   4,
		SeedCorpusU64:    []any{uint64(0), uint64(1), uint64(0x4c | (521 << 8))},
		DefaultPackage:   "audit",
		ScanRuns:         64,
		ScanSeconds:      900,
		AuditRuns:        256,
		AuditSeconds:     28800,
		DeepRuns:         2048,
		DeepSeconds:      86400,
		ExplainHints: []ExplainHint{
			{Contains: "0x4c", Message: "Script push bound violation class — oversized push claim; validate against your script/consensus path."},
			{Contains: "script", Message: "Consensus-style push bound hit — reproduce with repro_cmd before claiming a protocol bug."},
			{Contains: "push", Message: "Push-size property flagged — check OP_PUSHDATA handling and MAX_SCRIPT_ELEMENT_SIZE."},
		},
	},
	"filter_utf8": {
		ID:               "filter_utf8",
		Title:            "Malformed filter / UTF-8 skew",
		Summary:          "Catches invalid-UTF-8 + operator index skew (FluxTap-class display filter panic)",
		WasmRelPath:      "tasks/artifacts/security/rust_fluxtap_filter_bytes_guard.wasm",
		SourceRelPath:    "tasks/sources/security/rust_fluxtap_filter_bytes_guard.rs",
		InputMode:        "bytes",
		MaxInputBytes:    fuzzengine.DefaultMaxInputBytesStd,
		Guided:           true,
		WasmEdgeCoverage: true,
		MutationRounds:   2,
		SeedByteCorpus: []any{
			"c73d", // \xc7=
			"3d",   // =
			"213d", // !=
		},
		DefaultPackage: "audit",
		ScanRuns:       32,
		ScanSeconds:    600,
		AuditRuns:      128,
		AuditSeconds:   14400,
		DeepRuns:       512,
		DeepSeconds:    43200,
		ExplainHints: []ExplainHint{
			{Contains: "\xc7=", Message: "Invalid UTF-8 before '=' — ToLower length skew can panic parsers; validate filter/expr parsers on non-UTF-8."},
			{Contains: "c73d", Message: "Invalid UTF-8 before '=' — ToLower length skew can panic parsers; validate filter/expr parsers on non-UTF-8."},
			{Contains: "=", Message: "Bare or malformed operator expression — harden display-filter / query parsers against short/invalid inputs."},
		},
	},
	"parser_expat": {
		ID:             "parser_expat",
		Title:          "Parser · expat (XML)",
		Summary:        "Byte corpus + parser token dict for expat-class XML fuzz; native ASAN primary, WASM portable verify",
		WasmRelPath:    "tasks/artifacts/security/rust_parser_expat_bytes_guard.wasm",
		SourceRelPath:  "tasks/sources/security/rust_parser_expat_bytes_guard.rs",
		InputMode:      "bytes",
		MaxInputBytes:  fuzzengine.DefaultMaxInputBytesStd,
		Guided:         false,
		MutationRounds: 8,
		SeedByteCorpus: []any{
			"<?xml version=\"1.0\"?><root/>",
			"<root><child/></root>",
			"<![CDATA[test]]>",
			"&lt;entity&gt;",
		},
		DefaultPackage: "deep",
		ScanRuns:       32,
		AuditRuns:      128,
		DeepRuns:       512,
		ExplainHints: []ExplainHint{
			{Contains: "xml", Message: "XML parser signal — reproduce with native ASAN harness; WASM is portable verify only."},
			{Contains: "CDATA", Message: "CDATA section edge — confirm on native expat before claiming parser bug."},
		},
	},
}

// ParserPackTargets maps parser pack ids to native upstream keys (native repro bridge).
var ParserPackTargets = map[string]string{
	"parser_expat": "expat",
	"parser_md4c":  "md4c",
	"parser_cjson": "cjson",
}

func GuardPackFor(name string) (GuardPack, error) {
	key := strings.TrimSpace(strings.ToLower(name))
	switch key {
	case "secret", "tracefuse", "supply-chain", "supply_chain":
		key = "secrets"
	case "script", "script_push", "bitcoin_script", "bounds":
		key = "script_bounds"
	case "filter", "fluxtap", "utf8", "display_filter":
		key = "filter_utf8"
	case "parser", "expat", "xml_parser":
		key = "parser_expat"
	}
	p, ok := guardPacks[key]
	if !ok {
		return GuardPack{}, fmt.Errorf("unknown pack %q (use: secrets, script_bounds, filter_utf8, parser_expat)", name)
	}
	return p, nil
}

// ListGuardPacks returns packs in stable order.
func ListGuardPacks() []GuardPack {
	order := []string{"secrets", "script_bounds", "filter_utf8", "parser_expat"}
	out := make([]GuardPack, 0, len(order))
	for _, id := range order {
		out = append(out, guardPacks[id])
	}
	return out
}

// ResolvePackWasm returns absolute wasm path, building from source if missing.
func ResolvePackWasm(repoRoot string, p GuardPack) (string, error) {
	wasm := filepath.Join(repoRoot, p.WasmRelPath)
	if _, err := os.Stat(wasm); err == nil {
		return wasm, nil
	}
	src := filepath.Join(repoRoot, p.SourceRelPath)
	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("pack %s: missing wasm %s and source %s", p.ID, wasm, src)
	}
	if err := os.MkdirAll(filepath.Dir(wasm), 0o755); err != nil {
		return "", err
	}
	// Caller may build via rustc; return expected path after ensuring source exists.
	return wasm, fmt.Errorf("pack %s: wasm not built — run: rustc --target wasm32-unknown-unknown -O --crate-type=cdylib %s -o %s", p.ID, src, wasm)
}

// AdjustPackageForPack applies pack-aware budget presets on top of scan|audit|deep.
func AdjustPackageForPack(pkg B2BPackage, p GuardPack) B2BPackage {
	switch pkg.Name {
	case "scan":
		if p.ScanRuns > 0 {
			pkg.BudgetRuns = p.ScanRuns
		}
		if p.ScanSeconds > 0 {
			pkg.BudgetSeconds = p.ScanSeconds
		}
	case "audit":
		if p.AuditRuns > 0 {
			pkg.BudgetRuns = p.AuditRuns
		}
		if p.AuditSeconds > 0 {
			pkg.BudgetSeconds = p.AuditSeconds
		}
	case "deep":
		if p.DeepRuns > 0 {
			pkg.BudgetRuns = p.DeepRuns
		}
		if p.DeepSeconds > 0 {
			pkg.BudgetSeconds = p.DeepSeconds
		}
		if p.InputMode == "bytes" {
			pkg.CoverageGuided = true
		}
	}
	// Pack mutation_rounds wins when package has no override; deep keeps heavier if already set higher.
	if p.MutationRounds > 0 {
		if pkg.MutationRounds <= 0 || p.MutationRounds > pkg.MutationRounds {
			pkg.MutationRounds = p.MutationRounds
		}
	}
	if p.InputMode == "bytes" {
		pkg.SignalTypes = appendUniqueSignals(pkg.SignalTypes, "byte_corpus", "guard_pack:"+p.ID)
	} else {
		pkg.SignalTypes = appendUniqueSignals(pkg.SignalTypes, "guard_pack:"+p.ID)
	}
	pkg.Summary = fmt.Sprintf("%s · pack %s (%s)", pkg.Summary, p.ID, p.InputMode)
	return pkg
}

func appendUniqueSignals(base []string, add ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(base)+len(add))
	for _, s := range base {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, s := range add {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// ApplyPackConfig merges pack settings into a campaign config map.
func ApplyPackConfig(cfg map[string]any, p GuardPack) map[string]any {
	if cfg == nil {
		cfg = map[string]any{}
	}
	cfg["guard_pack"] = p.ID
	cfg["guard_name"] = p.ID
	cfg["check_semantics"] = "detector"
	if p.InputMode == "bytes" {
		cfg["input_mode"] = "bytes"
		if p.MaxInputBytes > 0 {
			cfg["max_input_bytes"] = p.MaxInputBytes
		}
		if len(p.SeedByteCorpus) > 0 {
			cfg["seed_byte_corpus"] = p.SeedByteCorpus
		}
	} else {
		cfg["input_mode"] = "u64"
		if len(p.SeedCorpusU64) > 0 {
			cfg["seed_corpus"] = p.SeedCorpusU64
		}
	}
	if p.Guided {
		cfg["guided_scheduling"] = true
	}
	if p.WasmEdgeCoverage {
		cfg["coverage_kind"] = fuzzengine.CoverageKindWasmEdgeBitmap
	}
	if p.MutationRounds > 0 {
		cfg["mutation_rounds"] = p.MutationRounds
	}
	cfg["stable_crash_buckets"] = true
	if strings.HasPrefix(p.ID, "parser_") {
		cfg["pack_role"] = "parser"
		cfg["native_repro_enabled"] = true
		cfg["bounty_requires_native"] = true
		cfg["native_repro_mode"] = "oss_upstream"
		cfg["depth_tier"] = string(fuzzengine.DepthBytesCorpus)
		if target, ok := ParserPackTargets[p.ID]; ok {
			cfg["parser_target"] = target
			cfg["upstream_target"] = target
		}
		cfg["coverage_kind"] = "input_fingerprint"
	}
	return cfg
}

// ExplainPackFinding returns a customer-facing note for a finding preview.
func ExplainPackFinding(packID, inputPreview, title string) string {
	p, err := GuardPackFor(packID)
	if err != nil {
		return "Detector flagged this input — reproduce locally, then fix the matching rule in your project."
	}
	hay := inputPreview + " " + title
	if decoded, err := hex.DecodeString(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(inputPreview)), "0x")); err == nil && len(decoded) > 0 {
		hay += " " + string(decoded)
	}
	hayLower := strings.ToLower(hay)
	for _, h := range p.ExplainHints {
		if h.Contains == "" {
			continue
		}
		if strings.Contains(hay, h.Contains) || strings.Contains(hayLower, strings.ToLower(h.Contains)) {
			return h.Message
		}
	}
	return fmt.Sprintf("[%s] Detector flagged this input — use repro to locate the matching pattern in configs/tests.", p.ID)
}
