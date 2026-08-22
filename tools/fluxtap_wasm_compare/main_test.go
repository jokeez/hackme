package main

import (
	"context"
	"os"
	"testing"

	"hackme/internal/sandbox"
)

func TestFilterWasmVsFluxTapInputs(t *testing.T) {
	raw, err := os.ReadFile("../../tasks/artifacts/security/rust_fluxtap_filter_bytes_guard.wasm")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	type row struct {
		label    string
		b        []byte
		wantHit  bool
		wantFlux string
	}
	rows := []row{
		{"\\xc7= PANIC class", []byte("\xc7="), true, "PANIC slice OOB in evalAtom"},
		{"bare =", []byte("="), true, "match=true, no panic"},
		{"bare !=", []byte("!="), true, "match=false"},
		{"dns", []byte("dns"), false, "match=true valid"},
		{"tcp.port == 443", []byte("tcp.port == 443"), false, "match=false valid"},
		{"http", []byte("http"), false, "match=true valid"},
	}
	t.Log("HackMe filter_utf8 WASM vs FluxTap filter (FounderB/FluxTap)")
	for _, r := range rows {
		res, err := sandbox.InvokeCheckOutcomeInput(ctx, raw, r.b)
		if err != nil {
			t.Fatalf("%s: %v", r.label, err)
		}
		if res.OK != r.wantHit {
			t.Errorf("%s: wasm hit=%v want=%v (flux: %s)", r.label, res.OK, r.wantHit, r.wantFlux)
		} else {
			t.Logf("OK %s wasm=%v bitmap=%v | FluxTap: %s", r.label, res.OK, sandbox.BitmapHasSignal(res.EdgeBitmap), r.wantFlux)
		}
	}
}
