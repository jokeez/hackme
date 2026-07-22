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

func TestValidateCheckWasmRejectsStartSection(t *testing.T) {
	// wat2wasm: (module (func $s (loop br 0)) (start $s) (func (export "check") (param i64) (result i32) i32.const 0))
	const withStart = "0061736d0100000001090260000060017e017f030302000107090105636865636b00010801000a0e02070003400c000b0b040041000b"
	raw, err := hex.DecodeString(withStart)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCheckWasm(context.Background(), raw); err == nil {
		t.Fatal("expected start section rejection")
	} else if !strings.Contains(err.Error(), "start section") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCheckWasmRejectsHugeTable(t *testing.T) {
	// (module (table 1000000 funcref) (func (export "check") (param i64) (result i32) i32.const 1))
	// table section: count=1, funcref, flags=0, min=1000000 — must fail before CompileModule (H44).
	const withHugeTable = "0061736d0100000001060160017e017f030201000406017000c0843d07090105636865636b00000a0601040041010b"
	raw, err := hex.DecodeString(withHugeTable)
	if err != nil {
		t.Fatal(err)
	}
	if err := rejectWasmHostileSections(raw); err == nil {
		t.Fatal("expected huge table rejection")
	} else if !strings.Contains(err.Error(), "table") {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidateCheckWasm(context.Background(), raw); err == nil {
		t.Fatal("expected ValidateCheckWasm to reject huge table")
	}
}

func TestRejectWasmHostileSectionsAllowsSmallRustTable(t *testing.T) {
	// table funcref min=1 max=1 (typical rustc wasm32 cdylib)
	const smallTable = "0061736d0100000001060160017e017f030201000405017001010107090105636865636b00000a0601040041010b"
	raw, err := hex.DecodeString(smallTable)
	if err != nil {
		t.Fatal(err)
	}
	if err := rejectWasmHostileSections(raw); err != nil {
		t.Fatal(err)
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
