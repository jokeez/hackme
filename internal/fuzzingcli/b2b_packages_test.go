package fuzzingcli

import (
	"testing"

	"hackme/internal/fuzzengine"
)

func TestB2BPackageFor(t *testing.T) {
	p, err := B2BPackageFor("audit")
	if err != nil {
		t.Fatal(err)
	}
	if p.DepthTier != fuzzengine.DepthWasmNative || p.BudgetHMC != 5 || !p.PoolDistributed {
		t.Fatalf("audit package: %+v", p)
	}
	if p.BudgetSeconds < 3600 {
		t.Fatalf("audit should have multi-hour SLA budget_seconds, got %d", p.BudgetSeconds)
	}
	if len(p.SignalTypes) < 2 {
		t.Fatalf("audit signals=%v", p.SignalTypes)
	}
	if _, err := B2BPackageFor("nope"); err == nil {
		t.Fatal("expected error for unknown package")
	}
	if p, err := B2BPackageFor("pro"); err != nil || p.Name != "audit" {
		t.Fatalf("pro alias: %+v err=%v", p, err)
	}
}

func TestB2BPackagesHonestDepth(t *testing.T) {
	scan, _ := B2BPackageFor("scan")
	audit, _ := B2BPackageFor("audit")
	deep, _ := B2BPackageFor("deep")
	if scan.DepthTier == audit.DepthTier || audit.DepthTier == deep.DepthTier {
		t.Fatal("packages must use distinct depth tiers")
	}
	if deep.DepthTier != fuzzengine.DepthBytesCorpus {
		t.Fatalf("deep tier=%s", deep.DepthTier)
	}
	if deep.BudgetRuns <= audit.BudgetRuns || deep.BudgetSeconds <= audit.BudgetSeconds {
		t.Fatalf("deep must exceed audit on runs and wall clock: deep=%+v audit=%+v", deep, audit)
	}
	if deep.MutationRounds < 8 || !deep.CoverageGuided {
		t.Fatalf("deep mutation/coverage: %+v", deep)
	}
	if scan.CreatePoHOrder || !audit.CreatePoHOrder || !deep.CreatePoHOrder {
		t.Fatalf("PoH flags scan=%v audit=%v deep=%v", scan.CreatePoHOrder, audit.CreatePoHOrder, deep.CreatePoHOrder)
	}
}

func TestIsLoopbackBase(t *testing.T) {
	if !IsLoopbackBase("http://127.0.0.1:8080") {
		t.Fatal("127.0.0.1 should be loopback")
	}
	if IsLoopbackBase("https://hackme.tech") {
		t.Fatal("hackme.tech should not be loopback")
	}
}
