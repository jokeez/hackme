package main

import (
	"context"
	"fmt"
	"os"

	"github.com/tetratelabs/wazero"

	"hackme/internal/sandbox"
)

func main() {
	raw, _ := os.ReadFile("tasks/artifacts/security/rust_tracefuse_detector_bytes_guard.wasm")
	ctx := context.Background()
	fmt.Println("validate:", sandbox.ValidateCheckWasm(ctx, raw))

	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)
	c, _ := rt.CompileModule(ctx, raw)
	m, _ := rt.InstantiateModule(ctx, c, wazero.NewModuleConfig())
	for n := range m.ExportedFunctionDefinitions() {
		fmt.Println("export func:", n)
	}
	fn := m.ExportedFunction("check_bytes")
	mem := m.Memory()
	line := []byte("AKIAIOSFODNN7EXAMPLE")
	mem.Write(0, line)
	res, err := fn.Call(ctx, 0, uint64(len(line)))
	fmt.Printf("direct call res=%v err=%v\n", res, err)

	ok, err := sandbox.InvokeCheckInput(ctx, raw, []byte("AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE"))
	fmt.Printf("invoke ok=%v err=%v\n", ok, err)
}
