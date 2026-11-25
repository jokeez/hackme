package chain

import (
	"strings"
	"time"
)

// sqliteBusyAttempts bounds a retry of a write that lost the SQLite lock race.
const sqliteBusyAttempts = 6

// IsSQLiteBusyErr reports a lock-contention failure — the only error class worth retrying.
//
// Why these happen at all, despite busy_timeout(60000) in the DSN: a deferred transaction
// reads before it writes, so when another writer commits in between, the upgrade fails at
// once with SQLITE_BUSY_SNAPSHOT (517) and busy_timeout never applies. The coordinator hit
// the same wall and lost ~1000 accrual persists a day until work_accrual_persist switched
// to BEGIN IMMEDIATE.
//
// AppendPoHBlock still runs under a deferred transaction, and it passes *sql.Tx into a
// dozen helpers, so converting it is a change for its own review. Measured 2026-11-25 on
// the live hub: "database is locked" accounted for 3 of 73 and then 1 of 41 refused
// baseline relays — about 2.4% of found blocks thrown away, each one a block that was
// never minted.
func IsSQLiteBusyErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "database is locked") ||
		strings.Contains(s, "database table is locked") ||
		strings.Contains(s, "database busy") ||
		strings.Contains(s, "sqlite_busy")
}

// retryOnSQLiteBusy runs do until it succeeds, fails for any other reason, or runs out of
// attempts. sleep is injectable so a test can drive the loop without waiting.
//
// Safe for a mint path only because every attempt re-reads the tip and re-validates the
// nonce from scratch: if the tip moved while we waited, the solve is correctly refused
// rather than applied to a chain it no longer matches.
func retryOnSQLiteBusy(attempts int, sleep func(time.Duration), do func() error) error {
	if attempts < 1 {
		attempts = 1
	}
	if sleep == nil {
		sleep = time.Sleep
	}
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			sleep(time.Duration(attempt*50) * time.Millisecond)
		}
		err = do()
		if !IsSQLiteBusyErr(err) {
			return err
		}
	}
	return err
}
