package main

import "testing"

func TestAuthorizeFinalize(t *testing.T) {
	if err := authorizeFinalize("hunt-a", "", ""); err == nil {
		t.Fatal("bare finalize must be refused")
	}
	if err := authorizeFinalize("hunt-a", "hunt-b", ""); err == nil {
		t.Fatal("mismatched confirm flag must be refused")
	}
	if err := authorizeFinalize("hunt-a", "", "hunt-b"); err == nil {
		t.Fatal("mismatched env must be refused")
	}
	if err := authorizeFinalize("hunt-a", "hunt-a", ""); err != nil {
		t.Fatalf("matching flag: %v", err)
	}
	if err := authorizeFinalize("hunt-a", "", "hunt-a"); err != nil {
		t.Fatalf("matching env: %v", err)
	}
}
