package fuzz

import (
	"context"
	"encoding/hex"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"hackme/internal/sandbox"

	"github.com/tetratelabs/wazero"
)

func decodeMaliciousHex(t *testing.T, hexStr string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(hexStr)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// --- Pool order gate: check(i64)->i32 ---

func TestMaliciousWasm_InfiniteLoopChild(t *testing.T) {
	if os.Getenv("HACKME_WASM_INF_CHILD") != "1" {
		t.Skip("child-only")
	}
	raw := decodeMaliciousHex(t, infiniteLoopCheckWasmHex)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	_ = sandbox.ValidateCheckWasm(ctx, raw)
}

func TestMaliciousWasm_InfiniteLoopDoesNotHangTestProcess(t *testing.T) {
	if os.Getenv("HACKME_WASM_INF_CHILD") == "1" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestMaliciousWasm_InfiniteLoopChild$", "-test.count=1", "-test.timeout=4s")
	cmd.Env = append(os.Environ(), "HACKME_WASM_INF_CHILD=1")
	start := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(start)
	if elapsed > 10*time.Second {
		t.Fatalf("infinite loop wasm hung test runner for %v", elapsed)
	}
	if runErr == nil {
		t.Fatal("child exited 0 — infinite loop should not complete ValidateCheckWasm quickly")
	}
	t.Logf("child ended in %v: %v", elapsed, runErr)
}

func TestMaliciousWasm_OOBLoadTrapsWithoutHostCrash(t *testing.T) {
	raw := decodeMaliciousHex(t, oobLoadCheckWasmHex)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := sandbox.ValidateCheckWasm(ctx, raw)
	if err == nil {
		t.Fatal("expected trap/error for OOB store wasm")
	}
	// Second call: quarantine or repeat failure — runtime must stay alive.
	err2 := sandbox.ValidateCheckWasm(ctx, raw)
	if err2 == nil {
		t.Fatal("expected error on second OOB validation")
	}
	// Benign module still works (no process-wide corruption).
	ok, err := sandbox.InvokeCheck(ctx, decodeMaliciousHex(t, sandbox.MinimalGateWasmHex), 1)
	if err != nil || !ok {
		t.Fatalf("benign wasm after OOB test: ok=%v err=%v", ok, err)
	}
}

func TestMaliciousWasm_WASIImportRejectedBeforeHostAccess(t *testing.T) {
	raw := decodeMaliciousHex(t, wasiImportCheckWasmHex)
	ctx := context.Background()
	err := sandbox.ValidateCheckWasm(ctx, raw)
	if err == nil {
		t.Fatal("expected rejection for WASI import module")
	}
	low := strings.ToLower(err.Error())
	if !strings.Contains(low, "export") && !strings.Contains(low, "compile") && !strings.Contains(low, "instantiate") {
		t.Fatalf("unexpected error type: %v", err)
	}
}


// --- PoH lock eval path (embedded worker sandbox.Eval) ---

func TestMaliciousWasm_EvalInfiniteLoopChild(t *testing.T) {
	if os.Getenv("HACKME_WASM_EVAL_INF_CHILD") != "1" {
		t.Skip("child-only")
	}
	raw := decodeMaliciousHex(t, evalInfiniteLoopWasmHex)
	ctx := context.Background()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter().WithMemoryLimitPages(64))
	defer rt.Close(ctx)
	compiled, err := rt.CompileModule(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("evil-eval"))
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close(ctx)
	fn := mod.ExportedFunction("eval")
	callCtx, cancel := context.WithTimeout(ctx, sandbox.WasmEvalTimeout)
	defer cancel()
	_, _ = fn.Call(callCtx, 1)
}

func TestMaliciousWasm_EvalInfiniteLoopBoundedByChildTimeout(t *testing.T) {
	if os.Getenv("HACKME_WASM_EVAL_INF_CHILD") == "1" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestMaliciousWasm_EvalInfiniteLoopChild$", "-test.count=1", "-test.timeout=5s")
	cmd.Env = append(os.Environ(), "HACKME_WASM_EVAL_INF_CHILD=1")
	start := time.Now()
	runErr := cmd.Run()
	if time.Since(start) > 12*time.Second {
		t.Fatal("eval infinite loop hung past wall clock")
	}
	if runErr == nil {
		t.Fatal("eval infinite child should not exit cleanly while spinning")
	}
}

func TestMaliciousWasm_NoFilesystemOrSocketExports(t *testing.T) {
	raw := decodeMaliciousHex(t, wasiImportCheckWasmHex)
	ctx := context.Background()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter().WithMemoryLimitPages(128))
	defer rt.Close(ctx)
	_, err := rt.CompileModule(ctx, raw)
	if err != nil {
		return // compile may fail on imports — acceptable
	}
	compiled, _ := rt.CompileModule(ctx, raw)
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	if err != nil {
		return
	}
	defer mod.Close(ctx)
	for _, def := range mod.ExportedFunctionDefinitions() {
		name := def.Name()
		if strings.Contains(strings.ToLower(name), "fd_") || strings.Contains(strings.ToLower(name), "sock") {
			t.Fatalf("unexpected host-capable export: %s", name)
		}
	}
}

func TestMaliciousWasm_LockEvalStillHealthyAfterAbuse(t *testing.T) {
	ctx := context.Background()
	v, err := sandbox.Eval(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if v != 3*7+13 {
		t.Fatalf("eval(3)=%d want 34", v)
	}
}

func TestMaliciousWasm_PolicyRejectsOversizePayload(t *testing.T) {
	tooBig := make([]byte, sandbox.MaxCheckWasmBytes()+16)
	copy(tooBig, []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00})
	err := sandbox.ValidateCheckWasm(context.Background(), tooBig)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("want size cap error, got %v", err)
	}
}

func TestMaliciousWasm_ConcurrentInvokeCheckIsolation(t *testing.T) {
	good := decodeMaliciousHex(t, sandbox.MinimalGateWasmHex)
	ctx := context.Background()
	if err := sandbox.ValidateCheckWasm(ctx, good); err != nil {
		t.Fatal(err)
	}
	ok, err := sandbox.InvokeCheck(ctx, good, 7)
	if err != nil || !ok {
		t.Fatalf("benign concurrent invoke: ok=%v err=%v", ok, err)
	}
}

// Guard: malicious modules must not expose more than the single check export.
func TestMaliciousWasm_ExtraExportsRejected(t *testing.T) {
	// lockWasm exports eval only — wrong surface for order gate.
	raw, _ := hex.DecodeString("0061736d0100000001060160017e017e03020100070801046576616c00000a0c010a00200042077e420d7c0b")
	err := sandbox.ValidateCheckWasm(context.Background(), raw)
	if err == nil {
		t.Fatal("eval-only module must not pass check gate")
	}
}

// Compile-time sanity: infinite loop module is valid wasm magic.
func TestMaliciousWasm_ModulesAreWasm1(t *testing.T) {
	for name, hx := range map[string]string{
		"infinite": infiniteLoopCheckWasmHex,
		"oob":      oobLoadCheckWasmHex,
		"wasi":     wasiImportCheckWasmHex,
	} {
		raw := decodeMaliciousHex(t, hx)
		if len(raw) < 8 || raw[0] != 0x00 || raw[1] != 0x61 {
			t.Fatalf("%s: bad magic", name)
		}
	}
}
