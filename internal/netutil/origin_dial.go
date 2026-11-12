package netutil

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
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
// When set, HTTPS/HTTP dials whose hostname equals DIAL_HOST connect to DIAL
// while preserving the original Host / SNI so the publicly trusted leaf verifies.

var (
	originDialOnce      sync.Once
	originDialIP        string
	originDialHost      string
	originTransportOnce sync.Once
	originTransport     http.RoundTripper
)

func loadOriginDial() {
	originDialOnce.Do(func() {
		raw := strings.TrimSpace(os.Getenv("HACKME_POOL_ORIGIN_DIAL"))
		// Pin must be a literal IP — hostnames would reintroduce DNS / CDN path.
		if ip := net.ParseIP(raw); raw != "" && ip == nil {
			raw = ""
		} else if raw != "" {
			ip := net.ParseIP(raw)
			// Always reject metadata/link-local/CGNAT; private/loopback only with lab flag.
			if IPUnsafeForEgress(ip, true) || IPUnsafeForEgress(ip, AllowPrivateEgress()) {
				raw = ""
			}
		}
		originDialIP = raw
		originDialHost = strings.ToLower(strings.TrimSpace(os.Getenv("HACKME_POOL_ORIGIN_DIAL_HOST")))
		originDialHost = strings.TrimSuffix(originDialHost, ".")
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

// ResetOriginDialForTest clears cached env + transport (tests only).
func ResetOriginDialForTest() {
	originDialOnce = sync.Once{}
	originTransportOnce = sync.Once{}
	originDialIP = ""
	originDialHost = ""
	originTransport = nil
}

func hostMatchesOriginDial(hostname string) bool {
	loadOriginDial()
	if originDialIP == "" {
		return false
	}
	h := strings.ToLower(strings.TrimSpace(hostname))
	h = strings.Trim(h, "[]")
	h = strings.TrimSuffix(h, ".")
	return h == originDialHost
}

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

// OriginDialTransport returns a process-wide RoundTripper. When
// HACKME_POOL_ORIGIN_DIAL is set, matching hosts dial that IP with normal TLS
// verification (SNI stays the URL hostname). Built once so idle conns are reused.
func OriginDialTransport() http.RoundTripper {
	originTransportOnce.Do(func() {
		loadOriginDial()
		base, ok := http.DefaultTransport.(*http.Transport)
		if !ok || base == nil {
			originTransport = http.DefaultTransport
			return
		}
		tr := base.Clone()
		// Align with coordinatorHTTPClient — HTTP/2 stall storms on large bodies.
		tr.ForceAttemptHTTP2 = false
		tr.TLSNextProto = map[string]func(authority string, c *tls.Conn) http.RoundTripper{}
		if originDialIP != "" {
			tr.DialContext = originDialContext
			// Clone inherits ProxyFromEnvironment. A system HTTPS_PROXY would
			// send pinned-host traffic through the proxy/CDN and silently undo
			// the origin pin (Hunt harness + settle outbox hang >~24KiB).
			// Defense in depth: when pin is configured, disable proxy entirely on
			// this opt-in client (poolsync/hunt are not general egress).
			tr.Proxy = func(req *http.Request) (*url.URL, error) {
				if req != nil && req.URL != nil && hostMatchesOriginDial(req.URL.Hostname()) {
					return nil, nil
				}
				// Non-pinned hosts on this client: still direct (no CONNECT undo risk).
				return nil, nil
			}
		}
		originTransport = tr
	})
	return originTransport
}

// HTTPClientPoolOriginDial is the opt-in client for Orders poolsync and Hunt
// harness fetch. Unrelated callers must keep using HTTPClientNoRedirect.
func HTTPClientPoolOriginDial(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		CheckRedirect: DenyAdminTokenRedirect,
		Transport:     OriginDialTransport(),
	}
}
