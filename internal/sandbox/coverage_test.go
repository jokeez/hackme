package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInvokeCheckOutcomeReadsWasmEdgeBitmap(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "tasks/sources/security/rust_tracefuse_detector_bytes_guard.rs")
	wasmPath := filepath.Join(t.TempDir(), "cov_guard.wasm")
	cmd := exec.Command("rustc", "--target", "wasm32-unknown-unknown", "-O", "--crate-type=cdylib", src, "-o", wasmPath)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("rustc unavailable: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatal(err)
	}
	res, err := InvokeCheckOutcomeInput(context.Background(), raw, []byte("AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE"))
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatal("expected detector hit")
	}
	if !BitmapHasSignal(res.EdgeBitmap) {
		t.Fatalf("expected wasm edge bitmap signal, got %v", res.EdgeBitmap[:16])
	}
	if PrimaryEdgeFromBitmap(res.EdgeBitmap) == 0 {
		t.Fatal("expected primary edge from bitmap")
	}
}
