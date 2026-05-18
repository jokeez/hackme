// scan_ed25519_seeds walks directories for 64-hex Ed25519 seeds and prints addresses.
//
// Usage:
//
//	go run ./tools/scan_ed25519_seeds --match HMC-719006d93916ad52 /path/to/tree ...
//
// Skips common binary extensions; uses stdin only when paths are "-".
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	pathpkg "path/filepath"
	"regexp"
	"strings"
)

var hex64 = regexp.MustCompile(`[0-9a-fA-F]{64}`)

func addrFromSeed(seed []byte) string {
	if len(seed) != ed25519.SeedSize {
		return ""
	}
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	sum := sha256.Sum256(pub)
	return "HMC-" + hex.EncodeToString(sum[:])[:16]
}

func skipExt(name string) bool {
	switch strings.ToLower(pathpkg.Ext(name)) {
	case ".db", ".db-wal", ".db-shm", ".wasm", ".exe", ".png", ".jpg", ".jpeg", ".gif", ".ico", ".zip", ".tar", ".gz", ".parquet":
		return true
	default:
		return false
	}
}

func scanFile(path string, match string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 4<<20)) // 4 MiB cap per file
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	// Skip likely binaries (nul bytes), except small hex-only-ish files
	if bytes.IndexByte(data, 0) >= 0 && len(data) > 128 {
		return nil
	}
	lineNum := 1
	start := 0
	for i := range len(data) {
		if data[i] == '\n' {
			line := data[start:i]
			start = i + 1
			checkLine(path, lineNum, line, match)
			lineNum++
		}
	}
	if start < len(data) {
		checkLine(path, lineNum, data[start:], match)
	}
	return nil
}

func checkLine(path string, lineNum int, line []byte, match string) {
	for _, m := range hex64.FindAll(line, -1) {
		raw := make([]byte, ed25519.SeedSize)
		if _, err := hex.Decode(raw, m); err != nil {
			continue
		}
		addr := addrFromSeed(raw)
		if addr == "" {
			continue
		}
		if match != "" && !strings.EqualFold(addr, match) {
			continue
		}
		fmt.Printf("%s:%d:%s -> %s\n", path, lineNum, strings.TrimSpace(string(m)), addr)
	}
}

func main() {
	match := flag.String("match", "", "only print rows whose derived address equals this (e.g. HMC-d4cc83d66f8e5be5)")
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: scan_ed25519_seeds [--match HMC-...] DIR...")
		os.Exit(2)
	}
	if len(args) == 1 && args[0] == "-" {
		stdin, _ := io.ReadAll(os.Stdin)
		lineNum := 1
		for _, line := range bytes.Split(stdin, []byte{'\n'}) {
			checkLine("<stdin>", lineNum, line, *match)
			lineNum++
		}
		return
	}
	for _, root := range args {
		_ = pathpkg.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				base := d.Name()
				if base == ".git" || base == "node_modules" || base == "target" {
					return pathpkg.SkipDir
				}
				return nil
			}
			if skipExt(path) {
				return nil
			}
			// Skip huge trees of reports binary blobs — keep scanning text/json/md/go/html/sh
			if strings.Contains(path, "/reports/tests/") && strings.HasSuffix(path, ".json") {
				fi, err := d.Info()
				if err == nil && fi.Size() > 512_000 {
					return nil
				}
			}
			_ = scanFile(path, *match)
			return nil
		})
	}
}
