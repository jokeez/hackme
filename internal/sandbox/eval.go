package sandbox

import (
	"context"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// WasmEvalTimeout caps a single eval(i64)->i64 call (embedded lock is trivial; limit is for future/custom WASM).
const WasmEvalTimeout = 2 * time.Second

// lockWasm is a minimal valid Wasm 1.0 module exporting eval(i64)->i64: return n*7+13.
// type section payload = 6 bytes: 1 functype (i64)->(i64)
const lockWasmHex = "0061736d0100000001060160017e017e03020100070801046576616c00000a0c010a00200042077e420d7c0b"

var (
	initOnce sync.Once
	rt       wazero.Runtime
	mod      api.Module
	evalFn   api.Function
	initErr  error
	callMu   sync.Mutex
)

var errNoEval = errors.New("sandbox: missing eval export")

func ensure(ctx context.Context) error {
	initOnce.Do(func() {
		raw, err := hex.DecodeString(lockWasmHex)
		if err != nil {
			initErr = err
			return
		}
		// Use interpreter backend to avoid executable-mmap requirements on locked-down VPS kernels.
		rt = wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter().WithMemoryLimitPages(128))
		compiled, err := rt.CompileModule(ctx, raw)
		if err != nil {
			initErr = err
			return
		}
		mod, err = rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("lock"))
		if err != nil {
			initErr = err
			return
		}
		evalFn = mod.ExportedFunction("eval")
		if evalFn == nil {
			initErr = errNoEval
		}
	})
	return initErr
}

// Eval runs the embedded lock WASM: eval(nonce) = nonce*7 + 13.
func Eval(ctx context.Context, nonce uint64) (uint64, error) {
	if err := ensure(ctx); err != nil {
		return 0, err
	}
	callCtx, cancel := context.WithTimeout(ctx, WasmEvalTimeout)
	defer cancel()
	callMu.Lock()
	defer callMu.Unlock()
	res, err := evalFn.Call(callCtx, nonce)
	if err != nil {
		return 0, err
	}
	return res[0], nil
}
