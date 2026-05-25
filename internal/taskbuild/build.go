// Package taskbuild compiles WASM check modules for fuzzing orders (local CLI / node).
package taskbuild

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hackme/internal/sandbox"
)

// Result holds compiled WASM and manifest fields for POST /api/tasks.
type Result struct {
	WasmPath       string
	WasmBytes      []byte
	ArtifactHash   string
	ManifestJSON   []byte
	CompileLog     string
}

// Options for BuildFromSource.
type Options struct {
	ID              string
	Language        string
	Source          string
	RewardHMC       float64
	DifficultyScore int
	TargetSolves    int
	PayerRef        string
	OutDir          string // default ./fuzzing-out
}

// NormalizeLang maps aliases to canonical language ids.
func NormalizeLang(raw string) string {
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
		return "tinygo"
	case "asm", "assembly", "wast", "wasm-text", "wasm_text":
		return "wat"
	default:
		return lang
	}
}

// ValidateShape rejects desktop-style sources unsuitable for WASM check export.
func ValidateShape(lang, code string) (errCode, message string) {
	lang = strings.TrimSpace(strings.ToLower(lang))
	low := strings.ToLower(code)
	switch lang {
	case "c", "cpp":
		if strings.Contains(code, "iostream") || strings.Contains(code, "<iostream") {
			return "app_not_task_code", "console/desktop C++ is not a WASM task; export only check(long long n)"
		}
		if strings.Contains(low, "int main(") || strings.Contains(low, "void main(") {
			return "app_not_task_code", "main() is not allowed; use exported check(long long n) only"
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

func srcFilename(lang string) string {
	switch lang {
	case "c":
		return "check.c"
	case "cpp":
		return "check.cpp"
	case "tinygo":
		return "check.go"
	case "wat":
		return "check.wat"
	case "zig":
		return "check.zig"
	case "assemblyscript":
		return "check.ts"
	default:
		return "check.rs"
	}
}

// BuildFromSource compiles source to WASM, validates ABI, returns manifest with wasm_check_hex.
func BuildFromSource(ctx context.Context, opt Options) (*Result, error) {
	lang := NormalizeLang(opt.Language)
	if lang == "" {
		return nil, errors.New("language required")
	}
	switch lang {
	case "rust", "c", "cpp", "tinygo", "zig", "assemblyscript", "wat":
	default:
		return nil, fmt.Errorf("unsupported language %q", lang)
	}
	code := strings.TrimSpace(opt.Source)
	if code == "" {
		return nil, errors.New("source required")
	}
	if code, msg := ValidateShape(lang, code); code != "" {
		return nil, fmt.Errorf("%s: %s", code, msg)
	}
	outDir := strings.TrimSpace(opt.OutDir)
	if outDir == "" {
		outDir = "fuzzing-out"
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	id := strings.TrimSpace(opt.ID)
	if id == "" {
		id = "fuzz-" + time.Now().UTC().Format("20060102t150405")
	}
	tmpDir, err := os.MkdirTemp("", "hackme-fuzz-build-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	srcPath := filepath.Join(tmpDir, srcFilename(lang))
	if err := os.WriteFile(srcPath, []byte(code), 0o600); err != nil {
		return nil, err
	}
	base := sanitizeID(id) + "-" + lang
	wasmName := base + ".wasm"
	wasmPath := filepath.Join(outDir, wasmName)
	log, err := compileWASM(ctx, lang, srcPath, wasmPath)
	if err != nil {
		return nil, fmt.Errorf("compile failed: %w\n%s", err, log)
	}
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, err
	}
	if lang == "tinygo" || lang == "zig" || lang == "assemblyscript" {
		if sanitized, serr := tinygoSanitizeWasm(wasmBytes); serr == nil {
			wasmBytes = sanitized
			_ = os.WriteFile(wasmPath, wasmBytes, 0o644)
		} else {
			return nil, fmt.Errorf("wasm sanitize: %w", serr)
		}
	}
	if err := sandbox.ValidateCheckWasm(ctx, wasmBytes); err != nil {
		return nil, fmt.Errorf("wasm validation: %w", err)
	}
	sum := sha256.Sum256(wasmBytes)
	artifactHash := hex.EncodeToString(sum[:])
	manifest := map[string]any{
		"id":                id,
		"kind":              "synthetic_poh_v1",
		"reward_hmc":        opt.RewardHMC,
		"difficulty_score":  opt.DifficultyScore,
		"target_solves":     opt.TargetSolves,
		"payer_ref":         strings.TrimSpace(opt.PayerRef),
		"artifact_hash":     artifactHash,
		"wasm_check_hex":    hex.EncodeToString(wasmBytes),
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	manifestPath := filepath.Join(outDir, sanitizeID(id)+".manifest.json")
	_ = os.WriteFile(manifestPath, raw, 0o644)
	return &Result{
		WasmPath:     wasmPath,
		WasmBytes:    wasmBytes,
		ArtifactHash: artifactHash,
		ManifestJSON: raw,
		CompileLog:   log,
	}, nil
}

func sanitizeID(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return "fuzz-order"
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
		return "fuzz-order"
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

func compileWASM(ctx context.Context, lang, srcPath, outPath string) (string, error) {
	workDir := filepath.Dir(srcPath)
	var cmd *exec.Cmd
	switch lang {
	case "rust":
		cmd = exec.CommandContext(ctx, "rustc", "--target", "wasm32-unknown-unknown", "-O", "--crate-type=cdylib", srcPath, "-o", outPath)
	case "c":
		cmd = exec.CommandContext(ctx, "clang", "--target=wasm32", "-O3", "-nostdlib", "-Wl,--no-entry", "-Wl,--export=check", "-Wl,--strip-all", "-o", outPath, "-x", "c", srcPath)
	case "cpp":
		cmd = exec.CommandContext(ctx, "clang", "--target=wasm32", "-O3", "-nostdlib", "-Wl,--no-entry", "-Wl,--export=check", "-Wl,--strip-all", "-o", outPath, srcPath)
	case "tinygo":
		cmd = exec.CommandContext(ctx, "tinygo", "build", "-target", "wasm-unknown", "-no-debug", "-opt=z", "-o", outPath, srcPath)
	case "zig":
		cmd = exec.CommandContext(ctx, "zig", "build-lib", srcPath, "-target", "wasm32-freestanding", "-dynamic", "-OReleaseSmall", "-femit-bin="+outPath)
	case "assemblyscript":
		cmd = exec.CommandContext(ctx, "asc", srcPath, "-o", outPath, "--optimize", "--runtime", "stub")
	case "wat":
		cmd = exec.CommandContext(ctx, "wat2wasm", srcPath, "-o", outPath)
	default:
		return "", fmt.Errorf("unsupported language %q", lang)
	}
	env := append(os.Environ(), "GOCACHE="+workDir, "XDG_CACHE_HOME="+workDir)
	if lang != "rust" && lang != "zig" && lang != "assemblyscript" {
		env = append(env, "HOME="+workDir)
	}
	if lang == "zig" {
		env = append(env, "ZIG_GLOBAL_CACHE_DIR="+workDir, "ZIG_LOCAL_CACHE_DIR="+workDir)
	}
	if lang == "rust" {
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
		if lang == "tinygo" {
			if candidates, _ := filepath.Glob(filepath.Join(workDir, "*.wasm")); len(candidates) > 0 {
				if b, rerr := os.ReadFile(candidates[0]); rerr == nil {
					_ = os.WriteFile(outPath, b, 0o644)
				}
			}
		}
		if _, statErr2 := os.Stat(outPath); statErr2 != nil {
			return logText, statErr2
		}
	}
	return logText, nil
}

// tinygoSanitizeWasm keeps only export "check" (copied from node task_codegen).
func tinygoSanitizeWasm(raw []byte) ([]byte, error) {
	if len(raw) < 8 || !bytes.Equal(raw[:8], []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}) {
		return nil, errors.New("wasm invalid header")
	}
	out := make([]byte, 0, len(raw))
	out = append(out, raw[:8]...)
	pos := 8
	foundCheck := false
	for pos < len(raw) {
		if pos >= len(raw) {
			break
		}
		sectionID := raw[pos]
		pos++
		size, err := readU32LEB(raw, &pos)
		if err != nil {
			return nil, err
		}
		if pos+int(size) > len(raw) {
			return nil, errors.New("malformed section")
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
				return nil, err
			}
			name := string(body[p : p+int(nameLen)])
			p += int(nameLen)
			if p >= len(body) {
				return nil, err
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
		if !foundCheck {
			return nil, errors.New("export check not found")
		}
		section := append([]byte{7}, writeU32LEB(keepCount)...)
		section = append(section, keep.Bytes()...)
		out = append(out, section...)
	}
	return out, nil
}

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
