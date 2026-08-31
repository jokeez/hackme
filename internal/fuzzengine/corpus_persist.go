package fuzzengine

import "strings"

// CorpusPersistNamespace returns stable key for cross-campaign corpus reuse.
func CorpusPersistNamespace(cfg map[string]any) string {
	if cfg == nil {
		return ""
	}
	if v := strings.TrimSpace(toString(cfg["corpus_persist_key"])); v != "" {
		return sanitizeCorpusNamespace(v)
	}
	if v := strings.TrimSpace(toString(cfg["guard_pack"])); v != "" {
		return "pack:" + sanitizeCorpusNamespace(v)
	}
	if v := strings.TrimSpace(toString(cfg["guard_name"])); v != "" {
		return "pack:" + sanitizeCorpusNamespace(v)
	}
	if v := strings.TrimSpace(toString(cfg["owner_ref"])); v != "" {
		return "owner:" + sanitizeCorpusNamespace(v)
	}
	if v := strings.TrimSpace(toString(cfg["payer_ref"])); v != "" {
		return "payer:" + sanitizeCorpusNamespace(v)
	}
	return ""
}

// CorpusPersistEnabled reports whether pool corpus should import/export a namespace.
func CorpusPersistEnabled(cfg map[string]any) bool {
	ns := CorpusPersistNamespace(cfg)
	if ns == "" {
		return false
	}
	if v, ok := cfg["corpus_persist"]; ok {
		s := strings.TrimSpace(strings.ToLower(toString(v)))
		return s == "true" || s == "1" || s == "yes" || s == "on"
	}
	return GuidedSchedulingEnabled(cfg) || ParseDepthTier(cfg) == DepthBytesCorpus
}

// CorpusPersistMax returns max seeds imported from a namespace per campaign.
func CorpusPersistMax(cfg map[string]any) int {
	if cfg == nil {
		return 64
	}
	if v, ok := cfg["corpus_persist_max"]; ok {
		n := intFromAny(v)
		if n >= 8 && n <= PoolCorpusMax(cfg) {
			return n
		}
	}
	max := PoolCorpusMax(cfg) / 4
	if max < 32 {
		max = 32
	}
	if max > 128 {
		max = 128
	}
	return max
}

func sanitizeCorpusNamespace(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == ':', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if len(out) > 120 {
		out = out[:120]
	}
	return out
}
