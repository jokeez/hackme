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
	defaultMaxCheckInputBytes = 4096
	minCheckInputBytes        = 1
	maxCheckInputBytesCeil    = 4096
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
	checkInvokeOnce  sync.Once
	checkInvokeSem   chan struct{}
)

type PolicySnapshot struct {
	Locked            bool   `json:"locked"`
	Profile           string `json:"profile"`
	MaxCheckWasmBytes int    `json:"max_check_wasm_bytes"`
	MaxCheckInputBytes int   `json:"max_check_input_bytes"`
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
	// Explicit non-negative bound for CodeQL integer-conversion check.
	if v < 0 {
		v = 32
	}
	return uint32(v) //nolint:gosec // v clamped to [32,1024]
}

// Policy returns active sandbox policy values.
func Policy() PolicySnapshot {
	return PolicySnapshot{
		Locked:             sandboxLocked(),
		Profile:            sandboxProfile(),
		MaxCheckWasmBytes:  MaxCheckWasmBytes(),
		MaxCheckInputBytes: MaxCheckInputBytes(),
		CheckTimeoutMS:     wasmCheckTimeout().Milliseconds(),
		CompiledCacheMax:   compiledCacheMax(),
		MemoryLimitPages:   maxMemoryLimitPages(),
		BlockQuarantined:   blockQuarantinedWasm(),
	}
}

func ensureCheckRuntime() wazero.Runtime {
	checkOnce.Do(func() {
		checkRuntime = wazero.NewRuntimeWithConfig(context.Background(),
			wazero.NewRuntimeConfigInterpreter().WithMemoryLimitPages(maxMemoryLimitPages()))
	})
	return checkRuntime
}

// MaxCheckInputBytes is the platform ceiling for check_bytes / InvokeCheckInput payloads.
func MaxCheckInputBytes() int {
	v := defaultMaxCheckInputBytes
	if !sandboxLocked() && sandboxProfile() == "permissive" {
		if s := strings.TrimSpace(os.Getenv("HACKME_SANDBOX_MAX_CHECK_INPUT_BYTES")); s != "" {
			if x, err := strconv.Atoi(s); err == nil {
				v = x
			}
		}
	}
	if v < minCheckInputBytes {
		v = minCheckInputBytes
	}
	if v > maxCheckInputBytesCeil {
		v = maxCheckInputBytesCeil
	}
	return v
}

func clampCheckInput(input []byte) []byte {
	max := MaxCheckInputBytes()
	if len(input) <= max {
		return input
	}
	return input[:max]
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

// ValidateCheckWasm ensures wasm exports check(i64)->i32 or check_bytes(i32,i32)->i32 + memory.
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
	if err := rejectWasmHostileSections(wasm); err != nil {
		return err
	}
	validateCtx, cancel := context.WithTimeout(ctx, wasmCheckTimeout())
	defer cancel()
	rt := ensureCheckRuntime()
	key := compiledKey(wasm)
	checkCompileMu.Lock()
	if blockQuarantinedWasm() {
		if v, ok := checkQuarantine.Load(key); ok {
			checkCompileMu.Unlock()
			return fmt.Errorf("sandbox: wasm is quarantined: %v", v)
		}
	}
	if _, ok := checkCompiled.Load(key); ok {
		checkCompileMu.Unlock()
		return nil
	}
	checkCompileMu.Unlock()

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
	if err := validateCheckExports(validateCtx, mod, exports); err != nil {
		_ = mod.Close(validateCtx)
		_ = compiled.Close(validateCtx)
		quarantineWasm(key, "invalid check export")
		return err
	}
	_ = mod.Close(validateCtx)

	checkCompileMu.Lock()
	defer checkCompileMu.Unlock()
	if blockQuarantinedWasm() {
		if v, ok := checkQuarantine.Load(key); ok {
			_ = compiled.Close(validateCtx)
			return fmt.Errorf("sandbox: wasm is quarantined: %v", v)
		}
	}
	if _, exists := checkCompiled.Load(key); exists {
		// Another goroutine won the race: keep the live cache entry.
		// Closing the cached module here caused
		// "source module must be compiled before instantiation" under pool load.
		_ = compiled.Close(validateCtx)
		return nil
	}
	checkQuarantine.Delete(key)
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
			checkCompiled.Delete(evictKey)
			_ = old.(wazero.CompiledModule).Close(validateCtx)
		}
	}
	return nil
}

func loadCompiledModule(key string) (wazero.CompiledModule, bool) {
	v, ok := checkCompiled.Load(key)
	if !ok || v == nil {
		return nil, false
	}
	compiled, ok := v.(wazero.CompiledModule)
	return compiled, ok
}

func dropCompiledModule(key string) {
	if key == "" {
		return
	}
	checkCompileMu.Lock()
	defer checkCompileMu.Unlock()
	if old, ok := checkCompiled.Load(key); ok {
		checkCompiled.Delete(key)
		_ = old.(wazero.CompiledModule).Close(context.Background())
	}
	// Drop stale LRU entries for this key (best-effort; duplicates are skipped on eviction).
	dst := checkCompiledLRU[:0]
	for _, k := range checkCompiledLRU {
		if k != key {
			dst = append(dst, k)
		}
	}
	checkCompiledLRU = dst
}

func isClosedCompiledErr(err error) bool {
	if err == nil {
		return false
	}
	low := strings.ToLower(err.Error())
	return strings.Contains(low, "must be compiled before instantiation") ||
		strings.Contains(low, "module has already been closed") ||
		strings.Contains(low, "closed module")
}

var errEmptyWasm = errors.New("sandbox: empty wasm")

// InvokeCheckInput runs check_bytes when exported, else packs bytes into check(i64).
func InvokeCheckInput(ctx context.Context, wasm []byte, input []byte) (bool, error) {
	out, err := InvokeCheckOutcomeInput(ctx, wasm, input)
	if err != nil {
		return false, err
	}
	return out.OK, nil
}

// InvokeCheckOutcomeInput runs check_bytes/check and returns pass/fail plus optional cov bitmap.
func InvokeCheckOutcomeInput(ctx context.Context, wasm []byte, input []byte) (CheckOutcome, error) {
	if len(wasm) == 0 {
		return CheckOutcome{}, errEmptyWasm
	}
	sem := ensureCheckInvokeSem()
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	case <-ctx.Done():
		return CheckOutcome{}, ctx.Err()
	}
	return invokeCheckOutcomeInputHeld(ctx, wasm, input)
}

func invokeCheckOutcomeInputHeld(ctx context.Context, wasm []byte, input []byte) (CheckOutcome, error) {
	rt := ensureCheckRuntime()
	key := compiledKey(wasm)
	compiled, ok := loadCompiledModule(key)
	if !ok {
		if err := ValidateCheckWasm(ctx, wasm); err != nil {
			return CheckOutcome{}, err
		}
		compiled, ok = loadCompiledModule(key)
		if !ok {
			return CheckOutcome{}, errors.New("sandbox: compiled module missing after validate")
		}
	}
	id := atomic.AddUint64(&checkInstSerial, 1)
	instCtx, instCancel := context.WithTimeout(ctx, wasmCheckTimeout())
	defer instCancel()
	mod, err := rt.InstantiateModule(instCtx, compiled, wazero.NewModuleConfig().WithName(fmt.Sprintf("chk-%s-%d", key[:16], id)))
	if err != nil && isClosedCompiledErr(err) {
		dropCompiledModule(key)
		if vErr := ValidateCheckWasm(ctx, wasm); vErr != nil {
			return CheckOutcome{}, vErr
		}
		compiled, ok = loadCompiledModule(key)
		if !ok {
			return CheckOutcome{}, errors.New("sandbox: compiled module missing after revalidate")
		}
		id = atomic.AddUint64(&checkInstSerial, 1)
		mod, err = rt.InstantiateModule(instCtx, compiled, wazero.NewModuleConfig().WithName(fmt.Sprintf("chk-%s-%d", key[:16], id)))
	}
	if err != nil {
		return CheckOutcome{}, err
	}
	defer mod.Close(ctx)
	if fn := mod.ExportedFunction("check_bytes"); fn != nil {
		mem := mod.Memory()
		if mem == nil {
			return CheckOutcome{}, errors.New("sandbox: check_bytes requires memory export")
		}
		if len(input) == 0 {
			input = []byte{0}
		}
		input = clampCheckInput(input)
		const inputMemOff = uint32(8) // avoid Rust null-pointer check on wasm address 0
		if !mem.Write(inputMemOff, input) {
			return CheckOutcome{}, errors.New("sandbox: check_bytes memory write failed")
		}
		callCtx, cancel := context.WithTimeout(ctx, wasmCheckTimeout())
		defer cancel()
		res, err := fn.Call(callCtx, uint64(inputMemOff), uint64(len(input)))
		if err != nil {
			return CheckOutcome{}, err
		}
		ok := len(res) > 0 && res[0] != 0
		return CheckOutcome{OK: ok, EdgeBitmap: ReadCovBitmap(mem)}, nil
	}
	var n uint64
	for i := 0; i < len(input) && i < 8; i++ {
		n |= uint64(input[i]) << (8 * i)
	}
	return invokeCheckOutcomeHeld(ctx, wasm, n)
}

// InvokeCheck runs export check(n) once; wasm must have passed ValidateCheckWasm.
func InvokeCheck(ctx context.Context, wasm []byte, n uint64) (bool, error) {
	out, err := InvokeCheckOutcome(ctx, wasm, n)
	if err != nil {
		return false, err
	}
	return out.OK, nil
}

// InvokeCheckOutcome runs check(i64) and returns optional cov bitmap from memory.
func InvokeCheckOutcome(ctx context.Context, wasm []byte, n uint64) (CheckOutcome, error) {
	if len(wasm) == 0 {
		return CheckOutcome{}, errEmptyWasm
	}
	sem := ensureCheckInvokeSem()
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	case <-ctx.Done():
		return CheckOutcome{}, ctx.Err()
	}
	return invokeCheckOutcomeHeld(ctx, wasm, n)
}

func invokeCheckOutcomeHeld(ctx context.Context, wasm []byte, n uint64) (CheckOutcome, error) {
	rt := ensureCheckRuntime()
	key := compiledKey(wasm)
	compiled, ok := loadCompiledModule(key)
	if !ok {
		if err := ValidateCheckWasm(ctx, wasm); err != nil {
			return CheckOutcome{}, err
		}
		compiled, ok = loadCompiledModule(key)
		if !ok {
			return CheckOutcome{}, errors.New("sandbox: compiled module missing after validate")
		}
	}

	id := atomic.AddUint64(&checkInstSerial, 1)
	instCtx, instCancel := context.WithTimeout(ctx, wasmCheckTimeout())
	defer instCancel()
	mod, err := rt.InstantiateModule(instCtx, compiled, wazero.NewModuleConfig().WithName(fmt.Sprintf("chk-%s-%d", key[:16], id)))
	if err != nil && isClosedCompiledErr(err) {
		dropCompiledModule(key)
		if vErr := ValidateCheckWasm(ctx, wasm); vErr != nil {
			return CheckOutcome{}, vErr
		}
		compiled, ok = loadCompiledModule(key)
		if !ok {
			return CheckOutcome{}, errors.New("sandbox: compiled module missing after revalidate")
		}
		id = atomic.AddUint64(&checkInstSerial, 1)
		mod, err = rt.InstantiateModule(instCtx, compiled, wazero.NewModuleConfig().WithName(fmt.Sprintf("chk-%s-%d", key[:16], id)))
	}
	if err != nil {
		return CheckOutcome{}, err
	}
	defer mod.Close(ctx)
	fn := mod.ExportedFunction("check")
	if fn == nil {
		return CheckOutcome{}, errors.New("sandbox: missing check export")
	}
	callCtx, cancel := context.WithTimeout(ctx, wasmCheckTimeout())
	defer cancel()
	res, err := fn.Call(callCtx, n)
	if err != nil {
		return CheckOutcome{}, err
	}
	pass := len(res) > 0 && res[0] != 0
	var bitmap []byte
	if mem := mod.Memory(); mem != nil {
		bitmap = ReadCovBitmap(mem)
	}
	return CheckOutcome{OK: pass, EdgeBitmap: bitmap}, nil
}

func invokeCheckHeld(ctx context.Context, wasm []byte, n uint64) (bool, error) {
	out, err := invokeCheckOutcomeHeld(ctx, wasm, n)
	if err != nil {
		return false, err
	}
	return out.OK, nil
}

func ensureCheckInvokeSem() chan struct{} {
	checkInvokeOnce.Do(func() {
		n := 4
		// Locked profile is the prod default: do not honor permissive concurrency overrides.
		if !sandboxLocked() {
			if v := strings.TrimSpace(os.Getenv("HACKME_SANDBOX_MAX_CONCURRENT")); v != "" {
				if x, err := strconv.Atoi(v); err == nil && x >= 1 && x <= 64 {
					n = x
				}
			}
		}
		checkInvokeSem = make(chan struct{}, n)
	})
	return checkInvokeSem
}

func exactCheckSignature(params, results []api.ValueType) bool {
	return len(params) == 1 && len(results) == 1 && params[0] == api.ValueTypeI64 && results[0] == api.ValueTypeI32
}

func exactCheckBytesSignature(params, results []api.ValueType) bool {
	return len(params) == 2 && len(results) == 1 &&
		params[0] == api.ValueTypeI32 && params[1] == api.ValueTypeI32 && results[0] == api.ValueTypeI32
}

func validateCheckExports(ctx context.Context, mod api.Module, exports map[string]api.FunctionDefinition) error {
	_, hasBytes := exports["check_bytes"]
	_, hasCheck := exports["check"]
	if hasBytes && hasCheck {
		return errors.New("sandbox: wasm must not export both check and check_bytes")
	}
	if hasBytes {
		if len(exports) != 1 {
			return errors.New("sandbox: check_bytes module must export only check_bytes(i32,i32)->i32")
		}
		def := exports["check_bytes"]
		if !exactCheckBytesSignature(def.ParamTypes(), def.ResultTypes()) {
			return errors.New("sandbox: check_bytes export must be exactly check_bytes(i32,i32)->i32")
		}
		if mod.Memory() == nil {
			return errors.New("sandbox: check_bytes requires exported memory")
		}
		fn := mod.ExportedFunction("check_bytes")
		if fn == nil {
			return errors.New("sandbox: missing check_bytes export")
		}
		mem := mod.Memory()
		const probeOff = uint32(8)
		if !mem.Write(probeOff, []byte{0}) {
			return errors.New("sandbox: check_bytes memory probe write failed")
		}
		res, err := fn.Call(ctx, uint64(probeOff), uint64(1))
		if err != nil {
			return fmt.Errorf("sandbox: %s", probeTrapReason(err))
		}
		if len(res) != 1 {
			return errors.New("sandbox: check_bytes export must return i32")
		}
		return nil
	}
	if len(exports) != 1 || !hasCheck {
		return errors.New("sandbox: wasm must export only check(i64)->i32")
	}
	def := exports["check"]
	if !exactCheckSignature(def.ParamTypes(), def.ResultTypes()) {
		return errors.New("sandbox: check export must be exactly check(i64)->i32")
	}
	fn := mod.ExportedFunction("check")
	if fn == nil {
		return errors.New("sandbox: missing check export")
	}
	res, err := fn.Call(ctx, 0)
	if err != nil || len(res) != 1 {
		reason := "check export call signature mismatch"
		if err != nil {
			reason = probeTrapReason(err)
		}
		return fmt.Errorf("sandbox: %s", reason)
	}
	return nil
}

func probeTrapReason(err error) string {
	low := strings.ToLower(err.Error())
	switch {
	case strings.Contains(low, "out of bounds"), strings.Contains(low, "oob"):
		return "check trapped: out-of-bounds memory access"
	case strings.Contains(low, "divide by zero"):
		return "check trapped: integer divide by zero"
	case strings.Contains(low, "unreachable"), strings.Contains(low, "indirect call"):
		return "check trapped: " + strings.TrimSpace(err.Error())
	default:
		return "check trapped during validation probe: " + strings.TrimSpace(err.Error())
	}
}
