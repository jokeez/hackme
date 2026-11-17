package chain

import (
	"context"
	"path/filepath"
	"testing"

	"hackme/internal/fuzzescrow"
	"hackme/internal/store"
)

// When primary wallet == DevFee under Economics v2, tenant open/cancel keep the
// wallet row stable while accounts track custody. Equality is not required
// (R20-03); strict dual-ledger skips this address pair.
func TestReviewRC20TenantKeepsPrimaryWalletStableUnderV2(t *testing.T) {
	for _, action := range []string{"open", "cancel", "rollback"} {
		t.Run(action, func(t *testing.T) {
			setV2HeightForTest(t, "1")
			t.Setenv("HACKME_ECON_STRICT_DUAL_LEDGER", "1")
			ctx := context.Background()
			db, err := store.Open(filepath.Join(t.TempDir(), "review.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			s := New(db)
			if _, _, err := s.InitGenesis(ctx, DevFeeAddress); err != nil {
				t.Fatal(err)
			}
			var walletBefore uint64
			if err := db.QueryRow(`SELECT balance_units FROM wallet WHERE id=1`).Scan(&walletBefore); err != nil {
				t.Fatal(err)
			}
			owner := "HMC-aabbccddeeff0011"
			if _, err := s.CreditOrdersFundForOwner(ctx, owner, "00112233445566778899aabbccddeeff", HMCToUnits(20)); err != nil {
				t.Fatal(err)
			}
			if _, err := s.OpenFuzzEscrowForOwner(ctx, "review-wallet", owner, 20, 64, fuzzescrow.EscrowSplit2080, FeeAssetHMC); err != nil {
				t.Fatal(err)
			}
			if action == "cancel" {
				if _, err := s.CancelFuzzEscrow(ctx, "review-wallet"); err != nil {
					t.Fatal(err)
				}
			}
			if action == "rollback" {
				if err := s.RollbackUnstartedFuzzEscrow(ctx, "review-wallet"); err != nil {
					t.Fatal(err)
				}
			}
			var walletAfter, account uint64
			if err := db.QueryRow(`SELECT balance_units FROM wallet WHERE id=1`).Scan(&walletAfter); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT balance_units FROM accounts WHERE address=?`, DevFeeAddress).Scan(&account); err != nil {
				t.Fatal(err)
			}
			if walletAfter != walletBefore {
				t.Errorf("%s: DevFee-primary wallet moved %d → %d (must stay stable)", action, walletBefore, walletAfter)
			}
			// Custody may diverge from wallet; ensure open still moved accounts.
			if action == "open" && account >= walletAfter {
				t.Errorf("%s: expected accounts custody below wallet after tenant open; wallet=%d accounts=%d", action, walletAfter, account)
			}
		})
	}
}

func TestReviewRC20ClosedEscrowHasNoRefundRemaining(t *testing.T) {
	setV2HeightForTest(t, "1")
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "review.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := New(db)
	if _, _, err := s.InitGenesis(ctx, "HMC-0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	owner := "HMC-aabbccddeeff0011"
	if _, err := s.CreditOrdersFundForOwner(ctx, owner, "11223344556677889900aabbccddeeff", HMCToUnits(20)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenHuntEscrowForOwner(ctx, "review-refund", owner, 20, 32); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ApplyFuzzSettleOnce(ctx, "review-run", "run_floor", "review-refund", "HMC-9988776655443322", ""); err != nil {
		t.Fatal(err)
	}
	closed, err := s.CancelFuzzEscrow(ctx, "review-refund")
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != "closed" {
		t.Fatalf("status=%s", closed.Status)
	}
	if closed.RefundableHMC != 0 {
		t.Errorf("closed escrow still advertises %.8f HMC refundable", closed.RefundableHMC)
	}
}
