package chain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hackme/internal/sandbox"
)

// WasmCheckBytesFromManifestJSON extracts optional wasm_check_hex (hex-encoded WASM).
func WasmCheckBytesFromManifestJSON(manifestJSON []byte) ([]byte, error) {
	var aux struct {
		Wasm string `json:"wasm_check_hex"`
	}
	if len(manifestJSON) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(manifestJSON, &aux); err != nil {
		return nil, fmt.Errorf("chain: manifest json: %w", err)
	}
	s := strings.TrimSpace(strings.ReplaceAll(aux.Wasm, " ", ""))
	if s == "" {
		return nil, nil
	}
	if len(s)%2 != 0 {
		return nil, errors.New("chain: wasm_check_hex must have even length")
	}
	if len(s)/2 > sandbox.MaxCheckWasmBytes() {
		return nil, fmt.Errorf("chain: wasm_check_hex too large (%d > %d)", len(s)/2, sandbox.MaxCheckWasmBytes())
	}
	raw, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("chain: wasm_check_hex: %w", err)
	}
	return raw, nil
}

// DefaultArtifactRoot is the directory for wasm_artifact_path (relative paths inside it).
// Override with env HACKME_TASK_ARTIFACT_DIR.
func DefaultArtifactRoot() string {
	if v := strings.TrimSpace(os.Getenv("HACKME_TASK_ARTIFACT_DIR")); v != "" {
		return filepath.Clean(v)
	}
	return filepath.Clean(filepath.Join(".", "tasks", "artifacts"))
}

// ResolveWasmCheckFromManifest loads WASM bytes either from wasm_artifact_path + artifact_hash
// or from wasm_check_hex. If wasm_artifact_path is set, wasm_check_hex must be empty.
func ResolveWasmCheckFromManifest(manifestJSON []byte, artifactRoot string) ([]byte, error) {
	var aux struct {
		WasmCheckHex     string `json:"wasm_check_hex"`
		WasmArtifactPath string `json:"wasm_artifact_path"`
		ArtifactHash     string `json:"artifact_hash"`
	}
	if len(manifestJSON) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(manifestJSON, &aux); err != nil {
		return nil, fmt.Errorf("chain: manifest json: %w", err)
	}
	path := strings.TrimSpace(aux.WasmArtifactPath)
	hexInline := strings.TrimSpace(strings.ReplaceAll(aux.WasmCheckHex, " ", ""))
	if path != "" && hexInline != "" {
		return nil, errors.New("chain: use only one of wasm_artifact_path or wasm_check_hex")
	}
	if path == "" {
		return WasmCheckBytesFromManifestJSON(manifestJSON)
	}
	want := strings.ToLower(strings.TrimSpace(aux.ArtifactHash))
	if want == "" {
		return nil, errors.New("chain: artifact_hash required when wasm_artifact_path is set (sha256 hex of the .wasm file)")
	}
	if !isSHA256Hex(want) {
		return nil, errors.New("chain: artifact_hash must be 64 lowercase hex chars (sha256 of wasm file)")
	}
	root := filepath.Clean(artifactRoot)
	full, err := secureJoinUnder(root, path)
	if err != nil {
		return nil, fmt.Errorf("chain: wasm_artifact_path: %w", err)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, fmt.Errorf("chain: read wasm artifact %s: %w", full, err)
	}
	if len(data) > sandbox.MaxCheckWasmBytes() {
		return nil, fmt.Errorf("chain: wasm artifact too large (%d > %d)", len(data), sandbox.MaxCheckWasmBytes())
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return nil, fmt.Errorf("chain: artifact_hash mismatch: manifest says %s, file %s is %s", want, full, got)
	}
	return data, nil
}

func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}

func secureJoinUnder(root, userRel string) (string, error) {
	if userRel == "" {
		return "", errors.New("empty wasm_artifact_path")
	}
	if filepath.IsAbs(userRel) {
		return "", errors.New("absolute paths not allowed")
	}
	if strings.Contains(userRel, "..") {
		return "", errors.New("path must not contain ..")
	}
	cleanRoot := filepath.Clean(root)
	full := filepath.Join(cleanRoot, filepath.Clean(userRel))
	full = filepath.Clean(full)
	rel, err := filepath.Rel(cleanRoot, full)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes artifact root")
	}
	resolvedRoot, err := filepath.EvalSymlinks(cleanRoot)
	if err != nil {
		return "", fmt.Errorf("resolve artifact root: %w", err)
	}
	resolvedFull, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", fmt.Errorf("resolve artifact path: %w", err)
	}
	relResolved, err := filepath.Rel(resolvedRoot, resolvedFull)
	if err != nil {
		return "", err
	}
	if relResolved == ".." || strings.HasPrefix(relResolved, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes artifact root via symlink")
	}
	return resolvedFull, nil
}
