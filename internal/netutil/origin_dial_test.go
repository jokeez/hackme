package netutil

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOriginDialRewritesMatchingHost(t *testing.T) {
	t.Cleanup(ResetOriginDialForTest)
	t.Setenv("HACKME_ALLOW_PRIVATE_EGRESS", "1")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if host != "hackme.tech" && host != "hackme.tech:"+port {
			t.Errorf("Host=%q want hackme.tech", host)
		}
		_, _ = io.WriteString(w, "ok-origin")
	})}
	go srv.Serve(ln) //nolint:errcheck
	defer srv.Close()

	t.Setenv("HACKME_POOL_ORIGIN_DIAL", "127.0.0.1")
	t.Setenv("HACKME_POOL_ORIGIN_DIAL_HOST", "hackme.tech")
	ResetOriginDialForTest()

	cl := HTTPClientPoolOriginDial(5 * time.Second)
	req, err := http.NewRequest(http.MethodGet, "http://hackme.tech:"+port+"/probe", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if string(body) != "ok-origin" {
		t.Fatalf("body=%q", body)
	}
}

func TestOriginDialBypassesEnvProxyForPinnedHost(t *testing.T) {
	t.Cleanup(ResetOriginDialForTest)
	t.Setenv("HACKME_ALLOW_PRIVATE_EGRESS", "1")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok-direct")
	})}
	go srv.Serve(ln) //nolint:errcheck
	defer srv.Close()

	// Dead proxy — if OriginDialTransport kept ProxyFromEnvironment, the
	// request would fail instead of dialing the pinned origin IP.
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("http_proxy", "http://127.0.0.1:1")
	t.Setenv("https_proxy", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	t.Setenv("HACKME_POOL_ORIGIN_DIAL", "127.0.0.1")
	t.Setenv("HACKME_POOL_ORIGIN_DIAL_HOST", "hackme.tech")
	ResetOriginDialForTest()

	cl := HTTPClientPoolOriginDial(5 * time.Second)
	req, err := http.NewRequest(http.MethodGet, "http://hackme.tech:"+port+"/probe", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if string(body) != "ok-direct" {
		t.Fatalf("body=%q (proxy must not intercept pinned host)", body)
	}
}

func TestOriginDialLeavesOtherHostsAlone(t *testing.T) {
	t.Cleanup(ResetOriginDialForTest)
	t.Setenv("HACKME_POOL_ORIGIN_DIAL", "203.0.113.9")
	t.Setenv("HACKME_POOL_ORIGIN_DIAL_HOST", "hackme.tech")
	ResetOriginDialForTest()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "local")
	}))
	defer srv.Close()

	cl := HTTPClientPoolOriginDial(5 * time.Second)
	res, err := cl.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if string(body) != "local" {
		t.Fatalf("body=%q", body)
	}
}

func TestOriginDialExactHostOnly(t *testing.T) {
	t.Cleanup(ResetOriginDialForTest)
	t.Setenv("HACKME_POOL_ORIGIN_DIAL", "203.0.113.9")
	t.Setenv("HACKME_POOL_ORIGIN_DIAL_HOST", "hackme.tech")
	ResetOriginDialForTest()

	if hostMatchesOriginDial("api.hackme.tech") {
		t.Fatal("subdomain must not match exact host")
	}
	if hostMatchesOriginDial("evil-hackme.tech") {
		t.Fatal("suffix lookalike must not match")
	}
	if !hostMatchesOriginDial("hackme.tech") {
		t.Fatal("exact host must match")
	}
	if !hostMatchesOriginDial("HackMe.Tech") {
		t.Fatal("exact host match is case-insensitive")
	}
}

func TestHTTPClientNoRedirectIgnoresOriginDial(t *testing.T) {
	t.Cleanup(ResetOriginDialForTest)
	t.Setenv("HACKME_POOL_ORIGIN_DIAL", "203.0.113.9")
	t.Setenv("HACKME_POOL_ORIGIN_DIAL_HOST", "hackme.tech")
	ResetOriginDialForTest()

	cl := HTTPClientNoRedirect(5 * time.Second)
	if cl.Transport != nil {
		t.Fatal("shared HTTPClientNoRedirect must not install origin dial transport")
	}
}

func TestOriginDialConfigured(t *testing.T) {
	t.Cleanup(ResetOriginDialForTest)
	t.Setenv("HACKME_POOL_ORIGIN_DIAL", "")
	ResetOriginDialForTest()
	if OriginDialConfigured() {
		t.Fatal("empty dial must be off")
	}
	t.Setenv("HACKME_POOL_ORIGIN_DIAL", "132.243.112.100")
	ResetOriginDialForTest()
	if !OriginDialConfigured() {
		t.Fatal("want configured")
	}
}

func TestOriginDialRejectsHostnamePin(t *testing.T) {
	t.Cleanup(ResetOriginDialForTest)
	t.Setenv("HACKME_POOL_ORIGIN_DIAL", "evil.example")
	t.Setenv("HACKME_POOL_ORIGIN_DIAL_HOST", "hackme.tech")
	ResetOriginDialForTest()
	if OriginDialConfigured() {
		t.Fatal("hostname pin must be ignored; only literal IPs are allowed")
	}
}

func TestOriginDialRejectsMetadataPin(t *testing.T) {
	t.Cleanup(ResetOriginDialForTest)
	t.Setenv("HACKME_ALLOW_PRIVATE_EGRESS", "1")
	t.Setenv("HACKME_POOL_ORIGIN_DIAL", "169.254.169.254")
	t.Setenv("HACKME_POOL_ORIGIN_DIAL_HOST", "hackme.tech")
	ResetOriginDialForTest()
	if OriginDialConfigured() {
		t.Fatal("metadata pin must be rejected")
	}
}

func TestOriginDialTrailingDotHostMatches(t *testing.T) {
	t.Cleanup(ResetOriginDialForTest)
	t.Setenv("HACKME_ALLOW_PRIVATE_EGRESS", "1")
	t.Setenv("HACKME_POOL_ORIGIN_DIAL", "127.0.0.1")
	t.Setenv("HACKME_POOL_ORIGIN_DIAL_HOST", "hackme.tech")
	ResetOriginDialForTest()
	if !hostMatchesOriginDial("hackme.tech.") {
		t.Fatal("trailing FQDN dot must still match pin host")
	}
}
