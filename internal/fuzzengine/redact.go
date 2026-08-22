package fuzzengine

import (
	"encoding/hex"
	"regexp"
	"strings"
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`ASIA[0-9A-Z]{16}`),
	regexp.MustCompile(`ghp_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`sk_live_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`sk-proj-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`AIza[0-9A-Za-z_-]{30,}`),
	regexp.MustCompile(`-----BEGIN[A-Z ]*PRIVATE KEY-----`),
}

const maxRedactInput = 8192

// RedactSensitiveString masks common secret/token patterns for public reports.
func RedactSensitiveString(s string) string {
	if len(s) > maxRedactInput {
		s = s[:maxRedactInput] + "…(truncated)"
	}
	out := s
	for _, re := range secretPatterns {
		out = re.ReplaceAllStringFunc(out, redactToken)
	}
	out = redactKVSecrets(out, "SECRET")
	out = redactKVSecrets(out, "PASSWORD")
	out = redactKVSecrets(out, "TOKEN")
	out = redactKVSecrets(out, "API_KEY")
	return out
}

func redactToken(s string) string {
	if len(s) <= 8 {
		return "…redacted…"
	}
	return s[:4] + "…" + s[len(s)-4:] + " (redacted)"
}

// redactKVSecrets masks values after KEY= without re-matching the same site
// (re-scanning after partial redact previously grew strings forever on SECRET=…).
func redactKVSecrets(s, key string) string {
	needle := strings.ToUpper(key) + "="
	upper := strings.ToUpper(s)
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		rel := strings.Index(upper[i:], needle)
		if rel < 0 {
			b.WriteString(s[i:])
			break
		}
		idx := i + rel
		start := idx + len(needle)
		end := start
		for end < len(s) && s[end] != '\n' && s[end] != '\r' && s[end] != ' ' && s[end] != '"' && s[end] != '\'' {
			end++
		}
		b.WriteString(s[i:start])
		if end-start >= 8 {
			b.WriteString(redactToken(s[start:end]))
		} else {
			b.WriteString(s[start:end])
		}
		i = end
	}
	return b.String()
}

// RedactInputForReport returns a customer-safe repro preview from hex or decimal input.
func RedactInputForReport(inputHex string) string {
	inputHex = strings.TrimSpace(strings.ToLower(inputHex))
	inputHex = strings.TrimPrefix(inputHex, "0x")
	if inputHex == "" {
		return ""
	}
	if len(inputHex) > maxRedactInput*2 {
		inputHex = inputHex[:48] + "…" + inputHex[len(inputHex)-12:] + " (binary truncated)"
		return inputHex
	}
	raw, err := hex.DecodeString(inputHex)
	if err != nil {
		return RedactSensitiveString(inputHex)
	}
	if mostlyPrintableASCII(raw) {
		return RedactSensitiveString(string(raw))
	}
	if len(inputHex) > 48 {
		return inputHex[:24] + "…" + inputHex[len(inputHex)-12:] + " (binary truncated)"
	}
	return inputHex
}

// RedactInputNForReport redacts decimal/string input_n fields when they embed secrets.
func RedactInputNForReport(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(strings.ToLower(s), "0x")); err == nil && len(s) >= 8 {
		return RedactInputForReport(s)
	}
	return RedactSensitiveString(s)
}

func mostlyPrintableASCII(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	printable := 0
	for _, c := range b {
		if c == '\n' || c == '\r' || c == '\t' || (c >= 32 && c < 127) {
			printable++
		}
	}
	return printable*2 >= len(b)
}

func ContainsSensitivePattern(s string) bool {
	for _, re := range secretPatterns {
		if re.FindStringIndex(s) != nil {
			return true
		}
	}
	upper := strings.ToUpper(s)
	for _, k := range []string{"SECRET=", "PASSWORD=", "TOKEN=", "API_KEY="} {
		if strings.Contains(upper, k) {
			return true
		}
	}
	return false
}
