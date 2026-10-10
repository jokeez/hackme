package fuzzupstream

import (
	"path/filepath"
	"regexp"
	"strings"
)

// catalogIDRE matches a single catalog target id segment (no path separators).
var catalogIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

// SanitizeCatalogID returns a single safe path segment for a catalog target id.
// Rejects empty, ".", "..", and any value with path separators.
func SanitizeCatalogID(id string) (string, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", false
	}
	base := filepath.Base(id)
	if base != id {
		// Contained a separator — reject rather than silently truncate.
		return "", false
	}
	if base == "." || base == ".." {
		return "", false
	}
	if !catalogIDRE.MatchString(base) {
		return "", false
	}
	return base, true
}
