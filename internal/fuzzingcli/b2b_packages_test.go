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
	if _, err := B2BPackageFor("nope"); err == nil {
		t.Fatal("expected error for unknown package")
	}
	if p, err := B2BPackageFor("pro"); err != nil || p.Name != "audit" {
		t.Fatalf("pro alias: %+v err=%v", p, err)
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
