package chain

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// OrdersPrepaidNormalizeAddr returns the ledger key for tenant prepaid.
// Long addresses collapse to legacy short form so Fund memo and Owner-Ref match.
func OrdersPrepaidNormalizeAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if IsLongAddress(addr) {
		if short := LegacyAddressOfLong(addr); short != "" {
			return short
		}
	}
	return addr
}

// OrdersFundOwnerFromMemo extracts the prepaid owner from an orders_fund_v1 memo.
func OrdersFundOwnerFromMemo(memo string) string {
	m := strings.TrimSpace(memo)
	prefix := OrdersFundMemoPrefix + ":"
	if !strings.HasPrefix(m, prefix) {
		return ""
	}
	return OrdersPrepaidNormalizeAddr(strings.TrimSpace(strings.TrimPrefix(m, prefix)))
}

// GetOrdersPrepaidUnits returns tenant prepaid balance (0 if missing).
func (s *Service) GetOrdersPrepaidUnits(ctx context.Context, ownerAddr string) (uint64, error) {
	ownerAddr = OrdersPrepaidNormalizeAddr(ownerAddr)
	if ownerAddr == "" {
		return 0, errors.New("orders prepaid: owner address required")
	}
	var units uint64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(balance_units,0) FROM orders_prepaid WHERE address=?`, ownerAddr).Scan(&units)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return units, err
}

// GetOrdersPrepaidHMC is GetOrdersPrepaidUnits in HMC.
func (s *Service) GetOrdersPrepaidHMC(ctx context.Context, ownerAddr string) (float64, error) {
	u, err := s.GetOrdersPrepaidUnits(ctx, ownerAddr)
	if err != nil {
		return 0, err
	}
	return UnitsToHMC(u), nil
}

func (s *Service) creditOrdersPrepaidTx(ctx context.Context, tx *sql.Tx, ownerAddr string, units uint64) error {
	ownerAddr = OrdersPrepaidNormalizeAddr(ownerAddr)
	if ownerAddr == "" || units == 0 {
		return nil
	}
	if !FitsSQLiteBindUint64(units) {
		return fmt.Errorf("orders prepaid: credit units out of range (%d)", units)
	}
	now := time.Now().Unix()
	_, err := tx.ExecContext(ctx,
		`INSERT INTO orders_prepaid (address, balance_units, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(address) DO UPDATE SET
		   balance_units = orders_prepaid.balance_units + excluded.balance_units,
		   updated_at = excluded.updated_at`,
		ownerAddr, units, now)
	return err
}

func (s *Service) debitOrdersPrepaidTx(ctx context.Context, tx *sql.Tx, ownerAddr string, units uint64) error {
	ownerAddr = OrdersPrepaidNormalizeAddr(ownerAddr)
	if ownerAddr == "" {
		return errors.New("orders prepaid: owner address required")
	}
	if units == 0 {
		return nil
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE orders_prepaid SET balance_units = balance_units - ?, updated_at = strftime('%s','now')
		 WHERE address = ? AND balance_units >= ?`,
		units, ownerAddr, units)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrFuzzInsufficientBalance
	}
	return nil
}

const metaOrdersPrepaidFundTxPrefix = "orders_prepaid_fund_tx:"

func ordersPrepaidFundMetaKey(txHash string) string {
	return metaOrdersPrepaidFundTxPrefix + strings.TrimSpace(strings.ToLower(txHash))
}

// CreditOrdersFundForOwner credits tenant prepaid on the hub for a proven Fund Orders tx.
// Unlike CreditOrdersFund, owner need not be this node's primary wallet.
// Uses a distinct meta key from local wallet fund so the same on-chain tx can credit
// both customer local spendable and hub tenant prepaid (rc20).
func (s *Service) CreditOrdersFundForOwner(ctx context.Context, ownerAddr, txHash string, amountUnits uint64) (*OrdersFundResult, error) {
	ownerAddr = OrdersPrepaidNormalizeAddr(ownerAddr)
	txHash = strings.TrimSpace(strings.ToLower(txHash))
	if ownerAddr == "" {
		return nil, errors.New("orders fund: owner address required")
	}
	if !IsValidAddress(ownerAddr) {
		return nil, fmt.Errorf("orders fund: invalid owner address %s", ownerAddr)
	}
	if txHash == "" || len(txHash) < 16 {
		return nil, errors.New("orders fund: tx_hash required")
	}
	if amountUnits == 0 {
		return nil, errors.New("orders fund: amount_units required")
	}
	amountHMC := UnitsToHMC(amountUnits)
	if err := ValidateOrdersFundAmountHMC(amountHMC); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	metaKey := ordersPrepaidFundMetaKey(txHash)
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, metaKey).Scan(&raw)
	if err == nil && strings.TrimSpace(raw) != "" {
		var prev OrdersFundResult
		if json.Unmarshal([]byte(raw), &prev) == nil && prev.TxHash != "" {
			prevOwner := OrdersPrepaidNormalizeAddr(prev.FromAddress)
			if prevOwner != "" && prevOwner != ownerAddr {
				return nil, fmt.Errorf("orders fund: tx_hash already credited to %s", prev.FromAddress)
			}
			if prev.AmountUnits != amountUnits {
				return nil, fmt.Errorf("orders fund: tx_hash already credited for %d units (got %d)", prev.AmountUnits, amountUnits)
			}
			prev.IdempotentReplay = true
			_ = tx.Rollback()
			return &prev, nil
		}
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	if err := s.creditOrdersPrepaidTx(ctx, tx, ownerAddr, amountUnits); err != nil {
		return nil, err
	}
	var balUnits uint64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(balance_units,0) FROM orders_prepaid WHERE address=?`, ownerAddr).Scan(&balUnits); err != nil {
		return nil, err
	}

	out := OrdersFundResult{
		TxHash:         txHash,
		AmountHMC:      amountHMC,
		AmountUnits:    amountUnits,
		SpendableAfter: UnitsToHMC(balUnits),
		FromAddress:    ownerAddr,
	}
	blob, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		metaKey, string(blob)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &out, nil
}
