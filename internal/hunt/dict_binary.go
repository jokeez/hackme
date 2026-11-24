package hunt

import (
	"debug/elf"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A libFuzzer dictionary is the cheapest large win available to a coverage-guided
// run: on a magic-gated parser it decides whether the fuzzer spends its wall
// guessing header bytes or exercising the grammar behind them. The profile dicts in
// mutator_dict.go are hand-written per format and keyed off the target id, so a
// customer repo or an unrecognised catalog id gets eleven generic boundary tokens.
//
// The harness binary already carries the target's own vocabulary — every string
// literal the library compares against is in its read-only data. Lifting those
// into the dict costs one file read per lease and needs no knowledge of the format.

const (
	minBinaryDictToken  = 3
	maxBinaryDictToken  = 40
	maxBinaryDictTokens = 192
	maxBinaryScanBytes  = 64 << 20
	maxLibFuzzerDictLen = 24 << 10
)

// toolchainNoise matches strings that belong to clang, the sanitizer runtimes or
// libc rather than to the target. Without this filter an ASan binary contributes a
// few thousand of its own diagnostics and drowns the target's vocabulary.
var toolchainNoise = []string{
	"sanitizer", "asan", "ubsan", "lsan", "msan", "tsan", "interceptor",
	"llvm", "clang", "gcc", "libfuzzer", "fuzzer::", "glibc", "__libc",
	"stack trace", "shadow", "malloc", "calloc", "realloc", "free()",
	"operator new", "std::", "abi::", "cxxabi", "unwind", "dwarf",
	"/usr/", "/lib/", "/proc/", "/tmp/", "/build", "compiler-rt",
	"deadlysignal", "allocator", "symbolize", "coverage", "sancov",
}

// DeriveDictTokensFromBinary lifts candidate dictionary tokens out of a built
// harness. Read-only data is preferred when the file parses as ELF; otherwise the
// whole file is scanned, which still works for a stripped or non-ELF harness.
func DeriveDictTokensFromBinary(path string, limit int) []string {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if limit <= 0 || limit > maxBinaryDictTokens {
		limit = maxBinaryDictTokens
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() == 0 || st.Size() > maxBinaryScanBytes {
		return nil
	}
	var blobs [][]byte
	if f, err := elf.Open(path); err == nil {
		defer f.Close()
		for _, name := range []string{".rodata", ".rodata.str1.1", ".rodata.str1.8", ".data.rel.ro"} {
			if sec := f.Section(name); sec != nil && sec.Size > 0 && sec.Size <= maxBinaryScanBytes {
				if b, err := sec.Data(); err == nil {
					blobs = append(blobs, b)
				}
			}
		}
	}
	if len(blobs) == 0 {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		blobs = [][]byte{b}
	}

	// Keep the last offset each token was seen at. Measured layout of a clang
	// -fsanitize=address binary: the sanitizer runtime's own .rodata is linked first
	// and the program's objects land at the end, so on a 77 KiB .rodata holding 2,386
	// strings the target's seven literals were the last seven. Distance from the end
	// of read-only data is therefore the strongest available signal for "this string
	// belongs to the target", and it needs no vocabulary list to maintain.
	last := map[string]int{}
	for _, blob := range blobs {
		for off, s := range printableRunsAt(blob) {
			if !binaryDictCandidate(s) {
				continue
			}
			if prev, seen := last[s]; !seen || off > prev {
				last[s] = off
			}
		}
	}
	if len(last) == 0 {
		return nil
	}
	toks := make([]string, 0, len(last))
	for s := range last {
		toks = append(toks, s)
	}
	// Closest to the end of read-only data first, then the more token-like string,
	// then shortest, then stable order.
	sort.Slice(toks, func(i, j int) bool {
		if last[toks[i]] != last[toks[j]] {
			return last[toks[i]] > last[toks[j]]
		}
		if si, sj := binaryDictScore(toks[i]), binaryDictScore(toks[j]); si != sj {
			return si > sj
		}
		if len(toks[i]) != len(toks[j]) {
			return len(toks[i]) < len(toks[j])
		}
		return toks[i] < toks[j]
	})
	if len(toks) > limit {
		toks = toks[:limit]
	}
	return toks
}

func printableRunsAt(b []byte) map[int]string {
	out := make(map[int]string, 256)
	start := -1
	// Close the run that ends at end (exclusive). Keeping the terminator out of the
	// loop means no index ever reaches len(b): the earlier version walked to i ==
	// len(b) and relied on short-circuit evaluation to avoid the read, which is one
	// careless reordering away from an out-of-bounds access.
	flush := func(end int) {
		if start < 0 {
			return
		}
		if end-start >= minBinaryDictToken {
			out[start] = string(b[start:end])
		}
		start = -1
	}
	for i := 0; i < len(b); i++ {
		if b[i] >= 0x20 && b[i] < 0x7f {
			if start < 0 {
				start = i
			}
			continue
		}
		flush(i)
	}
	flush(len(b))
	return out
}

func binaryDictCandidate(s string) bool {
	if len(s) < minBinaryDictToken || len(s) > maxBinaryDictToken {
		return false
	}
	low := strings.ToLower(s)
	for _, bad := range toolchainNoise {
		if strings.Contains(low, bad) {
			return false
		}
	}
	// Mangled or internal symbols, format strings, paths, CLI flags, log templates
	// and leak suppressions are not grammar.
	if strings.Contains(s, "__") || strings.HasPrefix(s, "_Z") || strings.Contains(s, "%") ||
		strings.HasPrefix(s, "-") || strings.Contains(s, "::") || strings.Contains(s, "??") ||
		strings.Contains(s, "{{") || strings.Contains(s, "*") || strings.Contains(s, "/") {
		return false
	}
	// snake_case identifiers are runtime flag names and libc symbols
	// (detect_stack_use_after_return, hard_rss_limit_mb, name_to_handle_at). A format
	// keyword that long is rare; an upper-case one (BEGIN_RECORD) still passes.
	if len(s) >= 8 && strings.Contains(s, "_") && s == strings.ToLower(s) {
		return false
	}
	letters, digits, punct := 0, 0, 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			letters++
		case c >= '0' && c <= '9':
			digits++
		case c == ' ':
			return false // prose and diagnostics, not tokens
		default:
			punct++
		}
	}
	if letters == 0 && digits == 0 && punct == 0 {
		return false
	}
	// All-digit runs are covered by the generic boundary dict already.
	return letters+punct > 0
}

func binaryDictScore(s string) int {
	score := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{', '}', '[', ']', '<', '>', '=', ':', ';', ',', '"', '\'', '(', ')', '.', '#', '@', '$', '!', '+', '&', '|':
			score += 3
		}
	}
	if len(s) <= 8 {
		score += 2
	}
	// An ALL_CAPS or Mixed_Case keyword is far more likely to be the target's own
	// vocabulary than a lower-case identifier of the same shape.
	if s != strings.ToLower(s) {
		score += 2
	}
	return score
}

// AugmentLibFuzzerDict appends tokens to an existing -dict= body, keeping the
// original lines first and the result inside maxLibFuzzerDictLen. A malformed or
// empty base is replaced by a fresh header so the result is always loadable.
func AugmentLibFuzzerDict(base []byte, tokens []string) []byte {
	text := strings.TrimRight(string(base), "\n")
	if strings.TrimSpace(text) == "" {
		text = "# hackme hunt dict"
	}
	seen := map[string]struct{}{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		seen[strings.Trim(line, `"`)] = struct{}{}
	}
	var add []string
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		add = append(add, t)
	}
	if len(add) == 0 {
		return append([]byte(nil), base...)
	}
	body := formatLibFuzzerDict(add)
	// formatLibFuzzerDict writes its own header; drop it when appending.
	if idx := strings.IndexByte(body, '\n'); idx >= 0 && strings.HasPrefix(body, "#") {
		body = body[idx+1:]
	}
	out := text + "\n# derived from the harness binary\n" + body
	for len(out) > maxLibFuzzerDictLen {
		cut := strings.LastIndexByte(strings.TrimRight(out, "\n"), '\n')
		if cut <= 0 {
			break
		}
		out = out[:cut+1]
	}
	return []byte(out)
}

// Source-derived tokens beat binary-derived ones: a clang -fsanitize binary mixes the
// target's literals with the sanitizer runtime's own tables (check names, .str.N, flag
// lists), while the target's sources hold exactly the vocabulary it compares against.
// The catalog clone and inventory packs are on disk at lease time, so prefer them and
// keep the binary scan for a published harness that arrives without sources.

const (
	maxSourceDictFiles = 2048
	maxSourceFileBytes = 4 << 20
)

var sourceDictExt = map[string]bool{
	".c": true, ".h": true, ".cc": true, ".cpp": true, ".cxx": true, ".hpp": true, ".hh": true,
}

// DeriveDictTokensFromSources lifts string literals out of a target's sources.
func DeriveDictTokensFromSources(dir string, limit int) []string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil
	}
	if limit <= 0 || limit > maxBinaryDictTokens {
		limit = maxBinaryDictTokens
	}
	freq := map[string]int{}
	files := 0
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch strings.ToLower(d.Name()) {
			case ".git", "node_modules", "build", "cmake-files", "cmakefiles",
				// A vendored unit-test framework contributes its own vocabulary
				// (unity.h, cmock.h, testrunner.exe) and none of the target's.
				"test", "tests", "testing", "unity", "cmock", "vendor", "third_party",
				"examples", "example", "doc", "docs", "benchmark", "benchmarks":
				return filepath.SkipDir
			}
			return nil
		}
		if files >= maxSourceDictFiles {
			return filepath.SkipAll
		}
		if !sourceDictExt[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil || info.Size() == 0 || info.Size() > maxSourceFileBytes {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		files++
		for _, lit := range cStringLiterals(b) {
			if !sourceDictCandidate(lit) {
				continue
			}
			freq[lit]++
		}
		return nil
	})
	if len(freq) == 0 {
		return nil
	}
	toks := make([]string, 0, len(freq))
	for s := range freq {
		toks = append(toks, s)
	}
	// A literal the code compares against appears in several places; prefer those,
	// then grammar-shaped strings, then the shortest.
	sort.Slice(toks, func(i, j int) bool {
		if freq[toks[i]] != freq[toks[j]] {
			return freq[toks[i]] > freq[toks[j]]
		}
		if si, sj := binaryDictScore(toks[i]), binaryDictScore(toks[j]); si != sj {
			return si > sj
		}
		if len(toks[i]) != len(toks[j]) {
			return len(toks[i]) < len(toks[j])
		}
		return toks[i] < toks[j]
	})
	if len(toks) > limit {
		toks = toks[:limit]
	}
	return toks
}

// cStringLiterals pulls double-quoted literals out of C/C++ text, honouring escapes
// and skipping line comments so a commented-out example is not treated as grammar.
func cStringLiterals(b []byte) []string {
	out := make([]string, 0, 64)
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '/':
			for i < len(b) && b[i] != '\n' {
				i++
			}
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '*':
			i += 2
			for i+1 < len(b) && !(b[i] == '*' && b[i+1] == '/') {
				i++
			}
			i++
		case b[i] == '"':
			var sb strings.Builder
			i++
			for i < len(b) && b[i] != '"' {
				if b[i] == '\\' && i+1 < len(b) {
					switch b[i+1] {
					case 'n', 'r', 't', '0':
						// control escapes are not dict grammar; end the literal
						sb.Reset()
						i = len(b)
					default:
						sb.WriteByte(b[i+1])
						i += 2
					}
					continue
				}
				if b[i] == '\n' {
					sb.Reset()
					break
				}
				sb.WriteByte(b[i])
				i++
			}
			if s := sb.String(); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func sourceDictCandidate(s string) bool {
	if len(s) < 2 || len(s) > maxBinaryDictToken {
		return false
	}
	if strings.ContainsAny(s, "\n\r\t") || strings.Contains(s, "%") || strings.Contains(s, " ") {
		return false
	}
	if strings.HasPrefix(s, "-") || strings.Contains(s, "::") || strings.Contains(s, "/") {
		return false
	}
	if len(s) >= 12 && strings.Contains(s, "_") && s == strings.ToLower(s) {
		return false
	}
	// Include names and build artefacts are not input grammar.
	for _, ext := range []string{".h", ".c", ".cc", ".cpp", ".hpp", ".exe", ".o", ".a", ".so"} {
		if strings.HasSuffix(strings.ToLower(s), ext) {
			return false
		}
	}
	return true
}
