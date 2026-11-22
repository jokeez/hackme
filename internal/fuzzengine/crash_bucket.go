package fuzzengine

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"
)

var (
	reSanPID     = regexp.MustCompile(`==\d+==`)
	reSanAddr    = regexp.MustCompile(`0x[0-9a-fA-F]{6,}`)
	reSanThread  = regexp.MustCompile(`thread T\d+`)
	reSanBuildID = regexp.MustCompile(`\(BuildId: [^)]+\)`)
	reSanLineNum = regexp.MustCompile(`:\d+:`)
	// symbolize=0 still prints (module+0xoffset). The offset is stable across
	// ASLR; the absolute "at pc 0x..." and the temp path are not.
	reModuleOff = regexp.MustCompile(`\(([^()\s]+)\+(0x[0-9a-fA-F]+)\)`)
)

// NormalizeSanitizerMsg extracts stable text from multi-line sanitizer output.
func NormalizeSanitizerMsg(msg string) string {
	kind, site := parseSanitizerMsg(msg)
	switch {
	case kind != "" && site != "":
		return kind + "|" + site
	case kind != "":
		return kind
	default:
		s := strings.TrimSpace(msg)
		s = reSanPID.ReplaceAllString(s, "==PID==")
		s = reSanAddr.ReplaceAllString(s, "0xADDR")
		s = reSanThread.ReplaceAllString(s, "thread T")
		s = reSanBuildID.ReplaceAllString(s, "")
		s = reSanLineNum.ReplaceAllString(s, ":LN:")
		if len(s) > 240 {
			s = s[:240]
		}
		return strings.ToLower(strings.TrimSpace(s))
	}
}

func parseSanitizerMsg(msg string) (kind, site string) {
	lines := strings.Split(msg, "\n")
	for _, line := range lines {
		l := strings.TrimSpace(line)
		if isBannerLine(l) {
			continue
		}
		low := strings.ToLower(l)
		if strings.Contains(l, "ERROR:") || strings.Contains(l, "runtime error:") {
			if k := sanitizerKindFromLine(low); k != "" {
				kind = k
			}
		}
		if strings.HasPrefix(l, "SUMMARY:") {
			if s := summarySiteFromLine(l); s != "" {
				site = s
			}
		}
		if strings.Contains(low, "buffer overflow detected") {
			if kind == "" {
				kind = "stack_oob"
			}
		}
		if site == "" && strings.Contains(l, " in ") {
			if strings.Contains(l, "#0") || strings.Contains(l, "fuzz_check") || strings.Contains(l, "memset") {
				if s := frameSiteFromLine(l); s != "" {
					site = s
				}
			}
		}
	}
	if kind == "" {
		low := strings.ToLower(msg)
		if strings.Contains(low, "addresssanitizer") {
			kind = sanitizerKindFromLine(low)
		}
	}
	return kind, site
}

func isBannerLine(line string) bool {
	s := strings.TrimSpace(line)
	if s == "" {
		return true
	}
	if len(s) >= 3 && strings.Trim(s, "=") == "" {
		return true
	}
	return false
}

func sanitizerKindFromLine(low string) string {
	switch {
	case strings.Contains(low, "stack-buffer-overflow"):
		return "stack_oob"
	case strings.Contains(low, "heap-buffer-overflow"):
		return "heap_oob"
	case strings.Contains(low, "global-buffer-overflow"):
		return "global_oob"
	case strings.Contains(low, "use-after-free"):
		return "uaf"
	case strings.Contains(low, "use-after-scope"):
		return "uas"
	case strings.Contains(low, "double-free"):
		return "double_free"
	case strings.Contains(low, "heap-use-after-free"):
		return "uaf"
	case strings.Contains(low, "segmentation fault") || strings.Contains(low, "sigsegv"):
		return "sigsegv"
	case strings.Contains(low, "undefined-behavior") || strings.Contains(low, "runtime error"):
		return ubsanKind(low)
	case strings.Contains(low, "divide by zero") || strings.Contains(low, "div-by-zero"):
		return "div0"
	case strings.Contains(low, "out of bounds"):
		return "wasm_oob"
	case strings.Contains(low, "leak"):
		return "leak"
	default:
		return ""
	}
}

func summarySiteFromLine(summary string) string {
	idx := strings.LastIndex(summary, " in ")
	if idx < 0 {
		return ""
	}
	site := strings.TrimSpace(summary[idx+4:])
	site = reSanLineNum.ReplaceAllString(site, "")
	site = reSanBuildID.ReplaceAllString(site, "")
	return strings.ToLower(strings.TrimSpace(site))
}

func frameSiteFromLine(line string) string {
	idx := strings.LastIndex(line, " in ")
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(line[idx+4:])
	if sp := strings.Index(rest, " "); sp > 0 {
		rest = rest[:sp]
	}
	rest = reSanLineNum.ReplaceAllString(rest, "")
	return strings.ToLower(strings.TrimSpace(rest))
}

// CanonicalModulePC normalizes a module-relative offset to "+0x" + lowercase hex
// with leading zeros stripped. Absolute addresses and empty input return "".
func CanonicalModulePC(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "pc=")
	s = strings.TrimPrefix(s, "+")
	if !strings.HasPrefix(s, "0x") {
		return ""
	}
	h := strings.TrimPrefix(s, "0x")
	if h == "" || len(h) > 16 {
		return ""
	}
	for _, r := range h {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return ""
		}
	}
	h = strings.TrimLeft(h, "0")
	if h == "" {
		h = "0"
	}
	return "+0x" + h
}

// FirstFailingModulePC is the first target (module+offset) in a symbolize=0
// ASAN report. SUMMARY wins when it names the target. Frames inside the ASAN
// runtime, libc, or the dynamic linker are skipped so every memcpy overflow
// does not share one interceptor offset. A runtime-only stack returns "".
func FirstFailingModulePC(msg string) string {
	if strings.TrimSpace(msg) == "" {
		return ""
	}
	type hit struct {
		pc      string
		runtime bool
		summary bool
	}
	var hits []hit
	for _, line := range strings.Split(msg, "\n") {
		summary := strings.HasPrefix(strings.TrimSpace(line), "SUMMARY:")
		for _, m := range reModuleOff.FindAllStringSubmatch(line, -1) {
			pc := CanonicalModulePC(m[2])
			if pc == "" {
				continue
			}
			hits = append(hits, hit{pc: pc, runtime: runtimeModule(m[1]), summary: summary})
		}
	}
	for _, h := range hits {
		if h.summary && !h.runtime {
			return h.pc
		}
	}
	for _, h := range hits {
		if !h.runtime {
			return h.pc
		}
	}
	return ""
}

func runtimeModule(path string) bool {
	base := path
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		base = path[i+1:]
	}
	low := strings.ToLower(base)
	if strings.Contains(low, "libclang_rt") || strings.Contains(low, "libasan") {
		return true
	}
	for _, p := range []string{
		"libc.so", "libc-", "ld-linux", "ld-musl", "linux-vdso",
		"libdl.so", "libpthread.so", "libgcc_s", "libstdc++", "libm.so",
	} {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
}

// huntCrashPC reads "hunt_crash:<subtype> pc=+0x..." stamped by coordinator replay.
func huntCrashPC(execErr string) (kind, pc string, ok bool) {
	s := strings.TrimSpace(execErr)
	if !strings.HasPrefix(s, "hunt_crash:") {
		return "", "", false
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	rest := strings.TrimPrefix(s, "hunt_crash:")
	sub := rest
	tail := ""
	if i := strings.IndexAny(rest, " \t"); i >= 0 {
		sub = rest[:i]
		tail = strings.TrimSpace(rest[i+1:])
	}
	if !strings.HasPrefix(strings.ToLower(tail), "pc=") {
		return "", "", false
	}
	pc = CanonicalModulePC(tail)
	if pc == "" {
		return "", "", false
	}
	kind = sanitizerKindFromLine(strings.ToLower(sub))
	if kind == "" {
		kind = "asan"
	}
	return kind, pc, true
}

// StableCrashBucket returns a dedup key for crash-class outcomes (one bug ≈ one bucket).
// symbolize=0 reports key on error kind + module-relative PC, so two heap-buffer-overflows
// at different instructions pay separately and the same instruction does not pay twice
// when the PID, absolute address, or temp binary path changes.
func StableCrashBucket(findingType, execErr string) string {
	_ = findingType
	if execErr == "" {
		return "crash|generic"
	}
	if strings.HasPrefix(execErr, "native_silent_exit|") {
		parts := strings.SplitN(execErr, "|", 3)
		if len(parts) >= 2 {
			return "crash|silent|" + parts[1]
		}
	}
	if kind, pc, ok := huntCrashPC(execErr); ok {
		return stableCrashKey(kind, pc)
	}
	kind, site := parseSanitizerMsg(execErr)
	if pc := FirstFailingModulePC(execErr); pc != "" && kind != "" {
		return stableCrashKey(kind, pc)
	}
	if kind != "" {
		if site == "" {
			site = inferSiteFromMsg(execErr)
		}
		return stableCrashKey(kind, site)
	}
	low := strings.ToLower(execErr)
	if strings.Contains(low, "buffer overflow detected") {
		site := inferSiteFromMsg(execErr)
		return stableCrashKey("stack_oob", site)
	}
	norm := NormalizeSanitizerMsg(execErr)
	if norm != "" {
		return "crash|" + hashStable(norm)
	}
	return "crash|generic"
}

func stableCrashKey(kind, site string) string {
	if site == "" {
		switch kind {
		case "stack_oob":
			site = "memset"
		case "heap_oob":
			site = "heap"
		}
	}
	if site != "" {
		return "crash|" + kind + "|" + site
	}
	return "crash|" + kind
}

func inferSiteFromMsg(msg string) string {
	_, site := parseSanitizerMsg(msg)
	if site != "" {
		return site
	}
	for _, line := range strings.Split(msg, "\n") {
		l := strings.TrimSpace(line)
		if strings.Contains(l, "fuzz_check") {
			return "fuzz_check"
		}
		if strings.Contains(l, " in memset") {
			return "memset"
		}
	}
	return ""
}

func ubsanKind(norm string) string {
	switch {
	case strings.Contains(norm, "function-type-mismatch") ||
		strings.Contains(norm, "function-pointer-cast") ||
		strings.Contains(norm, "call to function"):
		return "fn_ptr_cast"
	case strings.Contains(norm, "misaligned") || strings.Contains(norm, "unaligned"):
		return "misaligned"
	case strings.Contains(norm, "null pointer") || strings.Contains(norm, "null-pointer"):
		return "null_deref"
	case strings.Contains(norm, "signed integer overflow") || strings.Contains(norm, "integer overflow"):
		return "int_overflow"
	case strings.Contains(norm, "overflow"):
		return "overflow"
	case strings.Contains(norm, "shift"):
		return "shift"
	case strings.Contains(norm, "bool") && strings.Contains(norm, "invalid"):
		return "invalid_bool"
	case strings.Contains(norm, "load of value") || strings.Contains(norm, "invalid value"):
		return "invalid_value"
	case strings.Contains(norm, "vptr") || strings.Contains(norm, "dynamic type"):
		return "vptr"
	default:
		return hashStable(norm)
	}
}

// FindingFamily is the stable root-cause key for customer reports
// (many unique inputs → one family). Alias of StableCrashBucket.
func FindingFamily(findingType, execErr string) string {
	return StableCrashBucket(findingType, execErr)
}

// CountFindingFamilies returns unique root-cause buckets for a set of sanitizer messages.
func CountFindingFamilies(findingType string, msgs []string) int {
	seen := map[string]struct{}{}
	for _, m := range msgs {
		k := FindingFamily(findingType, m)
		seen[k] = struct{}{}
	}
	return len(seen)
}

func hashStable(s string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return fmt.Sprintf("%08x", h.Sum32())
}

// StableFindingKeyFromDetail builds a report dedup key from pool finding detail_json.
func StableFindingKeyFromDetail(findingType string, detail map[string]any) string {
	ft := strings.TrimSpace(strings.ToLower(findingType))
	if detail != nil {
		if trap := strings.TrimSpace(anyString(detail["trap"])); trap != "" {
			return StableCrashBucket(ft, trap)
		}
	}
	if ft == "guard_violation" {
		// One customer guard = one property: N violating inputs are one finding with N
		// reproducers, not N "unique crashes" (the uint64 op/item split is meaningless here).
		return "guard_violation|guard"
	}
	if IsCrashClass(ft) && detail != nil {
		op := anyInt(detail["op_type"])
		item := anyInt(detail["item_id"])
		return fmt.Sprintf("crash|detector|op=%d|item_hi=%d", op, item&0xff)
	}
	return ft + "|generic"
}

func anyString(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

func anyInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case string:
		var n int
		_, _ = fmt.Sscanf(strings.TrimSpace(x), "%d", &n)
		return n
	default:
		return 0
	}
}
