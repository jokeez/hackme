package chain

import (
	"context"
	"database/sql"
	"errors"
)

// OrdersEscrowLockedUnits returns HMC units locked and not yet refundable to spendable:
// open Dig/Hunt campaign remaining pools + PoH order escrow reserve.
func (s *Service) OrdersEscrowLockedUnits(ctx context.Context) (uint64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	var fuzzLocked sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(
  CASE WHEN runs_pool_units > runs_paid_units THEN runs_pool_units - runs_paid_units ELSE 0 END
  + CASE
      WHEN bounty_pool_units > (bounty_paid_units + COALESCE(crash_bonus_paid_units, 0))
        THEN bounty_pool_units - (bounty_paid_units + COALESCE(crash_bonus_paid_units, 0))
      ELSE 0
    END
), 0)
FROM fuzz_campaign_escrow
WHERE status IN ('open', 'bounty_paid')`).Scan(&fuzzLocked)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	var fuzzU uint64
	if fuzzLocked.Valid && fuzzLocked.Int64 > 0 {
		fuzzU = uint64(fuzzLocked.Int64)
	}
	pohU, err := s.metaUint(ctx, s.db, metaOrderEscrowUnits, 0)
	if err != nil {
		return 0, err
	}
	sum := fuzzU + pohU
	if sum < fuzzU { // overflow guard
		return ^uint64(0), nil
	}
	return sum, nil
}

// OrdersEscrowLockedHMC is OrdersEscrowLockedUnits as HMC float.
func (s *Service) OrdersEscrowLockedHMC(ctx context.Context) (float64, uint64, error) {
	u, err := s.OrdersEscrowLockedUnits(ctx)
	if err != nil {
		return 0, 0, err
	}
	return UnitsToHMC(u), u, nil
}
