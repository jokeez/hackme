package workerid

import (
	"os"
	"strings"
)

// SanitizeHostname maps a host name to a stable coordinator worker id segment
// (alphanumeric + hyphen, lowercased). Used by the node and workerpoh binaries.
func SanitizeHostname(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	var b strings.Builder
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "local"
	}
	return out
}

// DefaultDesktop returns worker-<hostname> for dashboard fallbacks when WORKER_ID is unset.
func DefaultDesktop() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return "worker-local"
	}
	return "worker-" + SanitizeHostname(host)
}
