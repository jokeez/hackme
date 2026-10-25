package netutil

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIsLoopbackURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"http://127.0.0.1:18081", true},
		{"http://localhost:8080", true},
		{"http://[::1]:8080", true},
		{"https://hackme.tech/pool", false},
		{"http://127.0.0.1.attacker.example/", false},
		{"http://evil.com/path?x=127.0.0.1", false},
		{"http://127.0.0.1:8080@evil.example", false}, // report #24 userinfo spoof
		{"http://localhost:99@evil.example/", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsLoopbackURL(tc.in); got != tc.want {
			t.Fatalf("%q: got %v want %v", tc.in, got, tc.want)
		}
	}
}

func TestLooksRemoteCoordinatorURL(t *testing.T) {
	if LooksRemoteCoordinatorURL("http://127.0.0.1:18081") {
		t.Fatal("loopback should not look remote")
	}
	if !LooksRemoteCoordinatorURL("https://hackme.tech/pool/coordinator") {
		t.Fatal("public URL should look remote")
	}
}

func TestHTTPClientNoRedirectRefusesFollow(t *testing.T) {
	var sawSecond bool
	mux := http.NewServeMux()
	mux.HandleFunc("/hop1", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/hop2", http.StatusFound)
	})
	mux.HandleFunc("/hop2", func(w http.ResponseWriter, r *http.Request) {
		sawSecond = true
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cli := HTTPClientNoRedirect(5 * time.Second)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/hop1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Hackme-Admin-Token", "secret-should-not-leak")
	_, err = cli.Do(req)
	if err == nil {
		t.Fatal("expected redirect refusal error")
	}
	if !strings.Contains(err.Error(), "refusing HTTP redirect") {
		t.Fatalf("unexpected err: %v", err)
	}
	if sawSecond {
		t.Fatal("client must not follow redirect to hop2")
	}
}

func TestIPUnsafeForEgress(t *testing.T) {
	if !IPUnsafeForEgress(net.ParseIP("127.0.0.1"), false) {
		t.Fatal("loopback must be unsafe without allowPrivate")
	}
	if IPUnsafeForEgress(net.ParseIP("127.0.0.1"), true) {
		t.Fatal("loopback allowed when allowPrivate")
	}
	if !IPUnsafeForEgress(net.ParseIP("169.254.169.254"), true) {
		t.Fatal("link-local metadata always unsafe")
	}
	if !IPUnsafeForEgress(net.ParseIP("10.0.0.1"), false) {
		t.Fatal("private must be unsafe by default")
	}
	if IPUnsafeForEgress(net.ParseIP("8.8.8.8"), false) {
		t.Fatal("public IP must be safe")
	}
}

func TestAssertHostSafeForEgress(t *testing.T) {
	if err := AssertHostSafeForEgress("127.0.0.1", false); err == nil {
		t.Fatal("expected loopback reject")
	}
	if err := AssertHostSafeForEgress("8.8.8.8", false); err != nil {
		t.Fatalf("public literal: %v", err)
	}
}

func TestSafeHTTPBaseURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://hackme.tech/pool/coordinator", true},
		{"http://127.0.0.1:18081", true},
		{"file:///etc/passwd", false},
		{"gopher://x", false},
		{"http://127.0.0.1@evil.example/", false},
		{"https://user:pass@evil.example/", false},
		{"", false},
		{"not-a-url", false},
	}
	for _, tc := range cases {
		if got := SafeHTTPBaseURL(tc.in); got != tc.want {
			t.Fatalf("SafeHTTPBaseURL(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

func TestSafeHTTPURLForEgress(t *testing.T) {
	t.Setenv("HACKME_ALLOW_PRIVATE_EGRESS", "0")
	if !SafeHTTPURLForEgress("https://hackme.tech/pool/coordinator") {
		t.Fatal("public https must pass")
	}
	if SafeHTTPURLForEgress("http://169.254.169.254/latest/meta-data") {
		t.Fatal("metadata must fail")
	}
	if SafeHTTPURLForEgress("http://127.0.0.1:18081") {
		t.Fatal("loopback must fail without ALLOW_PRIVATE_EGRESS")
	}
	t.Setenv("HACKME_ALLOW_PRIVATE_EGRESS", "1")
	if !SafeHTTPURLForEgress("http://127.0.0.1:18081") {
		t.Fatal("loopback must pass with ALLOW_PRIVATE_EGRESS")
	}
	if SafeHTTPURLForEgress("http://169.254.169.254/") {
		t.Fatal("metadata must still fail with ALLOW_PRIVATE_EGRESS")
	}
}

func TestSafeHTTPURLForEgressAllowLab(t *testing.T) {
	t.Setenv("HACKME_ALLOW_PRIVATE_EGRESS", "0")
	if !SafeHTTPURLForEgressAllowLab("http://127.0.0.1:18081") {
		t.Fatal("lab helper must allow loopback coordinator")
	}
	if SafeHTTPURLForEgressAllowLab("http://169.254.169.254/latest/meta-data") {
		t.Fatal("lab helper must still block metadata")
	}
}
