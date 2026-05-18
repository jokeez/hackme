package sandbox

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"
)

func TestMinimalCheckWasm(t *testing.T) {
	raw, err := hex.DecodeString(MinimalGateWasmHex)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCheckWasm(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ok, err := InvokeCheck(ctx, raw, 999)
	if err != nil || !ok {
		t.Fatalf("InvokeCheck(999)=%v,%v", ok, err)
	}
}

func TestValidateCheckWasmRejectsEvalModule(t *testing.T) {
	raw, err := hex.DecodeString(lockWasmHex) // exports eval, not check
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCheckWasm(context.Background(), raw); err == nil {
		t.Fatal("expected missing check export error")
	}
}

func TestValidateCheckWasmRejectsTooLarge(t *testing.T) {
	tooBig := make([]byte, MaxCheckWasmBytes()+1)
	if err := ValidateCheckWasm(context.Background(), tooBig); err == nil {
		t.Fatal("expected wasm too large error")
	}
}

func TestValidateCheckWasmRejectsInvalidHeader(t *testing.T) {
	bad := []byte{0x00, 0x61, 0x73, 0x6d, 0x02, 0x00, 0x00, 0x00}
	if err := ValidateCheckWasm(context.Background(), bad); err == nil {
		t.Fatal("expected invalid wasm magic/version")
	}
}

func TestValidateCheckWasmQuarantine(t *testing.T) {
	raw, err := hex.DecodeString(lockWasmHex) // invalid for check gate
	if err != nil {
		t.Fatal(err)
	}
	_ = ValidateCheckWasm(context.Background(), raw) // first failure marks quarantine
	err = ValidateCheckWasm(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "quarantined") {
		t.Fatalf("expected quarantined error, got %v", err)
	}
}
