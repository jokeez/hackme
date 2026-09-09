package sandbox

import (
	"context"
	"strings"
	"testing"
)

func TestDefaultPoHOrderGateValidAndSolvable(t *testing.T) {
	raw := DefaultPoHOrderGateWasm()
	if len(raw) < 32 {
		t.Fatalf("gate too small: %d", len(raw))
	}
	if err := ValidateCheckWasm(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	hex := DefaultPoHOrderGateWasmHex()
	if IsMinimalPoHGate(hex) {
		t.Fatal("order gate must not look like minimal")
	}
	// Random-ish nonce: packed economics usually invalid → check returns 1 → PoH accept.
	ok, err := InvokeCheck(context.Background(), raw, 0xdeadbeefcafe)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("typical nonce should pass PoH order gate")
	}
}

func TestResolvePoHOrderGateIgnoresDigAndMinimal(t *testing.T) {
	want := DefaultPoHOrderGateWasmHex()
	got, src := ResolvePoHOrderGateWasmHex(map[string]any{
		"wasm_check_hex": MinimalGateWasmHex,
	})
	if src != "default_order_gate" || !strings.EqualFold(got, want) {
		t.Fatalf("got src=%s len=%d", src, len(got))
	}
	got, src = ResolvePoHOrderGateWasmHex(map[string]any{
		"poh_wasm_check_hex": want,
	})
	if src != "poh_wasm_check_hex" || !strings.EqualFold(got, want) {
		t.Fatalf("explicit failed src=%s", src)
	}
	got, src = ResolvePoHOrderGateWasmHex(map[string]any{
		"poh_use_campaign_wasm": true,
		"wasm_check_hex":        MinimalGateWasmHex,
	})
	if src != "campaign_wasm_opt_in" {
		t.Fatalf("opt-in src=%s", src)
	}
	_ = got
}
