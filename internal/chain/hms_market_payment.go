package chain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hackme/internal/hms"
)

const metaHMSMarketEscrowUnits = "hms_market_escrow_units"

// HMSMarketPaymentResult is returned after debiting the node wallet for a storage order.
type HMSMarketPaymentResult struct {
	PaymentID      string  `json:"payment_id"`
	TotalDebitHMC  float64 `json:"total_debit_hmc"`
	StorageHMC     float64 `json:"storage_subtotal_hmc"`
	PlatformFeeHMC float64 `json:"platform_fee_hmc"`
	BurnHMC        float64 `json:"burn_hmc"`
	BalanceAfter   float64 `json:"balance_after"`
	QuoteHash      string  `json:"quote_hash"`
	PolicyHash     string  `json:"policy_hash"`
}

// PayHMSStorageMarket debits HMC per kernel-locked quote (rates from internal/hms, not env).
func (s *Service) PayHMSStorageMarket(ctx context.Context, label string, sizeBytes int64, retentionDays int, quoteHash string) (*HMSMarketPaymentResult, error) {
	q, err := hms.VerifyQuoteHash(sizeBytes, retentionDays, quoteHash)
	if err != nil {
		return nil, err
	}
	if q.PolicyHash != hms.MarketPricingPolicySnapshot().PolicyHash {
		return nil, fmt.Errorf("chain: hms market policy hash drift — rebuild node and coordinator")
	}
	storage := q.StorageSubtotalHMC
	fee := q.PlatformFeeHMC
	burn := q.BurnHMC
	total := q.TotalDebitHMC
	if total+1e-12 < hms.MarketMinPrepaidHMC {
		return nil, fmt.Errorf("chain: below min prepaid %.4f HMC", hms.MarketMinPrepaidHMC)
	}
	storageUnits := HMCToUnits(storage)
	feeUnits := HMCToUnits(fee)
	burnUnits := HMCToUnits(burn)
	totalUnits := storageUnits
	if ^uint64(0)-totalUnits < feeUnits {
		return nil, errors.New("chain: debit overflow")
	}
	totalUnits += feeUnits

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var balUnits uint64
	var walletAddr string
	if err := tx.QueryRowContext(ctx, `SELECT address, balance_units FROM wallet WHERE id = 1`).Scan(&walletAddr, &balUnits); err != nil {
		return nil, err
	}
	if balUnits < totalUnits {
		return nil, ErrInsufficientBalance
	}
	var accountUnits uint64
	if err := tx.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address = ?`, walletAddr).Scan(&accountUnits); err != nil {
		return nil, ErrInsufficientBalance
	}
	if accountUnits < totalUnits {
		return nil, ErrInsufficientBalance
	}
	if _, err := tx.ExecContext(ctx, `UPDATE wallet SET balance_units = balance_units - ?, balance_hmc = (balance_units - ?) / ? WHERE id = 1`,
		totalUnits, totalUnits, float64(UnitsPerHMC)); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE accounts SET balance_units = balance_units - ?, updated_at = strftime('%s','now') WHERE address = ?`,
		totalUnits, walletAddr); err != nil {
		return nil, err
	}
	if feeUnits > 0 {
		devAddr := DevFeeAddress
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, ?, 0, strftime('%s','now'))
			 ON CONFLICT(address) DO UPDATE SET balance_units=accounts.balance_units + excluded.balance_units, updated_at=excluded.updated_at`,
			devAddr, feeUnits); err != nil {
			return nil, err
		}
		if devAddr == walletAddr {
			if _, err := tx.ExecContext(ctx, `UPDATE wallet SET balance_units = balance_units + ?, balance_hmc = (balance_units + ?) / ? WHERE id = 1`,
				feeUnits, feeUnits, float64(UnitsPerHMC)); err != nil {
				return nil, err
			}
		}
	}
	if burnUnits > 0 {
		mintedUnits, burnedUnits, err := s.econTotalsUnits(ctx, tx)
		if err != nil {
			return nil, err
		}
		if err := s.upsertMetaUint(ctx, tx, metaTotalBurnedUnits, burnedUnits+burnUnits); err != nil {
			return nil, err
		}
		if err := s.upsertMetaFloat(ctx, tx, metaTotalBurnedHMC, UnitsToHMC(burnedUnits+burnUnits)); err != nil {
			return nil, err
		}
		if err := s.upsertMetaFloat(ctx, tx, metaTotalMintedHMC, UnitsToHMC(mintedUnits)); err != nil {
			return nil, err
		}
	}
	cur, err := s.metaUint(ctx, tx, metaHMSMarketEscrowUnits, 0)
	if err != nil {
		return nil, err
	}
	if ^uint64(0)-cur < storageUnits {
		return nil, errors.New("chain: hms market escrow overflow")
	}
	if err := s.upsertMetaUint(ctx, tx, metaHMSMarketEscrowUnits, cur+storageUnits); err != nil {
		return nil, err
	}
	paymentID := "hmsp-" + strings.ReplaceAll(strings.TrimSpace(label), " ", "-")
	if paymentID == "hmsp-" {
		paymentID = "hmsp"
	}
	paymentID = paymentID + "-" + fmt.Sprintf("%d", time.Now().UnixNano())
	if len(paymentID) > 120 {
		paymentID = paymentID[:120]
	}
	if err := s.checkEconomicInvariants(ctx, tx); err != nil {
		return nil, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT balance_units FROM wallet WHERE id = 1`).Scan(&balUnits); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &HMSMarketPaymentResult{
		PaymentID:      paymentID,
		TotalDebitHMC:  total,
		StorageHMC:     storage,
		PlatformFeeHMC: fee,
		BurnHMC:        burn,
		BalanceAfter:   UnitsToHMC(balUnits),
		QuoteHash:      q.QuoteHash,
		PolicyHash:     q.PolicyHash,
	}, nil
}
