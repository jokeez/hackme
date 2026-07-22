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

var (
	ErrFuzzEscrowNotFound      = errors.New("chain: fuzz escrow not found")
	ErrFuzzEscrowClosed        = errors.New("chain: fuzz escrow closed")
	ErrFuzzEscrowDepleted      = errors.New("chain: fuzz escrow pool depleted")
	ErrFuzzEscrowAlreadyPaid   = errors.New("chain: fuzz bounty already paid")
	ErrFuzzInsufficientBalance = errors.New("chain: insufficient wallet balance for fuzz escrow")
)

// FuzzEscrowRow is the locked 20/80 state for a campaign.
type FuzzEscrowRow struct {
	CampaignID        string  `json:"campaign_id"`
	BudgetHMC         float64 `json:"budget_hmc"`
	RunsPoolHMC       float64 `json:"runs_pool_hmc"`
	BountyPoolHMC     float64 `json:"bounty_pool_hmc"`
	RunsPaidHMC       float64 `json:"runs_paid_hmc"`
	BountyPaidHMC     float64 `json:"bounty_paid_hmc"`
	CrashBonusPaidHMC float64 `json:"crash_bonus_paid_hmc,omitempty"`
	RunsDone          int     `json:"runs_done"`
	BudgetRuns        int     `json:"budget_runs"`
	FindingWinner     string  `json:"finding_winner,omitempty"`
	Status            string  `json:"status"`
	RefundedBountyHMC float64 `json:"refunded_bounty_hmc,omitempty"`
	RefundedRunsHMC   float64 `json:"refunded_runs_hmc,omitempty"`

	// Live transparency fields for CLI / status (computed; safe to omit when zero only where noted).
	SpentRunsHMC     float64 `json:"spent_runs_hmc"`
	LockedBountyHMC  float64 `json:"locked_bounty_hmc"`
	RunsRemainingHMC float64 `json:"runs_remaining_hmc"`
	RefundableHMC    float64 `json:"refundable_hmc"`
	RefundPath       string  `json:"refund_path"`
}

// OpenFuzzEscrow locks budget from the primary wallet into a 20/80 split.
func (s *Service) OpenFuzzEscrow(ctx context.Context, campaignID string, budgetHMC float64, budgetRuns int) (*FuzzEscrowRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return nil, errors.New("chain: campaign_id required")
	}
	if budgetHMC < fuzzescrow.MinCampaignBudgetHMC {
		return nil, fmt.Errorf("fuzz escrow: budget below minimum %.2f HMC", fuzzescrow.MinCampaignBudgetHMC)
	}
	if budgetHMC > fuzzescrow.MaxCampaignBudgetHMC {
		return nil, fmt.Errorf("fuzz escrow: budget above maximum %.0f HMC", fuzzescrow.MaxCampaignBudgetHMC)
	}
	total := HMCToUnits(budgetHMC)
	if total == 0 {
		return nil, errors.New("fuzz escrow: budget rounds to zero units")
	}
	split, err := fuzzescrow.ComputeSplitUnits(total, budgetRuns)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM fuzz_campaign_escrow WHERE campaign_id=?`, campaignID).Scan(&exists); err == nil {
		return nil, fmt.Errorf("chain: fuzz escrow already exists for %s", campaignID)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var balUnits uint64
	var walletAddr string
	if err := tx.QueryRowContext(ctx, `SELECT address, balance_units FROM wallet WHERE id=1`).Scan(&walletAddr, &balUnits); err != nil {
		return nil, err
	}
	if balUnits < split.TotalUnits {
		return nil, ErrFuzzInsufficientBalance
	}
	var accountUnits uint64
	if err := tx.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address = ?`, walletAddr).Scan(&accountUnits); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrFuzzInsufficientBalance
		}
		return nil, err
	}
	if accountUnits < split.TotalUnits {
		return nil, ErrFuzzInsufficientBalance
	}
	// Dual-ledger: wallet + accounts must both debit (same invariant as order escrow).
	if _, err := tx.ExecContext(ctx,
		`UPDATE wallet SET balance_units = balance_units - ?, balance_hmc = (balance_units - ?) / ? WHERE id=1`,
		split.TotalUnits, split.TotalUnits, float64(UnitsPerHMC)); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE accounts SET balance_units = balance_units - ?, updated_at = strftime('%s','now') WHERE address = ?`,
		split.TotalUnits, walletAddr); err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO fuzz_campaign_escrow
		 (campaign_id, budget_units, runs_pool_units, bounty_pool_units, runs_paid_units, bounty_paid_units,
		  runs_done, budget_runs, per_run_units, finding_winner, status, created_at)
		 VALUES (?, ?, ?, ?, 0, 0, 0, ?, ?, '', 'open', ?)`,
		campaignID, split.TotalUnits, split.RunsPoolUnits, split.BountyPoolUnits,
		budgetRuns, split.PerRunUnits, now); err != nil {
		return nil, err
	}
	if err := s.checkEconomicInvariants(ctx, tx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetFuzzEscrow(ctx, campaignID)
}

func (s *Service) creditUnits(ctx context.Context, tx *sql.Tx, addr string, units uint64) error {
	addr = strings.TrimSpace(addr)
	if addr == "" || units == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, ?, 0, strftime('%s','now'))
		 ON CONFLICT(address) DO UPDATE SET balance_units=accounts.balance_units + excluded.balance_units, updated_at=excluded.updated_at`,
		addr, units); err != nil {
		return err
	}
	var walletAddr string
	if err := tx.QueryRowContext(ctx, `SELECT address FROM wallet WHERE id=1`).Scan(&walletAddr); err != nil {
		return err
	}
	if addr == strings.TrimSpace(walletAddr) {
		_, werr := tx.ExecContext(ctx,
			`UPDATE wallet SET balance_units = balance_units + ?, balance_hmc = (balance_units + ?) / ? WHERE id=1`,
			units, units, float64(UnitsPerHMC))
		return werr
	}
	return nil
}

// PayFuzzRun pays one run slice from the 20% pool to miner_address.
func (s *Service) PayFuzzRun(ctx context.Context, campaignID, minerAddress string) (*FuzzEscrowRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaignID = strings.TrimSpace(campaignID)
	minerAddress = strings.TrimSpace(minerAddress)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.payFuzzRunTx(ctx, tx, campaignID, minerAddress); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.getFuzzEscrowUnlocked(ctx, campaignID)
}

// PayFuzzBounty pays the 80% pool to the first qualifying finder (minus 5% platform fee).
func (s *Service) PayFuzzBounty(ctx context.Context, campaignID, minerAddress, severity string) (*FuzzEscrowRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaignID = strings.TrimSpace(campaignID)
	minerAddress = strings.TrimSpace(minerAddress)
	severity = strings.TrimSpace(strings.ToLower(severity))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.payFuzzBountyTx(ctx, tx, campaignID, minerAddress, severity); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.getFuzzEscrowUnlocked(ctx, campaignID)
}

// PayFuzzCrashBonus pays a one-shot unique-crash micro-bonus from the bounty pool.
func (s *Service) PayFuzzCrashBonus(ctx context.Context, campaignID, minerAddress string) (*FuzzEscrowRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaignID = strings.TrimSpace(campaignID)
	minerAddress = strings.TrimSpace(minerAddress)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.payFuzzCrashBonusTx(ctx, tx, campaignID, minerAddress); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.getFuzzEscrowUnlocked(ctx, campaignID)
}

// CancelFuzzEscrow refunds all unpaid escrow slices to the operator wallet and closes.
func (s *Service) CancelFuzzEscrow(ctx context.Context, campaignID string) (*FuzzEscrowRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaignID = strings.TrimSpace(campaignID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := s.lockFuzzEscrowTx(ctx, tx, campaignID)
	if err != nil {
		return nil, err
	}
	if row.status == "closed" {
		return s.GetFuzzEscrow(ctx, campaignID)
	}
	runsRefund := row.runsPoolUnits - row.runsPaidUnits
	bountyRefund := row.bountyPoolUnits - row.bountyPaidUnits - row.crashBonusPaidUnits
	if row.status == "bounty_paid" {
		bountyRefund = 0
	}
	if row.bountyPoolUnits < row.bountyPaidUnits+row.crashBonusPaidUnits {
		bountyRefund = 0
	}
	refund := runsRefund + bountyRefund
	if refund > 0 {
		var walletAddr string
		if err := tx.QueryRowContext(ctx, `SELECT address FROM wallet WHERE id=1`).Scan(&walletAddr); err != nil {
			return nil, err
		}
		if err := s.creditUnits(ctx, tx, walletAddr, refund); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE fuzz_campaign_escrow SET status='closed', refunded_bounty_units=refunded_bounty_units+? WHERE campaign_id=?`,
		bountyRefund, campaignID); err != nil {
		return nil, err
	}
	if err := s.checkEconomicInvariants(ctx, tx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	out, err := s.GetFuzzEscrow(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	out.RefundedBountyHMC = UnitsToHMC(bountyRefund)
	out.RefundedRunsHMC = UnitsToHMC(runsRefund)
	return out, nil
}

// FinalizeFuzzEscrow refunds unused run-pool + bounty to the primary wallet and closes escrow.
// Previously only bounty was refunded, which stranded unpaid runs_pool funds after time/budget complete.
func (s *Service) FinalizeFuzzEscrow(ctx context.Context, campaignID string) (*FuzzEscrowRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaignID = strings.TrimSpace(campaignID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := s.lockFuzzEscrowTx(ctx, tx, campaignID)
	if err != nil {
		return nil, err
	}
	if row.status == "closed" {
		return s.getFuzzEscrowUnlocked(ctx, campaignID)
	}
	runsRefund := row.runsPoolUnits - row.runsPaidUnits
	bountyRefund := row.bountyPoolUnits - row.bountyPaidUnits - row.crashBonusPaidUnits
	if row.status == "bounty_paid" {
		bountyRefund = 0
	}
	if row.bountyPoolUnits < row.bountyPaidUnits+row.crashBonusPaidUnits {
		bountyRefund = 0
	}
	if err := s.finalizeFuzzEscrowTx(ctx, tx, campaignID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	out, err := s.getFuzzEscrowUnlocked(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	out.RefundedBountyHMC = UnitsToHMC(bountyRefund)
	out.RefundedRunsHMC = UnitsToHMC(runsRefund)
	return out, nil
}

type fuzzEscrowDBRow struct {
	campaignID          string
	budgetUnits         uint64
	runsPoolUnits       uint64
	bountyPoolUnits     uint64
	runsPaidUnits       uint64
	bountyPaidUnits     uint64
	crashBonusPaidUnits uint64
	runsDone            int
	budgetRuns          int
	perRunUnits         uint64
	findingWinner       string
	status              string
	refundedBountyUnits uint64
}

func (s *Service) lockFuzzEscrowTx(ctx context.Context, tx *sql.Tx, campaignID string) (*fuzzEscrowDBRow, error) {
	return s.readFuzzEscrow(ctx, tx, campaignID)
}

func (s *Service) readFuzzEscrow(ctx context.Context, q queryRowContext, campaignID string) (*fuzzEscrowDBRow, error) {
	var r fuzzEscrowDBRow
	err := q.QueryRowContext(ctx,
		`SELECT campaign_id, budget_units, runs_pool_units, bounty_pool_units, runs_paid_units, bounty_paid_units,
		        COALESCE(crash_bonus_paid_units,0), runs_done, budget_runs, per_run_units, finding_winner, status, refunded_bounty_units
		 FROM fuzz_campaign_escrow WHERE campaign_id=?`, campaignID).
		Scan(&r.campaignID, &r.budgetUnits, &r.runsPoolUnits, &r.bountyPoolUnits, &r.runsPaidUnits, &r.bountyPaidUnits,
			&r.crashBonusPaidUnits, &r.runsDone, &r.budgetRuns, &r.perRunUnits, &r.findingWinner, &r.status, &r.refundedBountyUnits)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFuzzEscrowNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func fuzzEscrowPublic(r *fuzzEscrowDBRow) *FuzzEscrowRow {
	if r == nil {
		return nil
	}
	runsPaid := UnitsToHMC(r.runsPaidUnits)
	bountyPaid := UnitsToHMC(r.bountyPaidUnits)
	crashBonus := UnitsToHMC(r.crashBonusPaidUnits)
	runsRemainingU := uint64(0)
	if r.runsPoolUnits > r.runsPaidUnits {
		runsRemainingU = r.runsPoolUnits - r.runsPaidUnits
	}
	lockedBountyU := uint64(0)
	spentBountyU := r.bountyPaidUnits + r.crashBonusPaidUnits
	if r.bountyPoolUnits > spentBountyU && r.status != "closed" {
		lockedBountyU = r.bountyPoolUnits - spentBountyU
	}
	if r.status == "bounty_paid" {
		// Main bounty released; crash bonus already paid — nothing left locked.
		lockedBountyU = 0
	}
	refundableU := runsRemainingU
	if r.status == "open" {
		refundableU += lockedBountyU
	}
	// bounty_paid: only unused runs refund on finalize/cancel.
	path := "none"
	switch r.status {
	case "open":
		path = "finalize_or_cancel_refunds_unused_runs_and_locked_bounty"
	case "bounty_paid":
		path = "finalize_or_cancel_refunds_unused_runs_only"
	case "closed":
		path = "already_closed"
	}
	return &FuzzEscrowRow{
		CampaignID:        r.campaignID,
		BudgetHMC:         UnitsToHMC(r.budgetUnits),
		RunsPoolHMC:       UnitsToHMC(r.runsPoolUnits),
		BountyPoolHMC:     UnitsToHMC(r.bountyPoolUnits),
		RunsPaidHMC:       runsPaid,
		BountyPaidHMC:     bountyPaid,
		CrashBonusPaidHMC: crashBonus,
		RunsDone:          r.runsDone,
		BudgetRuns:        r.budgetRuns,
		FindingWinner:     r.findingWinner,
		Status:            r.status,
		RefundedBountyHMC: UnitsToHMC(r.refundedBountyUnits),
		SpentRunsHMC:      runsPaid,
		LockedBountyHMC:   UnitsToHMC(lockedBountyU),
		RunsRemainingHMC:  UnitsToHMC(runsRemainingU),
		RefundableHMC:     UnitsToHMC(refundableU),
		RefundPath:        path,
	}
}

// GetFuzzEscrow returns public escrow state for a campaign.
func (s *Service) GetFuzzEscrow(ctx context.Context, campaignID string) (*FuzzEscrowRow, error) {
	r, err := s.readFuzzEscrow(ctx, s.db, campaignID)
	if err != nil {
		return nil, err
	}
	return fuzzEscrowPublic(r), nil
}
