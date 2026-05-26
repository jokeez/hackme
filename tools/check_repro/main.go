package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"hackme/internal/sandbox"
)

func main() {
	wasmPath := flag.String("wasm", "", "path to wasm module")
	wasmHex := flag.String("wasm-hex", "", "wasm bytes as hex (writes temp file if -wasm empty)")
	inputRaw := flag.String("input", "", "u64 input (dec or 0xhex)")
	inputFile := flag.String("input-file", "", "artifact .input from fuzz (reads input_hex= line)")
	flag.Parse()
	if strings.TrimSpace(*wasmPath) == "" && strings.TrimSpace(*wasmHex) != "" {
		p, err := writeTempWasm(*wasmHex)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wasm-hex:", err)
			os.Exit(2)
		}
		*wasmPath = p
	}
	in := strings.TrimSpace(*inputRaw)
	if in == "" && strings.TrimSpace(*inputFile) != "" {
		var err error
		in, err = inputHexFromArtifact(*inputFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "input-file:", err)
			os.Exit(2)
		}
	}
	if strings.TrimSpace(*wasmPath) == "" || in == "" {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/check_repro -wasm <path> -input <u64|0xhex>")
		fmt.Fprintln(os.Stderr, "   or: -wasm-hex <hex> -input-file <path.input>")
		os.Exit(2)
	}
	n, err := parseU64(in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse input:", err)
		os.Exit(2)
	}
	root := findRoot()
	wp := *wasmPath
	if !filepath.IsAbs(wp) {
		wp = filepath.Join(root, wp)
	}
	raw, err := os.ReadFile(wp)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read wasm:", err)
		os.Exit(1)
	}
	ctx := context.Background()
	if err := sandbox.ValidateCheckWasm(ctx, raw); err != nil {
		fmt.Fprintln(os.Stderr, "validate:", err)
		os.Exit(1)
	}
	ok, err := sandbox.InvokeCheck(ctx, raw, n)
	if err != nil {
		fmt.Printf("trap error=%v input=0x%x (%d)\n", err, n, n)
		os.Exit(1)
	}
	fmt.Printf("ok=%v input=0x%x (%d)\n", ok, n, n)
}

func parseU64(s string) (uint64, error) {
	ss := strings.TrimSpace(strings.ToLower(s))
	if strings.HasPrefix(ss, "0x") {
		return strconv.ParseUint(strings.TrimPrefix(ss, "0x"), 16, 64)
	}
	return strconv.ParseUint(ss, 10, 64)
}

func inputHexFromArtifact(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "input_hex=") {
			return strings.TrimPrefix(line, "input_hex="), nil
		}
	}
	return "", fmt.Errorf("input_hex= not found in %s", path)
}

func writeTempWasm(hexStr string) (string, error) {
	hexStr = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(hexStr), "0x"))
	if len(hexStr)%2 == 1 {
		hexStr = "0" + hexStr
	}
	raw := make([]byte, len(hexStr)/2)
	for i := 0; i < len(raw); i++ {
		_, err := fmt.Sscanf(hexStr[i*2:i*2+2], "%02x", &raw[i])
		if err != nil {
			return "", err
		}
	}
	f, err := os.CreateTemp("", "check-repro-*.wasm")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		return "", err
	}
	_ = f.Close()
	return path, nil
}

func findRoot() string {
	cwd, _ := os.Getwd()
	for d := cwd; d != "/"; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
	}
	return cwd
}
