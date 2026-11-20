package p2p

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestEgressDialContextBlocksLoopbackWhenPrivateDisallowed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	dial := egressDialContext(false)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := dial(ctx, "tcp", ln.Addr().String())
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected dial to loopback to fail when allowPrivate=false")
	}
}

func TestEgressDialContextAllowsLoopbackWhenPrivateAllowed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	dial := egressDialContext(true)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := dial(ctx, "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("expected LAN/loopback dial OK when allowPrivate=true: %v", err)
	}
	_ = conn.Close()
}
