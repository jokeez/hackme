package chain

import (
	"context"
	"testing"
)

// Audit 2026-11-05 (HMS/SUP M1): a tx rejected at apply must not re-enter the pool — re-applying
// it failed the history insert on UNIQUE(tx_hash) and aborted every later block (chain halt).
func TestRejectedTxCannotBeResubmitted(t *testing.T) {
	ctx := context.Background()
	svc, addrA, privA := hmsSettleWallet(t)
	tx := hmsSettleTx(t, svc, addrA, "HMC-cccccccccccccccc", HMSToUnits(4.0), 0, privA)
	if _, st, err := svc.SubmitHmsTransferTx(ctx, tx); err != nil || st != "pending" {
		t.Fatalf("submit: %q %v", st, err)
	}
	if code, err := svc.BurnHMSForService(ctx, addrA, HMSToUnits(2.0), "drain"); err != nil || code != "" {
		t.Fatalf("burn: %q %v", code, err)
	}
	hmsSettleAppendBlock(t, svc) // rejected: insufficient balance
	if code, err := svc.MintHMS(ctx, addrA, HMSToUnits(5.0), "refill"); err != nil || code != "" {
		t.Fatalf("refill: %q %v", code, err)
	}
	if _, code, err := svc.SubmitHmsTransferTx(ctx, tx); err == nil || code != "duplicate_or_replay" {
		t.Fatalf("resubmit of a rejected tx: code=%q err=%v", code, err)
	}
	hmsSettleAppendBlock(t, svc) // block production continues
}
