package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hackme/internal/chain"
	"hackme/internal/sandbox"
)

func readU32LEB(data []byte, pos *int) (uint32, error) {
	var result uint32
	var shift uint
	for i := 0; i < 5; i++ {
		if *pos >= len(data) {
			return 0, errors.New("unexpected eof in leb128")
		}
		b := data[*pos]
		*pos = *pos + 1
		result |= uint32(b&0x7f) << shift
		if (b & 0x80) == 0 {
			return result, nil
		}
		shift += 7
	}
	return 0, errors.New("leb128 too long")
}

func writeU32LEB(v uint32) []byte {
	out := make([]byte, 0, 5)
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			break
		}
	}
	return out
}

// tinygoSanitizeWasm rewrites exports to keep only function export "check"
// and strips start section so no hidden init routine runs at instantiation.
func tinygoSanitizeWasm(raw []byte) ([]byte, error) {
	if len(raw) < 8 {
		return nil, errors.New("tinygo wasm too short")
	}
	if !bytes.Equal(raw[:8], []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}) {
		return nil, errors.New("tinygo wasm invalid header")
	}
	out := make([]byte, 0, len(raw))
	out = append(out, raw[:8]...)
	pos := 8
	foundCheck := false
	for pos < len(raw) {
		sectionID := raw[pos]
		pos++
		size, err := readU32LEB(raw, &pos)
		if err != nil {
			return nil, err
		}
		if pos+int(size) > len(raw) {
			return nil, errors.New("tinygo wasm malformed section size")
		}
		body := raw[pos : pos+int(size)]
		pos += int(size)
		if sectionID == 8 {
			continue
		}
		if sectionID != 7 {
			out = append(out, sectionID)
			out = append(out, writeU32LEB(uint32(len(body)))...)
			out = append(out, body...)
			continue
		}
		p := 0
		count, err := readU32LEB(body, &p)
		if err != nil {
			return nil, err
		}
		var keep bytes.Buffer
		keepCount := uint32(0)
		for i := uint32(0); i < count; i++ {
			nameLen, err := readU32LEB(body, &p)
			if err != nil {
				return nil, err
			}
			if p+int(nameLen) > len(body) {
				return nil, errors.New("tinygo wasm malformed export name")
			}
			name := string(body[p : p+int(nameLen)])
			p += int(nameLen)
			if p >= len(body) {
				return nil, errors.New("tinygo wasm malformed export kind")
			}
			kind := body[p]
			p++
			idxStart := p
			_, err = readU32LEB(body, &p)
			if err != nil {
				return nil, err
			}
			idxBytes := body[idxStart:p]
			if kind == 0x00 && name == "check" {
				keep.Write(writeU32LEB(uint32(len(name))))
				keep.WriteString(name)
				keep.WriteByte(kind)
				keep.Write(idxBytes)
				keepCount++
				foundCheck = true
			}
		}
		var newExport bytes.Buffer
		newExport.Write(writeU32LEB(keepCount))
		newExport.Write(keep.Bytes())
		b := newExport.Bytes()
		out = append(out, 7)
		out = append(out, writeU32LEB(uint32(len(b)))...)
		out = append(out, b...)
	}
	if !foundCheck {
		return nil, errors.New("tinygo wasm missing check export")
	}
	return out, nil
}

const (
	maxTaskCodeBytes           = 200_000
	taskCompileTimeout         = 25 * time.Second
	taskCompileTimeoutGoWASM   = 90 * time.Second
	taskCompileTimeoutZigOrASC = 90 * time.Second
)

type taskFromCodeRequest struct {
	ID              string  `json:"id"`
	Language        string  `json:"language"` // rust | c | cpp | tinygo | zig | assemblyscript | wat (+ aliases)
	Code            string  `json:"code"`
	RewardHMC       float64 `json:"reward_hmc"`
	DifficultyScore int     `json:"difficulty_score"`
	TargetSolves    int     `json:"target_solves"`
	PayerRef        string  `json:"payer_ref"`
}

type generatedManifest struct {
	ID               string  `json:"id"`
	Kind             string  `json:"kind"`
	RewardHMC        float64 `json:"reward_hmc"`
	DifficultyScore  int     `json:"difficulty_score,omitempty"`
	TargetSolves     int     `json:"target_solves"`
	PayerRef         string  `json:"payer_ref,omitempty"`
	WasmArtifactPath string  `json:"wasm_artifact_path"`
	ArtifactHash     string  `json:"artifact_hash"`
}

// validateFromCodeTaskShape rejects full desktop/console apps before WASM compile.
func validateFromCodeTaskShape(lang, code string) (errCode, message string) {
	lang = strings.TrimSpace(strings.ToLower(lang))
	low := strings.ToLower(code)
	switch lang {
	case "c", "cpp":
		if strings.Contains(code, "iostream") || strings.Contains(code, "<iostream") {
			return "app_not_task_code", "console/desktop C++ (iostream) is not a WASM task; export only check(long long n) without main/cin/cout"
		}
		if strings.Contains(low, "std::cin") || strings.Contains(low, "getline(std::cin") {
			return "app_not_task_code", "interactive stdin code cannot run as WASM check(); extract pure check(long long n) logic"
		}
		if strings.Contains(low, "int main(") || strings.Contains(low, "void main(") {
			return "app_not_task_code", "main() is not allowed in from_code tasks; use exported check(long long n) only"
		}
	case "rust":
		if strings.Contains(low, "fn main(") {
			return "app_not_task_code", "fn main() is not allowed; export pub extern \"C\" fn check(n: i64) -> i32"
		}
	case "tinygo":
		if strings.Contains(low, "func main(") {
			return "app_not_task_code", "func main() is not allowed; export func check(n int64) int32"
		}
	}
	return "", ""
}

func normalizeFromCodeLanguage(raw string) string {
	lang := strings.TrimSpace(strings.ToLower(raw))
	switch lang {
	case "rs":
		return "rust"
	case "gcc":
		return "c"
	case "c++", "cxx", "cc":
		return "cpp"
	case "as":
		return "assemblyscript"
	case "go", "golang":
		// from_code compiles Go sources via TinyGo WASM toolchain.
		return "tinygo"
	case "asm", "assembly", "wast", "wasm-text", "wasm_text":
		return "wat"
	default:
		return lang
	}
}

func writeAPIError(w http.ResponseWriter, status int, code, errMsg string, extra map[string]any) {
	resp := map[string]any{
		"ok":    false,
		"code":  strings.TrimSpace(code),
		"error": strings.TrimSpace(errMsg),
	}
	for k, v := range extra {
		resp[k] = v
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

func sanitizeCodeID(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return "order-code"
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "order-code"
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

func compileTaskWASM(ctx context.Context, lang, srcPath, outPath string) (string, error) {
	var cmd *exec.Cmd
	workDir := filepath.Dir(srcPath)
	switch lang {
	case "rust":
		cmd = exec.CommandContext(ctx, "rustc", "--target", "wasm32-unknown-unknown", "-O", "--crate-type=cdylib", srcPath, "-o", outPath)
	case "c":
		// Pure C (same WASM link flags as C++; force language mode for .c sources).
		cmd = exec.CommandContext(ctx, "clang", "--target=wasm32", "-O3", "-nostdlib", "-Wl,--no-entry", "-Wl,--export=check", "-Wl,--strip-all", "-o", outPath, "-x", "c", srcPath)
	case "cpp":
		cmd = exec.CommandContext(ctx, "clang", "--target=wasm32", "-O3", "-nostdlib", "-Wl,--no-entry", "-Wl,--export=check", "-Wl,--strip-all", "-o", outPath, srcPath)
	case "tinygo":
		cmd = exec.CommandContext(ctx, "tinygo", "build", "-target", "wasm-unknown", "-no-debug", "-opt=z", "-o", outPath, srcPath)
	case "zig":
		// Shared WASM object with a single exported check symbol (requires zig on PATH).
		cmd = exec.CommandContext(ctx, "zig", "build-lib", srcPath, "-target", "wasm32-freestanding", "-dynamic", "-OReleaseSmall", "-femit-bin="+outPath)
	case "assemblyscript":
		// Requires asc (AssemblyScript compiler) on PATH, typically via npm i -g assemblyscript.
		cmd = exec.CommandContext(ctx, "asc", srcPath, "-o", outPath, "--optimize", "--runtime", "stub")
	case "wat", "asm", "assembly":
		// Assembly/text path: compile WAT -> wasm via wabt toolchain.
		cmd = exec.CommandContext(ctx, "wat2wasm", srcPath, "-o", outPath)
	default:
		return "", fmt.Errorf("unsupported language %q", lang)
	}
	// Keep compiler caches inside writable temp sandbox to avoid
	// host-specific HOME permission failures for service users.
	//
	// Important: rustc is often provided by rustup shims, which require
	// a stable HOME/rustup config. Overriding HOME to a temp dir breaks
	// toolchain resolution ("no default is configured"), so we keep HOME
	// unchanged for rust and only sandbox cache dirs.
	env := append(os.Environ(),
		"GOCACHE="+workDir,
		"XDG_CACHE_HOME="+workDir,
	)
	// TinyGo/WAT/C/C++ benefit from an isolated HOME for caches; rust/rustup must keep real HOME.
	// Zig and AssemblyScript compilers may need the operator HOME for global toolchain installs.
	if lang != "rust" && lang != "zig" && lang != "assemblyscript" {
		env = append(env, "HOME="+workDir)
	}
	if lang == "zig" {
		env = append(env, "ZIG_GLOBAL_CACHE_DIR="+workDir, "ZIG_LOCAL_CACHE_DIR="+workDir)
	}
	if lang == "rust" {
		// When running under systemd or minimal env, rustup may need explicit
		// tool home dirs. Only default if missing (do not override operator config).
		if h := strings.TrimSpace(os.Getenv("HOME")); h != "" {
			if strings.TrimSpace(os.Getenv("RUSTUP_HOME")) == "" {
				env = append(env, "RUSTUP_HOME="+filepath.Join(h, ".rustup"))
			}
			if strings.TrimSpace(os.Getenv("CARGO_HOME")) == "" {
				env = append(env, "CARGO_HOME="+filepath.Join(h, ".cargo"))
			}
		}
	}
	cmd.Env = env
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	logText := strings.TrimSpace(string(out))
	if err != nil {
		if logText == "" {
			logText = err.Error()
		}
		return logText, err
	}
	if _, statErr := os.Stat(outPath); statErr != nil {
		// Some TinyGo builds may emit wasm into cwd even with -o.
		if lang == "tinygo" {
			if candidates, _ := filepath.Glob(filepath.Join(workDir, "*.wasm")); len(candidates) > 0 {
				if b, rerr := os.ReadFile(candidates[0]); rerr == nil {
					_ = os.WriteFile(outPath, b, 0o644)
				}
			}
		}
		if _, statErr2 := os.Stat(outPath); statErr2 != nil {
			msg := "compiled wasm output not produced: " + statErr2.Error()
			if logText != "" {
				msg = logText + "\n" + msg
			}
			return msg, fmt.Errorf(msg)
		}
	}
	return logText, nil
}

// compileTaskFromCode builds and validates WASM for export check(i64)->i32.
func (a *app) compileTaskFromCode(ctx context.Context, req taskFromCodeRequest) (wasmBytes []byte, artifactHash, artifactRelPath, compileLog string, err error) {
	req.Language = normalizeFromCodeLanguage(req.Language)
	req.Code = strings.TrimSpace(req.Code)
	if req.Language == "" {
		return nil, "", "", "", errors.New("language required")
	}
	if req.Code == "" {
		return nil, "", "", "", errors.New("code required")
	}
	if len(req.Code) > maxTaskCodeBytes {
		return nil, "", "", "", errors.New("code too large")
	}
	if code, hint := validateFromCodeTaskShape(req.Language, req.Code); code != "" {
		return nil, "", "", "", fmt.Errorf("%s: %s", code, hint)
	}
	if strings.TrimSpace(req.ID) == "" {
		req.ID = "order-code-" + time.Now().UTC().Format("20060102t150405")
	}
	artifactRoot := chain.DefaultArtifactRoot()
	if err := os.MkdirAll(artifactRoot, 0o755); err != nil {
		return nil, "", "", "", err
	}
	artifactRootAbs, err := filepath.Abs(artifactRoot)
	if err != nil {
		return nil, "", "", "", err
	}
	tmpDir, err := os.MkdirTemp("", "hackme-task-code-*")
	if err != nil {
		return nil, "", "", "", err
	}
	defer os.RemoveAll(tmpDir)
	srcName := "check.rs"
	switch req.Language {
	case "c":
		srcName = "check.c"
	case "cpp":
		srcName = "check.cpp"
	case "tinygo":
		srcName = "check.go"
	case "wat":
		srcName = "check.wat"
	case "zig":
		srcName = "check.zig"
	case "assemblyscript":
		srcName = "check.ts"
	}
	srcPath := filepath.Join(tmpDir, srcName)
	if err := os.WriteFile(srcPath, []byte(req.Code), 0o600); err != nil {
		return nil, "", "", "", err
	}
	base := sanitizeCodeID(req.ID) + "-" + req.Language + "-" + time.Now().UTC().Format("20060102t150405")
	outName := base + ".wasm"
	outPath := filepath.Join(artifactRootAbs, outName)
	compileTimeout := taskCompileTimeout
	switch req.Language {
	case "tinygo":
		compileTimeout = taskCompileTimeoutGoWASM
	case "zig", "assemblyscript":
		compileTimeout = taskCompileTimeoutZigOrASC
	}
	cctx, cancel := context.WithTimeout(ctx, compileTimeout)
	defer cancel()
	compileLog, compileErr := compileTaskWASM(cctx, req.Language, srcPath, outPath)
	if compileErr != nil {
		return nil, "", "", compileLog, compileErr
	}
	wasmBytes, err = os.ReadFile(outPath)
	if err != nil {
		return nil, "", "", compileLog, err
	}
	if req.Language == "tinygo" || req.Language == "zig" || req.Language == "assemblyscript" {
		if sanitized, serr := tinygoSanitizeWasm(wasmBytes); serr == nil {
			wasmBytes = sanitized
			_ = os.WriteFile(outPath, wasmBytes, 0o644)
		} else {
			_ = os.Remove(outPath)
			return nil, "", "", compileLog, serr
		}
	}
	if err := sandbox.ValidateCheckWasm(ctx, wasmBytes); err != nil {
		_ = os.Remove(outPath)
		return nil, "", "", compileLog, err
	}
	sum := sha256.Sum256(wasmBytes)
	artifactHash = hex.EncodeToString(sum[:])
	return wasmBytes, artifactHash, outName, compileLog, nil
}

func (a *app) handleTaskFromCode(w http.ResponseWriter, r *http.Request) {
	if !a.allowRate("tasks_from_code:"+clientIP(r), 2) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "rate limited", nil)
		return
	}
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	if !requireAdminAuth(w, r) {
		return
	}
	logAdminAction(r, "tasks_from_code_post")

	var req taskFromCodeRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	req.Language = normalizeFromCodeLanguage(req.Language)
	req.Code = strings.TrimSpace(req.Code)
	if req.Language == "" {
		writeAPIError(w, http.StatusBadRequest, "language_required", "language required (rust|c|cpp|tinygo|zig|assemblyscript|wat; aliases: rs|gcc|c++|go|zig|as|asm|wast)", nil)
		return
	}
	if req.Language != "rust" && req.Language != "c" && req.Language != "cpp" && req.Language != "tinygo" && req.Language != "zig" && req.Language != "assemblyscript" && req.Language != "wat" {
		writeAPIError(w, http.StatusBadRequest, "unsupported_language", "unsupported language (rust, c, cpp, tinygo, zig, assemblyscript, wat; aliases: rs, gcc, c++, cxx, cc, go, golang, zig, as, assemblyscript, asm, assembly, wast)", nil)
		return
	}
	if req.Code == "" {
		writeAPIError(w, http.StatusBadRequest, "code_required", "code required", nil)
		return
	}
	if len(req.Code) > maxTaskCodeBytes {
		writeAPIError(w, http.StatusBadRequest, "code_too_large", "code too large", nil)
		return
	}
	if code, hint := validateFromCodeTaskShape(req.Language, req.Code); code != "" {
		writeAPIError(w, http.StatusBadRequest, code, hint, map[string]any{
			"expected":  "WASM export check(long long n) -> int (no main, no iostream/cin)",
			"see_also":  "docs/FUZZ_PRODUCT_GUIDE.md",
			"languages": "rust|c|cpp|tinygo|zig|assemblyscript|wat",
		})
		return
	}
	if req.ID == "" {
		req.ID = "order-code-" + time.Now().UTC().Format("20060102t150405")
	}

	_, artifactHash, outName, compileLog, compileErr := a.compileTaskFromCode(r.Context(), req)
	if compileErr != nil {
		writeAPIError(w, http.StatusBadRequest, "compile_failed", "compile failed", map[string]any{"compile_log": compileLog, "detail": compileErr.Error()})
		return
	}
	m := generatedManifest{
		ID:               strings.TrimSpace(req.ID),
		Kind:             "synthetic_poh_v1",
		RewardHMC:        req.RewardHMC,
		DifficultyScore:  req.DifficultyScore,
		TargetSolves:     req.TargetSolves,
		PayerRef:         strings.TrimSpace(req.PayerRef),
		WasmArtifactPath: outName,
		ArtifactHash:     artifactHash,
	}
	rawManifest, err := json.Marshal(m)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "manifest_build_error", "manifest build error", nil)
		return
	}

	res, err := a.chain.InsertOrderTask(r.Context(), rawManifest)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, chain.ErrInsufficientBalance) {
			code = http.StatusPaymentRequired
		} else if errors.Is(err, chain.ErrOrderEscrowRateLimited) {
			code = http.StatusTooManyRequests
		}
		writeAPIError(w, code, "manifest_rejected", err.Error(), nil)
		return
	}

	receipt := a.buildOrderVerificationReceipt(res.ID, rawManifest)
	writeJSON(w, map[string]any{
		"ok":                         true,
		"id":                         res.ID,
		"payer_ref":                  res.PayerRef,
		"prepaid_hmc":                res.PrepaidHMC,
		"balance_after":              res.BalanceAfter,
		"artifact_path":              outName,
		"artifact_hash":              artifactHash,
		"compile_log":                compileLog,
		"signature_ed25519":          a.signer.SignHex(rawManifest),
		"signing_public_key_ed25519": a.signer.PublicKeyHex(),
		"verified_by":                receipt.VerifiedBy,
		"verification_receipt":       receipt,
	})
}
