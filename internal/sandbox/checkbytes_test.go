package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateCheckBytesGuardWasm(t *testing.T) {
	path := filepath.Join("..", "..", "tasks", "artifacts", "security", "rust_tracefuse_detector_bytes_guard.wasm")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skip("bytes guard wasm not built:", err)
	}
	ctx := context.Background()
	if err := ValidateCheckWasm(ctx, raw); err != nil {
		t.Fatal(err)
	}
	line := []byte("AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE")
	ok, err := InvokeCheckInput(ctx, raw, line)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected AKIA line to trigger detector")
	}
	ok, err = InvokeCheckInput(ctx, raw, []byte("hello world"))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("benign input should not trigger")
	}
}

func TestMaxCheckInputBytesClamp(t *testing.T) {
	max := MaxCheckInputBytes()
	big := make([]byte, max+100)
	cl := clampCheckInput(big)
	if len(cl) != max {
		t.Fatalf("clamp: got %d want %d", len(cl), max)
	}
}

func TestInvokeCheckInputRejectsOversize(t *testing.T) {
	path := filepath.Join("..", "..", "tasks", "artifacts", "security", "rust_tracefuse_detector_bytes_guard.wasm")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skip("bytes guard wasm not built:", err)
	}
	ctx := context.Background()
	if err := ValidateCheckWasm(ctx, raw); err != nil {
		t.Fatal(err)
	}
	huge := make([]byte, MaxCheckInputBytes()+500)
	for i := range huge {
		huge[i] = 'A'
	}
	_, err = InvokeCheckInput(ctx, raw, huge)
	if err != nil {
		t.Fatal(err)
	}
}
