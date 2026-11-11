package main

import (
	"errors"
	"fmt"
	"testing"
)

func TestSettleLocalPullResult(t *testing.T) {
	auth := fmt.Errorf("settle outbox HTTP 403: forbidden")
	other := errors.New("fuzz settle pull: pay failures blocked drain")

	if err := settleLocalPullResult(2, 1, nil, auth); err != nil {
		t.Fatalf("one 403 beside a successful drain must not backoff, got %v", err)
	}
	if err := settleLocalPullResult(4, 4, nil, auth); !errors.Is(err, auth) {
		t.Fatalf("all credentialed attempts auth-failed: got %v", err)
	}
	if err := settleLocalPullResult(0, 0, nil, nil); err != nil {
		t.Fatalf("legacy skips (no credentialed attempts) must not backoff, got %v", err)
	}
	if err := settleLocalPullResult(3, 1, other, auth); !errors.Is(err, other) {
		t.Fatalf("non-auth error must win over sibling 403, got %v", err)
	}
	if err := settleLocalPullResult(1, 0, nil, nil); err != nil {
		t.Fatalf("clean drain must stay quiet, got %v", err)
	}
}
