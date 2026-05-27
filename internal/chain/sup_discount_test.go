package chain

import "testing"

func TestApplyAuditSUPDiscount(t *testing.T) {
	cash, sup := ApplyAuditSUPDiscount(10.0, 0)
	if cash != 10.0 || sup != 0 {
		t.Fatalf("no sup: cash=%v sup=%v", cash, sup)
	}
	cash, sup = ApplyAuditSUPDiscount(10.0, 2.0)
	if sup != 1.5 || cash != 8.5 {
		t.Fatalf("cap 15%%: cash=%v sup=%v", cash, sup)
	}
	cash, sup = ApplyAuditSUPDiscount(10.0, 100.0)
	if sup != 1.5 || cash != 8.5 {
		t.Fatalf("cap 15%% large balance: cash=%v sup=%v", cash, sup)
	}
}
