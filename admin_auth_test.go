package main

import (
	"net/http"
	"testing"
)

func TestRequestFromLoopbackUsesRemoteAddrNotHost(t *testing.T) {
	req := httptestReq(t, "127.0.0.1:54321", "evil.example")
	if !requestFromLoopback(req) {
		t.Fatal("loopback RemoteAddr must be trusted even when Host is spoofed")
	}

	req = httptestReq(t, "203.0.113.9:443", "localhost")
	if requestFromLoopback(req) {
		t.Fatal("Host: localhost from non-loopback RemoteAddr must NOT get loopback trust")
	}

	req = httptestReq(t, "10.0.0.5:9999", "127.0.0.1")
	if requestFromLoopback(req) {
		t.Fatal("Host: 127.0.0.1 from non-loopback RemoteAddr must NOT get loopback trust")
	}

	req = httptestReq(t, "[::1]:8080", "example.com")
	if !requestFromLoopback(req) {
		t.Fatal("::1 RemoteAddr must be loopback")
	}
}

func TestRequestFromLoopbackRejectsForwardedHeaders(t *testing.T) {
	req := httptestReq(t, "127.0.0.1:1", "127.0.0.1")
	req.Header.Set("X-Forwarded-For", "203.0.113.1")
	if requestFromLoopback(req) {
		t.Fatal("X-Forwarded-For must deny loopback trust")
	}

	req = httptestReq(t, "127.0.0.1:1", "127.0.0.1")
	req.Header.Set("X-Real-IP", "203.0.113.1")
	if requestFromLoopback(req) {
		t.Fatal("X-Real-IP must deny loopback trust")
	}
}

func httptestReq(t *testing.T, remoteAddr, host string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+host+"/api/desktop/local-auth", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.RemoteAddr = remoteAddr
	req.Host = host
	return req
}
