package sandbox

import (
	_ "embed"
	"encoding/hex"
	"strings"
)

//go:embed embed/upstream_hackme_order_gate.wasm
var pohOrderGateWASM []byte

// DefaultPoHOrderGateWasm is the solvable PoH order gate (economics floor check on nonce bits).
// Dig detector WASM must not be used as a PoH gate — it rejects almost all nonces.
func DefaultPoHOrderGateWasm() []byte {
	out := make([]byte, len(pohOrderGateWASM))
	copy(out, pohOrderGateWASM)
	return out
}

// DefaultPoHOrderGateWasmHex returns lowercase hex for manifests / attach.
func DefaultPoHOrderGateWasmHex() string {
	return hex.EncodeToString(pohOrderGateWASM)
}

// IsMinimalPoHGate reports always-pass demo check(i64)->i32 modules.
func IsMinimalPoHGate(wasmHex string) bool {
	h := strings.TrimSpace(strings.ToLower(wasmHex))
	if h == "" {
		return true
	}
	return h == strings.ToLower(MinimalGateWasmHex)
}

// ResolvePoHOrderGateWasmHex picks WASM for coordinator→chain PoH attach.
// Explicit poh_wasm_check_hex wins; otherwise the dedicated order gate is used
// (never Dig detector wasm_check_hex, never silent MinimalGate).
func ResolvePoHOrderGateWasmHex(cfg map[string]any) (wasmHex string, source string) {
	if cfg != nil {
		if v, ok := cfg["poh_wasm_check_hex"]; ok {
			if s, ok := v.(string); ok {
				s = strings.TrimSpace(strings.ToLower(s))
				if s != "" && !IsMinimalPoHGate(s) {
					return s, "poh_wasm_check_hex"
				}
			}
		}
		// Opt-in escape hatch for labs that intentionally want campaign wasm on PoH.
		if truthyCfg(cfg, "poh_use_campaign_wasm") {
			if v, ok := cfg["wasm_check_hex"]; ok {
				if s, ok := v.(string); ok {
					s = strings.TrimSpace(strings.ToLower(s))
					if s != "" {
						return s, "campaign_wasm_opt_in"
					}
				}
			}
		}
	}
	return DefaultPoHOrderGateWasmHex(), "default_order_gate"
}

func truthyCfg(cfg map[string]any, keys ...string) bool {
	for _, k := range keys {
		v, ok := cfg[k]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case bool:
			if t {
				return true
			}
		case string:
			s := strings.TrimSpace(strings.ToLower(t))
			if s == "1" || s == "true" || s == "yes" || s == "on" {
				return true
			}
		case float64:
			if t != 0 {
				return true
			}
		case int:
			if t != 0 {
				return true
			}
		}
	}
	return false
}
