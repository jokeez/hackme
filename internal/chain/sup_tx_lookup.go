package chain

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// SupTxByHash returns a SUP transfer's status (history first, then the pending pool), so a
// settler can confirm inclusion before advancing its watermark (same contract as /api/tx/{hash}).
func (s *Service) SupTxByHash(ctx context.Context, txHash string) (TransferStatusRow, bool, error) {
	txHash = strings.TrimSpace(txHash)
	var r TransferStatusRow
	var txJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT tx_hash, status, block_index, block_hash, reject_code, from_address, to_address, amount_units, fee_units, nonce, tx_json
		 FROM sup_tx_history WHERE tx_hash = ?`, txHash).
		Scan(&r.TxHash, &r.Status, &r.BlockIndex, &r.BlockHash, &r.RejectCode, &r.From, &r.To, &r.AmountUnits, &r.FeeUnits, &r.Nonce, &txJSON)
	if err == nil {
		r.Memo = memoFromTransferTxJSON(txJSON)
		return r, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return TransferStatusRow{}, false, err
	}
	err = s.db.QueryRowContext(ctx,
		`SELECT tx_hash, status, from_address, to_address, amount_units, fee_units, nonce, reject_code, tx_json
		 FROM sup_tx_pool WHERE tx_hash = ?`, txHash).
		Scan(&r.TxHash, &r.Status, &r.From, &r.To, &r.AmountUnits, &r.FeeUnits, &r.Nonce, &r.RejectCode, &txJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return TransferStatusRow{}, false, nil
	}
	if err != nil {
		return TransferStatusRow{}, false, err
	}
	r.Memo = memoFromTransferTxJSON(txJSON)
	return r, true, nil
}

// SupTxsByFromNonce returns SUP transfers that used (from, nonce): history (included/rejected)
// and pending pool rows. Mirrors TransfersByFromNonce so the SUP settler can resolve
// empty-hash stranded intents without settling on nonce movement alone (valax).
func (s *Service) SupTxsByFromNonce(ctx context.Context, from string, nonce uint64) ([]TransferStatusRow, error) {
	from = strings.TrimSpace(from)
	if err := CheckSQLiteBindUint64(nonce); err != nil {
		return nil, err
	}
	var out []TransferStatusRow
	rows, err := s.db.QueryContext(ctx,
		`SELECT tx_hash, status, block_index, block_hash, reject_code, from_address, to_address, amount_units, fee_units, nonce, tx_json
		 FROM sup_tx_history WHERE from_address = ? AND nonce = ? LIMIT 16`, from, nonce)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r TransferStatusRow
		var txJSON string
		if err := rows.Scan(&r.TxHash, &r.Status, &r.BlockIndex, &r.BlockHash, &r.RejectCode, &r.From, &r.To, &r.AmountUnits, &r.FeeUnits, &r.Nonce, &txJSON); err != nil {
			_ = rows.Close()
			return nil, err
		}
		r.Memo = memoFromTransferTxJSON(txJSON)
		out = append(out, r)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	rows, err = s.db.QueryContext(ctx,
		`SELECT tx_hash, status, from_address, to_address, amount_units, fee_units, nonce, reject_code, tx_json
		 FROM sup_tx_pool WHERE from_address = ? AND nonce = ? AND status = 'pending' LIMIT 16`, from, nonce)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r TransferStatusRow
		var txJSON string
		if err := rows.Scan(&r.TxHash, &r.Status, &r.From, &r.To, &r.AmountUnits, &r.FeeUnits, &r.Nonce, &r.RejectCode, &txJSON); err != nil {
			return nil, err
		}
		r.Memo = memoFromTransferTxJSON(txJSON)
		out = append(out, r)
	}
	return out, rows.Err()
}
