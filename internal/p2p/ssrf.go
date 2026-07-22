package p2p

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

func envTruthy(keys ...string) bool {
	for _, k := range keys {
		v := strings.TrimSpace(strings.ToLower(os.Getenv(k)))
		if v == "1" || v == "true" || v == "yes" || v == "on" {
			return true
		}
	}
	return false
}

func allowPrivateDiscovery() bool {
	return envTruthy("HACKME_P2P_ALLOW_PRIVATE_DISCOVERY", "HACKME_P2P_ALLOW_PRIVATE_PEERS")
}

func allowTokenToDiscovered() bool {
	return envTruthy("HACKME_P2P_TOKEN_TO_DISCOVERED")
}

// validateStaticPeerURL allows operator-configured LAN peers but still blocks
// metadata/link-local ranges and credentialed URLs.
func validateStaticPeerURL(raw string) string {
	norm := normalizePeerURL(raw)
	if norm == "" {
		return ""
	}
	if err := assertPeerURLSafe(norm, true); err != nil {
		return ""
	}
	return norm
}

// validateDiscoveredPeerURL fail-closes discovered peers that resolve to
// private/loopback/link-local/metadata unless HACKME_P2P_ALLOW_PRIVATE_DISCOVERY=1.
func validateDiscoveredPeerURL(raw string) (string, error) {
	norm := normalizePeerURL(raw)
	if norm == "" {
		return "", fmt.Errorf("invalid peer url")
	}
	if err := assertPeerURLSafe(norm, allowPrivateDiscovery()); err != nil {
		return "", err
	}
	return norm, nil
}

func assertPeerURLSafe(norm string, allowPrivate bool) error {
	u, err := url.Parse(norm)
	if err != nil {
		return fmt.Errorf("invalid peer url")
	}
	if u.User != nil {
		return fmt.Errorf("peer url must not include userinfo")
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return fmt.Errorf("peer url missing host")
	}
	return assertHostSafeForEgress(host, allowPrivate)
}

func assertHostSafeForEgress(host string, allowPrivate bool) error {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return fmt.Errorf("empty host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ipUnsafeForEgress(ip, allowPrivate) {
			return fmt.Errorf("peer host is blocked address class")
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("peer host dns lookup failed")
	}
	if len(ips) == 0 {
		return fmt.Errorf("peer host dns returned no addresses")
	}
	for _, ip := range ips {
		if ipUnsafeForEgress(ip, allowPrivate) {
			return fmt.Errorf("peer host resolves to blocked address class")
		}
	}
	return nil
}

func ipUnsafeForEgress(ip net.IP, allowPrivate bool) bool {
	if ip == nil {
		return true
	}
	if ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
	}
	if !allowPrivate {
		if ip.IsLoopback() || ip.IsPrivate() {
			return true
		}
	}
	return false
}
