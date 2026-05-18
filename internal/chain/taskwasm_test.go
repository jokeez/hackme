package chain

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/sandbox"
)

func TestWasmCheckBytesFromManifestJSON(t *testing.T) {
	raw := []byte(`{"id":"x","wasm_check_hex":"` + sandbox.MinimalGateWasmHex + `"}`)
	b, err := WasmCheckBytesFromManifestJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Fatal("expected wasm bytes")
	}
}

func TestResolveWasmCheckArtifactPath(t *testing.T) {
	root := t.TempDir()
	raw, err := hex.DecodeString(sandbox.MinimalGateWasmHex)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "gate.wasm")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(raw)
	sum := hex.EncodeToString(h[:])
	manifest := []byte(`{"id":"x","wasm_artifact_path":"gate.wasm","artifact_hash":"` + sum + `"}`)
	got, err := ResolveWasmCheckFromManifest(manifest, root)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Fatal("bytes mismatch")
	}
}

func TestResolveWasmCheckHashMismatch(t *testing.T) {
	root := t.TempDir()
	raw, _ := hex.DecodeString(sandbox.MinimalGateWasmHex)
	_ = os.WriteFile(filepath.Join(root, "g.wasm"), raw, 0o600)
	manifest := []byte(`{"wasm_artifact_path":"g.wasm","artifact_hash":"0000000000000000000000000000000000000000000000000000000000000000"}`)
	_, err := ResolveWasmCheckFromManifest(manifest, root)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveWasmCheckArtifactTooLarge(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "large.wasm")
	large := make([]byte, sandbox.MaxCheckWasmBytes()+1)
	if err := os.WriteFile(p, large, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(large)
	manifest := []byte(`{"wasm_artifact_path":"large.wasm","artifact_hash":"` + hex.EncodeToString(sum[:]) + `"}`)
	_, err := ResolveWasmCheckFromManifest(manifest, root)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected size error, got %v", err)
	}
}

func TestResolveWasmCheckBothPathAndHex(t *testing.T) {
	manifest := []byte(`{"wasm_artifact_path":"a.wasm","wasm_check_hex":"aa","artifact_hash":"0000000000000000000000000000000000000000000000000000000000000000"}`)
	_, err := ResolveWasmCheckFromManifest(manifest, t.TempDir())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSecureJoinRejectsDotDot(t *testing.T) {
	_, err := secureJoinUnder(t.TempDir(), ".."+string(filepath.Separator)+"etc")
	if err == nil {
		t.Fatal("expected error")
	}
}
