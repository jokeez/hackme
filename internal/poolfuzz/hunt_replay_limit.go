package poolfuzz

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
)

const defaultHuntReplayMaxParallel = 3

var (
	huntReplaySlots     chan struct{}
	huntReplaySlotsOnce sync.Once
)

func huntReplayMaxParallel() int {
	v := strings.TrimSpace(os.Getenv("HACKME_POOL_HUNT_REPLAY_MAX_PARALLEL"))
	if v == "" {
		return defaultHuntReplayMaxParallel
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return defaultHuntReplayMaxParallel
	}
	if n > 32 {
		return 32
	}
	return n
}

// acquireHuntReplaySlot limits concurrent coordinator Hunt ASAN replays (CPU protection).
func acquireHuntReplaySlot(ctx context.Context) (release func(), err error) {
	huntReplaySlotsOnce.Do(func() {
		n := huntReplayMaxParallel()
		huntReplaySlots = make(chan struct{}, n)
	})
	select {
	case huntReplaySlots <- struct{}{}:
		return func() { <-huntReplaySlots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
