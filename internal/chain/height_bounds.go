package chain

import (
	"errors"
	"math"
)

// ErrHeightOutOfRange is returned when a client height cannot be bound to the
// SQLite INTEGER path (database/sql rejects uint64 with the high bit set).
var ErrHeightOutOfRange = errors.New("block height out of supported range")

// MaxSQLiteBlockHeight is the largest height the default SQLite backend can bind
// (signed int64). Values above this used to parse as uint64 then 500 on query.
const MaxSQLiteBlockHeight = uint64(math.MaxInt64)

// FitsSQLiteBlockHeight reports whether h can be passed to database/sql as a
// block_index bind argument on the default SQLite backend.
func FitsSQLiteBlockHeight(h uint64) bool {
	return h <= MaxSQLiteBlockHeight
}

// CheckSQLiteBlockHeight returns ErrHeightOutOfRange when h is outside the
// supported bind range.
func CheckSQLiteBlockHeight(h uint64) error {
	if !FitsSQLiteBlockHeight(h) {
		return ErrHeightOutOfRange
	}
	return nil
}
