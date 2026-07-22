package main

import "testing"

func TestCoordinatorBindLoopbackOnlyEmptyHost(t *testing.T) {
	if coordinatorBindLoopbackOnly(":8081") {
		t.Fatal("empty host must not count as loopback")
	}
	if coordinatorBindLoopbackOnly("0.0.0.0:8081") {
		t.Fatal("0.0.0.0 must not count as loopback")
	}
	if !coordinatorBindLoopbackOnly("127.0.0.1:8081") {
		t.Fatal("127.0.0.1 must be loopback")
	}
}
