package fuzzengine

import "strings"

// CorpusPersistNamespace returns a stable key for cross-campaign corpus reuse.
// Namespaces are always tenant-scoped when an owner/payer is present so two
// customers on the same Dig guard_pack cannot share raw fuzz seeds (cross-tenant
// leak). Pack-only namespaces without owner_ref/payer_ref are refused (empty).
func CorpusPersistNamespace(cfg map[string]any) string {
	if cfg == nil {
		return ""
	}
	owner := sanitizeCorpusNamespace(toString(cfg["owner_ref"]))
	if owner == "" {
		owner = sanitizeCorpusNamespace(toString(cfg["payer_ref"]))
	}
	if v := strings.TrimSpace(toString(cfg["corpus_persist_key"])); v != "" {
		key := sanitizeCorpusNamespace(v)
		if key == "" {
			return ""
		}
		// Never honor a bare/global key: require tenant scope.
		if owner == "" {
			return ""
		}
		return "owner:" + owner + ":key:" + key
	}
	pack := strings.TrimSpace(toString(cfg["guard_pack"]))
	if pack == "" {
		pack = strings.TrimSpace(toString(cfg["guard_name"]))
	}
	if pack != "" {
		pack = sanitizeCorpusNamespace(pack)
		if pack == "" {
			return ""
		}
		if owner == "" {
			// Shared pack:<id> namespaces are forbidden (cross-tenant leak).
			return ""
		}
		return "owner:" + owner + ":pack:" + pack
	}
	if owner != "" {
		return "owner:" + owner
	}
	return ""
}

// CorpusNamespaceOwnedBy reports whether ns is scoped to the given owner/payer ref.
func CorpusNamespaceOwnedBy(ns, ownerRef string) bool {
	ns = strings.TrimSpace(strings.ToLower(ns))
	owner := sanitizeCorpusNamespace(ownerRef)
	if ns == "" || owner == "" {
		return false
	}
	prefix := "owner:" + owner
	return ns == prefix || strings.HasPrefix(ns, prefix+":")
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

// SanitizeCorpusNamespacePublic exposes namespace sanitization for research slot IDs.
func SanitizeCorpusNamespacePublic(s string) string {
	return sanitizeCorpusNamespace(s)
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
	out := b.String()
	if len(out) > 96 {
		out = out[:96]
	}
	return out
}
