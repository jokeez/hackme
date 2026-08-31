package chain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"hackme/internal/fuzzescrow"
)

// ApplyFuzzSettleOnce credits a settle kind at most once per durable event_id.
// The applied-event row and the escrow credit share one SQLite transaction, so a
// crash cannot leave a marked event unpaid or allow a timeout retry to double-pay.
func (s *Service) ApplyFuzzSettleOnce(ctx context.Context, eventID, kind, campaignID, minerAddress, severity string) (*FuzzEscrowRow, bool, error) {
	if s == nil || s.db == nil {
		return nil, false, fmt.Errorf("chain: no database")
	}
	eventID = strings.TrimSpace(eventID)
	kind = strings.TrimSpace(strings.ToLower(kind))
	campaignID = strings.TrimSpace(campaignID)
	minerAddress = strings.TrimSpace(minerAddress)
	severity = strings.TrimSpace(strings.ToLower(severity))
	if eventID == "" || campaignID == "" || kind == "" {
		return nil, false, fmt.Errorf("chain: settle once requires event_id, campaign_id, kind")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().Unix()
	res, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO fuzz_settle_applied (event_id, campaign_id, kind, applied_at) VALUES (?, ?, ?, ?)`,
		eventID, campaignID, kind, now)
	if err != nil {
		return nil, false, err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		// Already applied — safe no-op (lost ACK / relay timeout redelivery).
		_ = tx.Rollback()
		row, gerr := s.getFuzzEscrowUnlocked(ctx, campaignID)
		return row, false, gerr
	}

	var payErr error
	switch kind {
	case "run":
		payErr = s.payFuzzRunTx(ctx, tx, campaignID, minerAddress)
	case "finding", "bounty":
		payErr = s.payFuzzBountyTx(ctx, tx, campaignID, minerAddress, severity)
	case "crash_bonus", "unique_crash":
		payErr = s.payFuzzCrashBonusTx(ctx, tx, campaignID, minerAddress)
	case "finalize", "close":
		payErr = s.finalizeFuzzEscrowTx(ctx, tx, campaignID)
	default:
		payErr = fmt.Errorf("chain: unknown settle kind %q", kind)
	}
	if payErr != nil {
		// M5: do not mark applied on deplete/closed/already-paid — miner must not lose
		// the event when finalize races settle. Rollback drops the INSERT OR IGNORE row.
		return nil, true, payErr
	}
	if err := tx.Commit(); err != nil {
		return nil, true, err
	}
	row, gerr := s.getFuzzEscrowUnlocked(ctx, campaignID)
	return row, true, gerr
}

func isFuzzSettleDrainErr(err error) bool {
	return errors.Is(err, ErrFuzzEscrowClosed) ||
		errors.Is(err, ErrFuzzEscrowDepleted) ||
		errors.Is(err, ErrFuzzEscrowAlreadyPaid)
}

func (s *Service) getFuzzEscrowUnlocked(ctx context.Context, campaignID string) (*FuzzEscrowRow, error) {
	r, err := s.readFuzzEscrow(ctx, s.db, campaignID)
	if err != nil {
		return nil, err
	}
	return fuzzEscrowPublic(r), nil
}

func (s *Service) payFuzzRunTx(ctx context.Context, tx *sql.Tx, campaignID, minerAddress string) error {
	if !strings.HasPrefix(minerAddress, "HMC-") || len(minerAddress) != 20 {
		return errors.New("chain: valid miner_address required")
	}
	row, err := s.lockFuzzEscrowTx(ctx, tx, campaignID)
	if err != nil {
		return err
	}
	if row.status != "open" {
		return ErrFuzzEscrowClosed
	}
	if row.runsPaidUnits+row.perRunUnits > row.runsPoolUnits {
		return ErrFuzzEscrowDepleted
	}
	pay := row.perRunUnits
	if row.runsPaidUnits+pay > row.runsPoolUnits {
		pay = row.runsPoolUnits - row.runsPaidUnits
	}
	if pay == 0 {
		return ErrFuzzEscrowDepleted
	}
	if err := s.creditUnits(ctx, tx, minerAddress, pay); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`UPDATE fuzz_campaign_escrow SET runs_paid_units=runs_paid_units+?, runs_done=runs_done+1 WHERE campaign_id=?`,
		pay, campaignID)
	return err
}

// payFuzzCrashBonusTx pays a one-shot unique-crash bonus from the bounty pool without closing bounty.
func (s *Service) payFuzzCrashBonusTx(ctx context.Context, tx *sql.Tx, campaignID, minerAddress string) error {
	if !strings.HasPrefix(minerAddress, "HMC-") || len(minerAddress) != 20 {
		return errors.New("chain: valid miner_address required")
	}
	row, err := s.lockFuzzEscrowTx(ctx, tx, campaignID)
	if err != nil {
		return err
	}
	if row.status != "open" {
		return ErrFuzzEscrowClosed
	}
	if row.crashBonusPaidUnits > 0 {
		return ErrFuzzEscrowAlreadyPaid
	}
	remaining := row.bountyPoolUnits - row.bountyPaidUnits
	if remaining == 0 {
		return ErrFuzzEscrowDepleted
	}
	bonus := fuzzescrow.UniqueCrashBonusUnitsForSplit(row.bountyPoolUnits, row.escrowSplit)
	if bonus == 0 || bonus > remaining {
		return ErrFuzzEscrowDepleted
	}
	if err := s.creditUnits(ctx, tx, minerAddress, bonus); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`UPDATE fuzz_campaign_escrow SET crash_bonus_paid_units=? WHERE campaign_id=?`,
		bonus, campaignID)
	return err
}

func (s *Service) payFuzzBountyTx(ctx context.Context, tx *sql.Tx, campaignID, minerAddress, severity string) error {
	if !strings.HasPrefix(minerAddress, "HMC-") || len(minerAddress) != 20 {
		return errors.New("chain: valid miner_address required")
	}
	if severity != "high" && severity != "critical" && severity != "medium" {
		return errors.New("chain: bounty severity must be medium|high|critical")
	}
	row, err := s.lockFuzzEscrowTx(ctx, tx, campaignID)
	if err != nil {
		return err
	}
	if row.status != "open" {
		return ErrFuzzEscrowClosed
	}
	if row.bountyPaidUnits > 0 || row.findingWinner != "" {
		return ErrFuzzEscrowAlreadyPaid
	}
	remaining := row.bountyPoolUnits - row.crashBonusPaidUnits
	if remaining == 0 || row.bountyPoolUnits < row.crashBonusPaidUnits {
		return ErrFuzzEscrowDepleted
	}
	var minerUnits, feeUnits uint64
	var paidSlice uint64
	if row.escrowSplit == fuzzescrow.EscrowSplit5050 {
		var ok bool
		minerUnits, feeUnits, ok = fuzzescrow.HuntBountyPayoutUnits(remaining, severity)
		if !ok {
			return fmt.Errorf("chain: hunt bounty not payable for severity %q", severity)
		}
		paidSlice = minerUnits + feeUnits
	} else {
		minerUnits, feeUnits = fuzzescrow.BountyPayoutUnits(remaining)
		paidSlice = remaining
	}
	if err := s.creditUnits(ctx, tx, minerAddress, minerUnits); err != nil {
		return err
	}
	if feeUnits > 0 {
		if err := s.creditUnits(ctx, tx, DevFeeAddress, feeUnits); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx,
		`UPDATE fuzz_campaign_escrow SET bounty_paid_units=?, finding_winner=?, status='bounty_paid' WHERE campaign_id=?`,
		paidSlice, minerAddress, campaignID)
	return err
}

func (s *Service) finalizeFuzzEscrowTx(ctx context.Context, tx *sql.Tx, campaignID string) error {
	row, err := s.lockFuzzEscrowTx(ctx, tx, campaignID)
	if err != nil {
		return err
	}
	if row.status == "closed" {
		return nil
	}
	runsRefund := row.runsPoolUnits - row.runsPaidUnits
	bountyRefund := row.bountyPoolUnits - row.bountyPaidUnits - row.crashBonusPaidUnits
	if row.bountyPoolUnits < row.bountyPaidUnits+row.crashBonusPaidUnits {
		bountyRefund = 0
	}
	refund := runsRefund + bountyRefund
	if refund > 0 {
		var walletAddr string
		if err := tx.QueryRowContext(ctx, `SELECT address FROM wallet WHERE id=1`).Scan(&walletAddr); err != nil {
			return err
		}
		if err := s.creditUnits(ctx, tx, walletAddr, refund); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE fuzz_campaign_escrow SET status='closed', refunded_bounty_units=refunded_bounty_units+? WHERE campaign_id=?`,
		bountyRefund, campaignID); err != nil {
		return err
	}
	return s.checkEconomicInvariants(ctx, tx)
}
