package fuzzingcli

import (
	"strings"
	"testing"
)

func TestGuardPackForAliases(t *testing.T) {
	p, err := GuardPackFor("tracefuse")
	if err != nil || p.ID != "secrets" {
		t.Fatalf("tracefuse -> secrets: %+v err=%v", p, err)
	}
	p, err = GuardPackFor("fluxtap")
	if err != nil || p.ID != "filter_utf8" {
		t.Fatalf("fluxtap -> filter_utf8: %+v err=%v", p, err)
	}
}

func TestApplyPackConfigSecrets(t *testing.T) {
	p, _ := GuardPackFor("secrets")
	cfg := ApplyPackConfig(nil, p)
	if cfg["input_mode"] != "bytes" {
		t.Fatalf("input_mode=%v", cfg["input_mode"])
	}
	if cfg["guided_scheduling"] != true {
		t.Fatal("expected guided")
	}
	if cfg["guard_pack"] != "secrets" {
		t.Fatalf("guard_pack=%v", cfg["guard_pack"])
	}
	if cfg["coverage_kind"] != "wasm_edge_bitmap" {
		t.Fatalf("coverage_kind=%v", cfg["coverage_kind"])
	}
}

func TestApplyPackConfigInstrumentedPacks(t *testing.T) {
	for _, id := range []string{"filter_utf8", "script_bounds"} {
		p, err := GuardPackFor(id)
		if err != nil {
			t.Fatal(err)
		}
		if !p.WasmEdgeCoverage {
			t.Fatalf("%s: WasmEdgeCoverage=false", id)
		}
		cfg := ApplyPackConfig(nil, p)
		if cfg["coverage_kind"] != "wasm_edge_bitmap" {
			t.Fatalf("%s coverage_kind=%v", id, cfg["coverage_kind"])
		}
	}
}

func TestExplainPackFinding(t *testing.T) {
	msg := ExplainPackFinding("secrets", "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE", "")
	if !strings.Contains(msg, "AWS") {
		t.Fatalf("explain=%q", msg)
	}
}

func TestAdjustPackageForPackBudgets(t *testing.T) {
	p, err := GuardPackFor("filter_utf8")
	if err != nil {
		t.Fatal(err)
	}
	audit, err := B2BPackageFor("audit")
	if err != nil {
		t.Fatal(err)
	}
	adj := AdjustPackageForPack(audit, p)
	if adj.BudgetRuns != p.AuditRuns {
		t.Fatalf("audit runs=%d want %d", adj.BudgetRuns, p.AuditRuns)
	}
	if adj.BudgetRuns >= audit.BudgetRuns {
		t.Fatalf("filter_utf8 audit should be lighter than default audit (%d >= %d)", adj.BudgetRuns, audit.BudgetRuns)
	}
	secrets, _ := GuardPackFor("secrets")
	scan, _ := B2BPackageFor("scan")
	adjScan := AdjustPackageForPack(scan, secrets)
	if adjScan.BudgetRuns != secrets.ScanRuns {
		t.Fatalf("secrets scan runs=%d want %d", adjScan.BudgetRuns, secrets.ScanRuns)
	}
}

func TestGuardPackForParserAlias(t *testing.T) {
	p, err := GuardPackFor("expat")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "parser_expat" {
		t.Fatalf("got %q", p.ID)
	}
	cfg := ApplyPackConfig(nil, p)
	if cfg["pack_role"] != "parser" {
		t.Fatalf("pack_role=%v", cfg["pack_role"])
	}
	if cfg["bounty_requires_native"] != true {
		t.Fatal("parser pack requires native bounty")
	}
	if cfg["native_repro_mode"] != "oss_upstream" {
		t.Fatalf("native_repro_mode=%v", cfg["native_repro_mode"])
	}
	if cfg["upstream_target"] != "expat" {
		t.Fatalf("upstream_target=%v", cfg["upstream_target"])
	}
	if p.WasmRelPath == "" {
		t.Fatal("parser pack needs portable wasm path")
	}
}

func TestListGuardPacksIncludesScanSmokes(t *testing.T) {
	packs := ListGuardPacks()
	ids := map[string]bool{}
	for _, p := range packs {
		ids[p.ID] = true
	}
	for _, id := range []string{"bounds_smoke", "overflow_smoke", "state_smoke"} {
		if !ids[id] {
			t.Fatalf("missing scan pack %q in %d packs", id, len(packs))
		}
	}
}

func TestExplainPackFindingHex(t *testing.T) {
	msg := ExplainPackFinding("filter_utf8", "c73d", "")
	if !strings.Contains(msg, "UTF-8") && !strings.Contains(msg, "ToLower") {
		t.Fatalf("explain=%q", msg)
	}
}
