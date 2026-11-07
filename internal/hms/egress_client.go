package hms

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// egressHTTPClient dials only after re-checking the peer IP against the SSRF
// blocklist at connect time (audit H12 — closes DNS rebinding TOCTOU).
func egressHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	allowPrivate := allowPrivateEndpoints()
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     false,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: timeout,
		IdleConnTimeout:       30 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			host = strings.Trim(host, "[]")
			var ips []net.IP
			if ip := net.ParseIP(host); ip != nil {
				ips = []net.IP{ip}
			} else {
				ips, err = net.DefaultResolver.LookupIP(ctx, "ip", host)
				if err != nil {
					return nil, fmt.Errorf("egress dns: %w", err)
				}
			}
			var last error
			for _, ip := range ips {
				if ipUnsafeForEgress(ip, allowPrivate) {
					last = fmt.Errorf("egress blocked address class %s", ip)
					continue
				}
				target := net.JoinHostPort(ip.String(), port)
				conn, derr := dialer.DialContext(ctx, network, target)
				if derr == nil {
					return conn, nil
				}
				last = derr
			}
			if last == nil {
				last = fmt.Errorf("egress: no safe addresses for %s", host)
			}
			return nil, last
		},
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		// Never follow redirects: Authorization would be replayed onto a new host.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
