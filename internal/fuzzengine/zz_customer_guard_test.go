package fuzzengine

import "testing"

func TestCustomerGuardViolationIsAFinding(t *testing.T) {
	ft, sev := CustomerGuardFinding(map[string]any{"customer_guard": true}, "security_violation", "high")
	if ft != "guard_violation" || sev != "high" || !IsCrashClass(ft) || IsCoverageNoise(ft) {
		t.Fatalf("customer guard violation must be a top finding, got %s/%s", ft, sev)
	}
	if k := StableFindingKeyFromDetail(ft, map[string]any{"op_type": 3, "item_id": 9}); k != "guard_violation|guard" {
		t.Fatalf("one guard = one family, got %q", k)
	}
	// Shipped detector packs keep violations in the coverage-noise appendix.
	if ft, _ := CustomerGuardFinding(map[string]any{"guard_pack": "secrets"}, "security_violation", "high"); ft != "security_violation" || !IsCoverageNoise(ft) {
		t.Fatalf("pack violation must stay noise, got %s", ft)
	}
}
