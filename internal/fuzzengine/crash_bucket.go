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
	case strings.Contains(low, "use-after-free"):
		return "uaf"
	case strings.Contains(low, "undefined-behavior") || strings.Contains(low, "runtime error"):
		return ubsanKind(low)
	case strings.Contains(low, "divide by zero"):
		return "div0"
	case strings.Contains(low, "out of bounds"):
		return "wasm_oob"
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

// StableCrashBucket returns a dedup key for crash-class outcomes (one bug ≈ one bucket).
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
	kind, site := parseSanitizerMsg(execErr)
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
	case strings.Contains(norm, "overflow"):
		return "overflow"
	case strings.Contains(norm, "shift"):
		return "shift"
	default:
		return hashStable(norm)
	}
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
