package chain

import (
	"errors"
	"testing"
	"time"
)

// Lock contention is the only error worth retrying on a mint path: anything else means the
// solve is wrong and a retry would just lose the wall.
func TestIsSQLiteBusyErr(t *testing.T) {
	for _, e := range []error{
		errors.New("database is locked"),
		errors.New("SQLITE_BUSY: database is locked (5) (sqlite_busy)"),
		errors.New("database table is locked: blocks"),
		errors.New("Database Busy"),
	} {
		if !IsSQLiteBusyErr(e) {
			t.Errorf("IsSQLiteBusyErr(%q) = false, want true", e)
		}
	}
	for _, e := range []error{
		nil,
		errors.New("chain: invalid poh solution for submitted target_mod"),
		errors.New("chain: empty (genesis required)"),
		errors.New("context deadline exceeded"),
	} {
		if IsSQLiteBusyErr(e) {
			t.Errorf("IsSQLiteBusyErr(%v) = true, want false", e)
		}
	}
}

func TestRetryOnSQLiteBusy(t *testing.T) {
	busy := errors.New("database is locked")

	// Succeeds on the third try: the found block survives a lost lock race instead of
	// being discarded, which is the whole point.
	calls, slept := 0, 0
	err := retryOnSQLiteBusy(6, func(time.Duration) { slept++ }, func() error {
		calls++
		if calls < 3 {
			return busy
		}
		return nil
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
	if slept != 2 {
		t.Fatalf("slept %d times, want 2 (no sleep before the first attempt)", slept)
	}

	// A validation failure must not be retried: it will fail identically every time.
	bad := errors.New("chain: invalid poh solution for submitted target_mod")
	calls = 0
	if err := retryOnSQLiteBusy(6, func(time.Duration) {}, func() error {
		calls++
		return bad
	}); !errors.Is(err, bad) {
		t.Fatalf("err = %v, want the validation error", err)
	}
	if calls != 1 {
		t.Fatalf("a non-busy error was retried %d times", calls)
	}

	// Exhausted attempts return the last contention error rather than a success.
	calls = 0
	if err := retryOnSQLiteBusy(4, func(time.Duration) {}, func() error {
		calls++
		return busy
	}); !IsSQLiteBusyErr(err) {
		t.Fatalf("err = %v, want the contention error", err)
	}
	if calls != 4 {
		t.Fatalf("calls = %d, want 4 attempts", calls)
	}

	// Backoff grows, so a long writer is waited out rather than hammered.
	var waits []time.Duration
	_ = retryOnSQLiteBusy(4, func(d time.Duration) { waits = append(waits, d) }, func() error { return busy })
	for i := 1; i < len(waits); i++ {
		if waits[i] <= waits[i-1] {
			t.Fatalf("backoff did not grow: %v", waits)
		}
	}

	// Degenerate attempts still run once: never skip the write entirely.
	calls = 0
	_ = retryOnSQLiteBusy(0, nil, func() error { calls++; return nil })
	if calls != 1 {
		t.Fatalf("attempts=0 ran the body %d times, want 1", calls)
	}
}
