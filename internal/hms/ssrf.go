package hms

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

// ValidateWorkerEndpointURL normalizes and fail-closes worker push endpoints (HMC-RES-03).
// Rejects non-http(s), userinfo, missing host, and private/loopback/link-local/metadata
// destinations unless HMS_ALLOW_PRIVATE_ENDPOINTS=1 (lab/LAN).
func ValidateWorkerEndpointURL(raw string) (string, error) {
	raw = strings.TrimSpace(strings.TrimRight(raw, "/"))
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid endpoint url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("endpoint url scheme must be http or https")
	}
	if u.User != nil {
		return "", fmt.Errorf("endpoint url must not include userinfo")
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return "", fmt.Errorf("endpoint url missing host")
	}
	if err := assertHostSafeForEgress(host, allowPrivateEndpoints()); err != nil {
		return "", err
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	u.RawPath = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func allowPrivateEndpoints() bool {
	for _, k := range []string{"HMS_ALLOW_PRIVATE_ENDPOINTS", "HACKME_HMS_ALLOW_PRIVATE_ENDPOINTS"} {
		v := strings.TrimSpace(strings.ToLower(os.Getenv(k)))
		if v == "1" || v == "true" || v == "yes" || v == "on" {
			return true
		}
	}
	return false
}

func assertHostSafeForEgress(host string, allowPrivate bool) error {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return fmt.Errorf("empty host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ipUnsafeForEgress(ip, allowPrivate) {
			return fmt.Errorf("endpoint host resolves to blocked address class")
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("endpoint host dns lookup failed")
	}
	if len(ips) == 0 {
		return fmt.Errorf("endpoint host dns returned no addresses")
	}
	for _, ip := range ips {
		if ipUnsafeForEgress(ip, allowPrivate) {
			return fmt.Errorf("endpoint host resolves to blocked address class")
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
	// Always block cloud/link-local metadata style ranges.
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		// 169.254.0.0/16 already covered by IsLinkLocalUnicast; keep explicit.
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
