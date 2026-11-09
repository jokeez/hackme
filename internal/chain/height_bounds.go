package chain

import (
	"errors"
	"math"
)

// ErrHeightOutOfRange is returned when a client uint64 cannot be bound to the
// SQLite INTEGER path (database/sql rejects uint64 with the high bit set).
// Name kept for the first height-filter fix; also used for nonce / amount binds.
var ErrHeightOutOfRange = errors.New("block height out of supported range")

// MaxSQLiteBlockHeight is the largest uint64 the default SQLite backend can bind
// (signed int64). Values above this used to parse as uint64 then 500 on query.
const MaxSQLiteBlockHeight = uint64(math.MaxInt64)

// FitsSQLiteBlockHeight reports whether h can be passed to database/sql as a
// SQLite INTEGER bind (block_index, nonce, amount_units, fee_units, …).
func FitsSQLiteBlockHeight(h uint64) bool {
	return h <= MaxSQLiteBlockHeight
}

// FitsSQLiteBindUint64 is an alias for FitsSQLiteBlockHeight (non-height binds).
func FitsSQLiteBindUint64(v uint64) bool { return FitsSQLiteBlockHeight(v) }

// CheckSQLiteBlockHeight returns ErrHeightOutOfRange when h is outside the
// supported bind range.
func CheckSQLiteBlockHeight(h uint64) error {
	if !FitsSQLiteBlockHeight(h) {
		return ErrHeightOutOfRange
	}
	return nil
}

// CheckSQLiteBindUint64 is an alias for CheckSQLiteBlockHeight.
func CheckSQLiteBindUint64(v uint64) error { return CheckSQLiteBlockHeight(v) }
