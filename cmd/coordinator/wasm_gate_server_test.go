package main

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hackme/internal/chain"
	"hackme/internal/sandbox"
)

func mustOrderWasmHex(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "..", "tasks", "artifacts", "security", "rust_script_push_bounds_guard.wasm")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(raw)
}

func TestWasmGateServerRejectsFabricatedPass(t *testing.T) {
	wm := &workManager{
		defaultBatch:         1000,
		targetMod:            997,
		leaseSec:             30,
		maxWorkers:           1000,
		maxActiveLeases:      1000,
		maxDedupEntries:      1000,
		active:               make(map[workKey]leaseRecord),
		worker:               make(map[string]workerPayoutStat),
		acceptedResultHashes: make(map[string]struct{}),
		acceptedFoundNonces:  make(map[uint64]struct{}),
	}
	wm.schedulerMu.Lock()
	wm.activeOrder = activeOrderSnap{
		ID:        "order-wasm-gate-rt",
		WasmHex:   mustOrderWasmHex(t),
		FetchedAt: time.Now().Unix(),
	}
	wm.schedulerMu.Unlock()

	base, size, _, _, _, okClaim, reasonClaim := wm.claim("w1", 1000)
	if !okClaim || reasonClaim != "" {
		t.Fatalf("claim failed: ok=%v reason=%q", okClaim, reasonClaim)
	}
	var foundNonce uint64
	for n := base; n < base+size; n++ {
		if chain.PohEval(n)%wm.targetMod != 0 {
			continue
		}
		raw, _ := hex.DecodeString(strings.TrimSpace(wm.activeOrder.WasmHex))
		ok, err := sandbox.InvokeCheck(context.Background(), raw, n)
		if err == nil && !ok {
			foundNonce = n
			break
		}
	}
	if foundNonce == 0 {
		t.Fatal("no poh hit with failing wasm check in range")
	}

	ok, reason, _, _, _ := wm.submit(submitWorkRequest{
		WorkerID:     "w1",
		BaseNonce:    base,
		BatchSize:    size,
		WorkID:       buildWorkID("w1", base, size),
		Found:        true,
		FoundNonce:   foundNonce,
		ResultHash:   "wasm-gate-rt-hash",
		WasmGatePass: true,
		OrderTaskID:  "order-wasm-gate-rt",
	})
	if ok || reason != "wasm_gate_server_reject" {
		t.Fatalf("fabricated wasm_gate_pass must be rejected server-side, got ok=%v reason=%q", ok, reason)
	}
}
