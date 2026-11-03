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
