package netutil

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Origin dial bypasses CDN middleboxes that stall large POST/GET bodies
// (field: Cloudflare ≈24KiB hang on Hunt harness + settle outbox).
//
//	HACKME_POOL_ORIGIN_DIAL=132.243.112.100
//	HACKME_POOL_ORIGIN_DIAL_HOST=hackme.tech   // optional; default hackme.tech
//
// When set, HTTPS/HTTP dials whose hostname matches DIAL_HOST connect to DIAL
// while preserving the original Host / SNI (certificate name).

var (
	originDialOnce sync.Once
	originDialIP   string
	originDialHost string
)

func loadOriginDial() {
	originDialOnce.Do(func() {
		originDialIP = strings.TrimSpace(os.Getenv("HACKME_POOL_ORIGIN_DIAL"))
		originDialHost = strings.ToLower(strings.TrimSpace(os.Getenv("HACKME_POOL_ORIGIN_DIAL_HOST")))
		if originDialHost == "" {
			originDialHost = "hackme.tech"
		}
	})
}

// OriginDialConfigured reports whether HACKME_POOL_ORIGIN_DIAL is set.
func OriginDialConfigured() bool {
	loadOriginDial()
	return originDialIP != ""
}

// ResetOriginDialForTest clears the cached env (tests only).
func ResetOriginDialForTest() {
	originDialOnce = sync.Once{}
	originDialIP = ""
	originDialHost = ""
}

func hostMatchesOriginDial(hostname string) bool {
	loadOriginDial()
	if originDialIP == "" {
		return false
	}
	h := strings.ToLower(strings.TrimSpace(hostname))
	h = strings.Trim(h, "[]")
	if h == originDialHost {
		return true
	}
	// Allow www.hackme.tech when dial host is hackme.tech.
	if strings.HasSuffix(h, "."+originDialHost) {
		return true
	}
	return false
}

// originDialContext dials originDialIP when the requested host matches, else default.
func originDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	loadOriginDial()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	if originDialIP != "" && hostMatchesOriginDial(host) {
		return d.DialContext(ctx, network, net.JoinHostPort(originDialIP, port))
	}
	return d.DialContext(ctx, network, addr)
}

// OriginAwareTransport returns a clone of http.DefaultTransport with optional
// origin IP dial + TLS ServerName preserved for the logical host.
func OriginAwareTransport() http.RoundTripper {
	loadOriginDial()
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok || base == nil {
		return http.DefaultTransport
	}
	tr := base.Clone()
	if originDialIP == "" {
		return tr
	}
	tr.DialContext = originDialContext
	// Ensure TLS handshake uses the SNI/hostname from the URL, not the dial IP.
	if tr.TLSClientConfig == nil {
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	} else {
		tr.TLSClientConfig = tr.TLSClientConfig.Clone()
	}
	// Origin :443 often serves a self-signed / incomplete chain while CF holds the
	// public leaf. Opt-in only — never default insecure.
	insecure := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_POOL_ORIGIN_DIAL_INSECURE")))
	if insecure == "1" || insecure == "true" || insecure == "yes" || insecure == "on" {
		tr.TLSClientConfig.InsecureSkipVerify = true
	}
	return tr
}
