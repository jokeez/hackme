package sandbox

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

const (
	defaultMaxWasmCheckBytes = 65536
	maxWasmCheckBytesFloor   = 1024
	maxWasmCheckBytesCeil    = 2 * 1024 * 1024
	defaultWasmCheckTimeout  = 350 * time.Millisecond
	defaultCompiledCacheMax  = 256
	defaultMemoryLimitPages  = 128
)

// MinimalGateWasmHex is a tiny valid module exporting check(i64)->i32 that always returns 1.
// Use in ./tasks manifests as wasm_check_hex for demos (still requires native PoH hit first).
const MinimalGateWasmHex = "0061736d0100000001060160017e017f0302010007090105636865636b00000a0601040041010b"

var (
	checkCompileMu   sync.Mutex
	checkCompiled    sync.Map // sha256 hex -> wazero.CompiledModule (owned by checkRuntime)
	checkCompiledLRU []string
	checkQuarantine  sync.Map // sha256 hex -> reason string (blocked signatures)
	checkRuntime     wazero.Runtime
	checkOnce        sync.Once
	checkInstSerial  uint64
)

type PolicySnapshot struct {
	Locked            bool   `json:"locked"`
	Profile           string `json:"profile"`
	MaxCheckWasmBytes int    `json:"max_check_wasm_bytes"`
	CheckTimeoutMS    int64  `json:"check_timeout_ms"`
	CompiledCacheMax  int    `json:"compiled_cache_max"`
	MemoryLimitPages  uint32 `json:"memory_limit_pages"`
	BlockQuarantined  bool   `json:"block_quarantined"`
}

func sandboxProfile() string {
	if sandboxLocked() {
		return "secure"
	}
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_SANDBOX_PROFILE")))
	switch v {
	case "secure", "default", "permissive":
		return v
	default:
		return "default"
	}
}

func sandboxLocked() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_SANDBOX_LOCKED")))
	if v == "" {
		return true
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func maxMemoryLimitPages() uint32 {
	v := defaultMemoryLimitPages
	switch sandboxProfile() {
	case "secure":
		v = 96
	case "default":
		v = 128
	case "permissive":
		v = 256
	}
	if !sandboxLocked() && sandboxProfile() == "permissive" {
		if s := strings.TrimSpace(os.Getenv("HACKME_SANDBOX_MEMORY_PAGES")); s != "" {
			if x, err := strconv.Atoi(s); err == nil {
				v = x
			}
		}
	}
	if v < 32 {
		v = 32
	}
	if v > 1024 {
		v = 1024
	}
	return uint32(v)
}

// Policy returns active sandbox policy values.
func Policy() PolicySnapshot {
	return PolicySnapshot{
		Locked:            sandboxLocked(),
		Profile:           sandboxProfile(),
		MaxCheckWasmBytes: MaxCheckWasmBytes(),
		CheckTimeoutMS:    wasmCheckTimeout().Milliseconds(),
		CompiledCacheMax:  compiledCacheMax(),
		MemoryLimitPages:  maxMemoryLimitPages(),
		BlockQuarantined:  blockQuarantinedWasm(),
	}
}

func ensureCheckRuntime() wazero.Runtime {
	checkOnce.Do(func() {
		checkRuntime = wazero.NewRuntimeWithConfig(context.Background(),
			wazero.NewRuntimeConfigInterpreter().WithMemoryLimitPages(maxMemoryLimitPages()))
	})
	return checkRuntime
}

func compiledKey(wasm []byte) string {
	sum := sha256.Sum256(wasm)
	return fmt.Sprintf("%x", sum[:])
}

// MaxCheckWasmBytes is the active size cap for wasm_check_hex / wasm_artifact_path payloads.
// Tune with HACKME_SANDBOX_MAX_CHECK_WASM_BYTES (floor=1024, ceil=2MiB).
func MaxCheckWasmBytes() int {
	v := defaultMaxWasmCheckBytes
	switch sandboxProfile() {
	case "secure":
		// Keep secure mode conservative, but large enough for practical
		// audited Rust payloads that otherwise exceed 64KiB after compile.
		v = 1536 * 1024
	case "default":
		v = 131072
	case "permissive":
		v = 262144
	}
	if !sandboxLocked() && sandboxProfile() == "permissive" {
		if s := strings.TrimSpace(os.Getenv("HACKME_SANDBOX_MAX_CHECK_WASM_BYTES")); s != "" {
			if x, err := strconv.Atoi(s); err == nil {
				v = x
			}
		}
	}
	if v < maxWasmCheckBytesFloor {
		v = maxWasmCheckBytesFloor
	}
	if v > maxWasmCheckBytesCeil {
		v = maxWasmCheckBytesCeil
	}
	return v
}

func wasmCheckTimeout() time.Duration {
	d := defaultWasmCheckTimeout
	switch sandboxProfile() {
	case "secure":
		d = 300 * time.Millisecond
	case "default":
		d = 500 * time.Millisecond
	case "permissive":
		d = 800 * time.Millisecond
	}
	if !sandboxLocked() && sandboxProfile() == "permissive" {
		if s := strings.TrimSpace(os.Getenv("HACKME_SANDBOX_CHECK_TIMEOUT_MS")); s != "" {
			if x, err := strconv.Atoi(s); err == nil && x >= 50 {
				d = time.Duration(x) * time.Millisecond
			}
		}
	}
	return d
}

func compiledCacheMax() int {
	v := defaultCompiledCacheMax
	switch sandboxProfile() {
	case "secure":
		v = 128
	case "default":
		v = 256
	case "permissive":
		v = 512
	}
	if !sandboxLocked() && sandboxProfile() == "permissive" {
		if s := strings.TrimSpace(os.Getenv("HACKME_SANDBOX_COMPILED_CACHE_MAX")); s != "" {
			if x, err := strconv.Atoi(s); err == nil {
				v = x
			}
		}
	}
	if v < 16 {
		return 16
	}
	if v > 4096 {
		return 4096
	}
	return v
}

func blockQuarantinedWasm() bool {
	if sandboxLocked() {
		return true
	}
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_SANDBOX_BLOCK_QUARANTINED")))
	if v == "" {
		return true
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func quarantineWasm(key, reason string) {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(reason) == "" {
		return
	}
	checkQuarantine.Store(key, reason)
}

func wasmMagicAndVersionOK(wasm []byte) bool {
	// Wasm 1.0 magic+version header: 00 61 73 6d 01 00 00 00
	if len(wasm) < 8 {
		return false
	}
	return wasm[0] == 0x00 && wasm[1] == 0x61 && wasm[2] == 0x73 && wasm[3] == 0x6d &&
		wasm[4] == 0x01 && wasm[5] == 0x00 && wasm[6] == 0x00 && wasm[7] == 0x00
}

// ValidateCheckWasm ensures wasm is a small module exporting check(i64)->i32.
func ValidateCheckWasm(ctx context.Context, wasm []byte) error {
	if len(wasm) == 0 {
		return errors.New("sandbox: empty wasm")
	}
	if !wasmMagicAndVersionOK(wasm) {
		return errors.New("sandbox: invalid wasm magic/version")
	}
	maxBytes := MaxCheckWasmBytes()
	if len(wasm) > maxBytes {
		return fmt.Errorf("sandbox: wasm too large (%d > %d)", len(wasm), maxBytes)
	}
	validateCtx, cancel := context.WithTimeout(ctx, wasmCheckTimeout())
	defer cancel()
	rt := ensureCheckRuntime()
	checkCompileMu.Lock()
	defer checkCompileMu.Unlock()
	key := compiledKey(wasm)
	if blockQuarantinedWasm() {
		if v, ok := checkQuarantine.Load(key); ok {
			return fmt.Errorf("sandbox: wasm is quarantined: %v", v)
		}
	}
	if _, ok := checkCompiled.Load(key); ok {
		return nil
	}
	compiled, err := rt.CompileModule(validateCtx, wasm)
	if err != nil {
		quarantineWasm(key, "compile wasm failed")
		return fmt.Errorf("sandbox: compile wasm: %w", err)
	}
	mod, err := rt.InstantiateModule(validateCtx, compiled, wazero.NewModuleConfig().WithName("chk-"+key[:12]))
	if err != nil {
		_ = compiled.Close(validateCtx)
		quarantineWasm(key, "instantiate wasm failed")
		return fmt.Errorf("sandbox: instantiate wasm: %w", err)
	}
	exports := mod.ExportedFunctionDefinitions()
	if len(exports) != 1 {
		_ = mod.Close(validateCtx)
		_ = compiled.Close(validateCtx)
		quarantineWasm(key, "invalid exports set")
		return errors.New("sandbox: wasm must export only check(i64)->i32")
	}
	fn := mod.ExportedFunction("check")
	if fn == nil {
		_ = mod.Close(validateCtx)
		_ = compiled.Close(validateCtx)
		quarantineWasm(key, "missing check export")
		return errors.New("sandbox: wasm must export check(i64)->i32")
	}
	def, ok := exports["check"]
	if !ok {
		_ = mod.Close(validateCtx)
		_ = compiled.Close(validateCtx)
		quarantineWasm(key, "missing check export definition")
		return errors.New("sandbox: wasm must export check(i64)->i32")
	}
	if !exactCheckSignature(def.ParamTypes(), def.ResultTypes()) {
		_ = mod.Close(validateCtx)
		_ = compiled.Close(validateCtx)
		quarantineWasm(key, "check export has invalid signature")
		return errors.New("sandbox: check export must be exactly check(i64)->i32")
	}
	res, err := fn.Call(validateCtx, 0)
	if err != nil || len(res) != 1 {
		_ = mod.Close(validateCtx)
		_ = compiled.Close(validateCtx)
		quarantineWasm(key, "check export call signature mismatch")
		return errors.New("sandbox: check export must be callable as check(i64)->i32")
	}
	_ = mod.Close(validateCtx)
	checkQuarantine.Delete(key)
	if old, exists := checkCompiled.Load(key); exists {
		_ = old.(wazero.CompiledModule).Close(validateCtx)
	}
	checkCompiled.Store(key, compiled)
	checkCompiledLRU = append(checkCompiledLRU, key)
	maxEntries := compiledCacheMax()
	for len(checkCompiledLRU) > maxEntries {
		evictKey := checkCompiledLRU[0]
		checkCompiledLRU = checkCompiledLRU[1:]
		if evictKey == key {
			continue
		}
		if old, ok := checkCompiled.Load(evictKey); ok {
			_ = old.(wazero.CompiledModule).Close(validateCtx)
			checkCompiled.Delete(evictKey)
		}
	}
	return nil
}

// InvokeCheck runs export check(n) once; wasm must have passed ValidateCheckWasm.
func InvokeCheck(ctx context.Context, wasm []byte, n uint64) (bool, error) {
	if len(wasm) == 0 {
		return true, nil
	}
	rt := ensureCheckRuntime()
	key := compiledKey(wasm)
	v, ok := checkCompiled.Load(key)
	if !ok {
		if err := ValidateCheckWasm(ctx, wasm); err != nil {
			return false, err
		}
		v, _ = checkCompiled.Load(key)
	}
	compiled := v.(wazero.CompiledModule)

	id := atomic.AddUint64(&checkInstSerial, 1)
	instCtx, instCancel := context.WithTimeout(ctx, wasmCheckTimeout())
	defer instCancel()
	mod, err := rt.InstantiateModule(instCtx, compiled, wazero.NewModuleConfig().WithName(fmt.Sprintf("chk-%s-%d", key[:16], id)))
	if err != nil {
		return false, err
	}
	defer mod.Close(ctx)
	fn := mod.ExportedFunction("check")
	if fn == nil {
		return false, errors.New("sandbox: missing check export")
	}
	callCtx, cancel := context.WithTimeout(ctx, wasmCheckTimeout())
	defer cancel()
	res, err := fn.Call(callCtx, n)
	if err != nil {
		return false, err
	}
	if len(res) == 0 {
		return false, nil
	}
	return res[0] != 0, nil
}

func exactCheckSignature(params, results []api.ValueType) bool {
	return len(params) == 1 && len(results) == 1 && params[0] == api.ValueTypeI64 && results[0] == api.ValueTypeI32
}
