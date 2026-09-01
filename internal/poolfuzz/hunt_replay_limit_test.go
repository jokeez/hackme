package poolfuzz

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestAcquireHuntReplaySlotRespectsLimit(t *testing.T) {
	t.Setenv("HACKME_POOL_HUNT_REPLAY_MAX_PARALLEL", "2")
	huntReplaySlots = nil
	huntReplaySlotsOnce = sync.Once{}

	ctx := context.Background()
	r1, err := acquireHuntReplaySlot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := acquireHuntReplaySlot(ctx)
	if err != nil {
		t.Fatal(err)
	}

	waiting := make(chan error, 1)
	go func() {
		waitCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		_, err := acquireHuntReplaySlot(waitCtx)
		waiting <- err
	}()
	select {
	case err := <-waiting:
		if err == nil {
			t.Fatal("third acquire should block until timeout")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("third acquire did not return on timeout")
	}

	r1()
	r2()
	_, err = acquireHuntReplaySlot(ctx)
	if err != nil {
		t.Fatalf("slot should be free after release: %v", err)
	}
}

func TestHuntReplayMaxParallelEnv(t *testing.T) {
	t.Setenv("HACKME_POOL_HUNT_REPLAY_MAX_PARALLEL", "bogus")
	if got := huntReplayMaxParallel(); got != defaultHuntReplayMaxParallel {
		t.Fatalf("bogus env: got %d want %d", got, defaultHuntReplayMaxParallel)
	}
	t.Setenv("HACKME_POOL_HUNT_REPLAY_MAX_PARALLEL", "99")
	if got := huntReplayMaxParallel(); got != 32 {
		t.Fatalf("cap: got %d want 32", got)
	}
}
