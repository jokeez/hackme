package main

import (
	"errors"
	"testing"
	"time"
)

func TestPoolSyncRetryBackoffSecGrowsAndCaps(t *testing.T) {
	if got := poolSyncRetryBackoffSec(0); got != 180 {
		t.Fatalf("streak 0 = %d, want 180", got)
	}
	if got := poolSyncRetryBackoffSec(1); got != 180 {
		t.Fatalf("streak 1 = %d, want 180", got)
	}
	if got := poolSyncRetryBackoffSec(2); got != 360 {
		t.Fatalf("streak 2 = %d, want 360", got)
	}
	if got := poolSyncRetryBackoffSec(3); got != 720 {
		t.Fatalf("streak 3 = %d, want 720", got)
	}
	prev := int64(0)
	for streak := 1; streak <= 40; streak++ {
		got := poolSyncRetryBackoffSec(streak)
		if got < prev {
			t.Fatalf("streak %d went backwards: %d < %d", streak, got, prev)
		}
		if got > poolSyncRetryBackoffCapSec {
			t.Fatalf("streak %d = %d exceeds cap %d", streak, got, poolSyncRetryBackoffCapSec)
		}
		prev = got
	}
	if prev != poolSyncRetryBackoffCapSec {
		t.Fatalf("backoff never reached the cap: %d", prev)
	}
}

func TestPoolSyncRetryDueBacksOffStuckCampaign(t *testing.T) {
	a := &app{}
	const id = "hunt-bootstrap-duktape-20261120t013917z"

	if !a.poolSyncRetryDue(id) {
		t.Fatal("an unknown campaign must be retryable")
	}

	// Permanently stuck: missing local harness artifact, nothing the node can fix.
	a.poolSyncMarkFailed(id, errors.New("hunt harness sync: local artifact: hunt artifact: de2b275b not found"))
	if a.poolSyncRetryDue(id) {
		t.Fatal("a just-failed campaign must wait for its backoff window")
	}
	if got := a.poolSyncFailStreak[id]; got != 1 {
		t.Fatalf("streak = %d, want 1", got)
	}

	for i := 0; i < 8; i++ {
		a.poolSyncMarkFailed(id, errors.New("hunt harness sync: local artifact: hunt artifact: de2b275b not found"))
	}
	wait := a.poolSyncRetryAt[id] - time.Now().Unix()
	if wait < 1800 {
		t.Fatalf("repeated failures must stretch the window, got %ds", wait)
	}
	if wait > poolSyncRetryBackoffCapSec+5 {
		t.Fatalf("window %ds exceeds cap %ds", wait, poolSyncRetryBackoffCapSec)
	}

	// Due again once the window has passed.
	a.poolSyncMu.Lock()
	a.poolSyncRetryAt[id] = time.Now().Unix() - 1
	a.poolSyncMu.Unlock()
	if !a.poolSyncRetryDue(id) {
		t.Fatal("campaign must be retryable after its window")
	}

	// A success clears the backoff so the next failure starts from 3m again.
	a.poolSyncMarkOK(id)
	if _, still := a.poolSyncRetryAt[id]; still {
		t.Fatal("mark ok must clear the retry window")
	}
	if _, still := a.poolSyncFailStreak[id]; still {
		t.Fatal("mark ok must clear the failure streak")
	}
	if !a.poolSyncRetryDue(id) {
		t.Fatal("campaign must be retryable after success")
	}
}
