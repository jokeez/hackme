package fuzzupstream

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func corpusKey(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

func loadCorpusDir(dir string, limit int) [][]byte {
	if dir == "" || limit <= 0 {
		return nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type item struct {
		name string
		mod  int64
	}
	var files []item
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Size() <= 0 || info.Size() > 1<<20 {
			continue
		}
		files = append(files, item{name: name, mod: info.ModTime().UnixNano()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod > files[j].mod })
	if len(files) > limit {
		files = files[:limit]
	}
	out := make([][]byte, 0, len(files))
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(dir, f.name))
		if err != nil || len(b) == 0 {
			continue
		}
		out = append(out, b)
	}
	return out
}

func addCorpusFile(dir string, input []byte, saved *int, maxCorpus int) bool {
	if dir == "" || len(input) == 0 || saved == nil {
		return false
	}
	if maxCorpus > 0 {
		if ents, err := os.ReadDir(dir); err == nil && len(ents) >= maxCorpus {
			return false
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	name := fmt.Sprintf("seed-%s.bin", corpusKey(input))
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err == nil {
		return false
	}
	if err := os.WriteFile(path, input, 0o600); err != nil {
		return false
	}
	*saved++
	return true
}

// shouldKeepNovel decides whether a non-crashing input is worth keeping.
// Without coverage bits we approximate novelty via rare lengths and unseen hashes.
func shouldKeepNovel(input []byte, lengthSeen map[int]int, seenHash map[string]struct{}, iter int) bool {
	if len(input) < 2 {
		return false
	}
	h := corpusKey(input)
	if _, ok := seenHash[h]; ok {
		return false
	}
	// Sample periodically to bound disk/CPU.
	if iter%47 != 0 {
		return false
	}
	n := lengthSeen[len(input)]
	lengthSeen[len(input)] = n + 1
	if n >= 3 {
		return false
	}
	return true
}

// loadExtraSeedDirs returns optional persistent seed locations for a target.
func loadExtraSeedDirs(repoRoot, targetID string) []string {
	id, ok := SanitizeCatalogID(targetID)
	if !ok || repoRoot == "" {
		return nil
	}
	candidates := []string{
		filepath.Join(repoRoot, ".cache", "hunt-lf-seeds", id),
		filepath.Join(repoRoot, "reports", "oss-cve-libfuzzer", id, "corpus"),
		filepath.Join(repoRoot, "reports", "oss-cve", "corpus", id),
	}
	var out []string
	for _, d := range candidates {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			out = append(out, d)
		}
	}
	return out
}
