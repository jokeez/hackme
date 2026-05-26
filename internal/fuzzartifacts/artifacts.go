// Package fuzzartifacts stores per-input repro files for fuzz findings.
package fuzzartifacts

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Root returns the artifact directory (env HACKME_FUZZ_ARTIFACT_DIR or data/fuzz-artifacts).
func Root() string {
	v := strings.TrimSpace(os.Getenv("HACKME_FUZZ_ARTIFACT_DIR"))
	if v == "" {
		return filepath.Join(".", "data", "fuzz-artifacts")
	}
	return v
}

// WriteInput stores a reproducible input under campaignID/<sha>.input; returns absolute path or "".
func WriteInput(campaignID, inputSHA string, input uint64) string {
	campaignID = strings.TrimSpace(campaignID)
	inputSHA = strings.TrimSpace(strings.ToLower(inputSHA))
	if campaignID == "" || inputSHA == "" {
		return ""
	}
	root, err := filepath.Abs(Root())
	if err != nil {
		return ""
	}
	dir := filepath.Join(root, campaignID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	path := filepath.Join(dir, inputSHA+".input")
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], input)
	payload := fmt.Sprintf("input_hex=0x%x\ninput_dec=%d\nsha256=%s\n", input, input, inputSHA)
	if err := os.WriteFile(path, append([]byte(payload), buf[:]...), 0o600); err != nil {
		return ""
	}
	return path
}

// WriteWasmHex stores guard.wasm for a campaign (idempotent). Returns path or "".
func WriteWasmHex(campaignID, wasmHex string) string {
	campaignID = strings.TrimSpace(campaignID)
	wasmHex = strings.TrimSpace(wasmHex)
	if campaignID == "" || wasmHex == "" {
		return ""
	}
	raw, err := decodeHex(wasmHex)
	if err != nil || len(raw) == 0 {
		return ""
	}
	root, err := filepath.Abs(Root())
	if err != nil {
		return ""
	}
	dir := filepath.Join(root, campaignID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	path := filepath.Join(dir, "guard.wasm")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return ""
	}
	return path
}

func decodeHex(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.ToLower(s), "0x")
	if len(s)%2 == 1 {
		s = "0" + s
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		var b byte
		_, err := fmt.Sscanf(s[i*2:i*2+2], "%02x", &b)
		if err != nil {
			return nil, err
		}
		out[i] = b
	}
	return out, nil
}
