package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFluxtapFilterGuardPOC(t *testing.T) {
	wasm := filepath.Join("..", "..", "tasks", "artifacts", "security", "rust_fluxtap_filter_bytes_guard.wasm")
	raw, err := os.ReadFile(wasm)
	if err != nil {
		t.Skip("fluxtap guard wasm not built:", err)
	}
	ctx := context.Background()
	if err := ValidateCheckWasm(ctx, raw); err != nil {
		t.Fatal(err)
	}
	res, err := InvokeCheckOutcomeInput(ctx, raw, []byte("\xc7="))
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatal("expected guard to flag FluxTap POC \\xc7=")
	}
	if !BitmapHasSignal(res.EdgeBitmap) {
		t.Fatal("expected wasm edge bitmap on filter_utf8 hit")
	}
	ok, err := InvokeCheckInput(ctx, raw, []byte("dns"))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("benign filter should not hit")
	}
}

func TestParserExpatPortableGuard(t *testing.T) {
	wasm := filepath.Join("..", "..", "tasks", "artifacts", "security", "rust_parser_expat_bytes_guard.wasm")
	raw, err := os.ReadFile(wasm)
	if err != nil {
		t.Skip("parser expat wasm not built:", err)
	}
	ctx := context.Background()
	if err := ValidateCheckWasm(ctx, raw); err != nil {
		t.Fatal(err)
	}
	hit, err := InvokeCheckInput(ctx, raw, []byte("<root><child"))
	if err != nil {
		t.Fatal(err)
	}
	if !hit {
		t.Fatal("expected unclosed tag to hit portable guard")
	}
	ok, err := InvokeCheckInput(ctx, raw, []byte(`<?xml version="1.0"?><root/>`))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("well-formed minimal XML should not hit portable guard")
	}
}

func TestScriptPushKnownViolation(t *testing.T) {
	wasm := filepath.Join("..", "..", "tasks", "artifacts", "security", "rust_script_push_bounds_guard.wasm")
	raw, err := os.ReadFile(wasm)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	violation := uint64(0x4c | (521 << 8))
	res, err := InvokeCheckOutcome(ctx, raw, violation)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("expected script push violation for input 0x%x", violation)
	}
	if !BitmapHasSignal(res.EdgeBitmap) {
		t.Fatal("expected wasm edge bitmap on script_bounds violation")
	}
}
