package store

import (
	"context"
	"database/sql"
)

type AccountRow struct {
	Address      string
	BalanceUnits uint64
	NextNonce    uint64
}

type PoolTxRow struct {
	TxHash     string
	TxJSON     string
	From       string
	To         string
	Nonce      uint64
	FeeUnits   uint64
	AmountUnit uint64
	ReceivedAt int64
	Status     string
	RejectCode string
}

func UpsertAccount(ctx context.Context, db queryExecer, row AccountRow) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO accounts (address, balance_units, next_nonce, updated_at)
		 VALUES (?, ?, ?, strftime('%s','now'))
		 ON CONFLICT(address) DO UPDATE SET
		  balance_units=excluded.balance_units,
		  next_nonce=excluded.next_nonce,
		  updated_at=excluded.updated_at`,
		row.Address, row.BalanceUnits, row.NextNonce,
	)
	return err
}

func LoadAccount(ctx context.Context, db *sql.DB, address string) (AccountRow, bool, error) {
	var row AccountRow
	var bal, nonce uint64
	err := db.QueryRowContext(ctx, `SELECT address, balance_units, next_nonce FROM accounts WHERE address = ?`, address).
		Scan(&row.Address, &bal, &nonce)
	if err == sql.ErrNoRows {
		return AccountRow{}, false, nil
	}
	if err != nil {
		return AccountRow{}, false, err
	}
	row.BalanceUnits = bal
	row.NextNonce = nonce
	return row, true, nil
}

type queryExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}
