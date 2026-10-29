package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"hackme/internal/chain"
	"hackme/internal/poolsync"
)

// errSettleOutboxUndrainableHead means the coordinator outbox head is all
// non-local or permanently stuck (closed escrow + unpaid run/finding). Other
// campaigns may still finalize; ops must recover the stuck campaign separately.
var errSettleOutboxUndrainableHead = errors.New("fuzz settle pull: undrainable outbox head")

func (a *app) pullFuzzSettleOutbox(ctx context.Context) error {
	if a.chain == nil || !poolSyncCoordinatorConfigured() {
		return nil
	}
	// Multi-pass catch-up until the outbox window is empty (or hard-stuck).
	// Early "acked < 64 → break" used to return nil while unpaid rows remained,
	// then cancel/finalize refunded the payer (report #23 residual).
	const maxPasses = 32
	totalAcked := 0
	for pass := 0; pass < maxPasses; pass++ {
		items, err := poolsync.FetchSettleOutbox(ctx, 256)
		if err != nil {
			log.Printf("fuzz settle pull: fetch outbox: %v", err)
			return err
		}
		if len(items) == 0 {
			if totalAcked > 0 {
				log.Printf("fuzz settle pull: applied %d outbox row(s)", totalAcked)
			}
			return nil
		}
		acked := make([]int64, 0, len(items))
		localHits := 0
		undrainable := 0
		// Prefer run/crash_bonus/finding before finalize so refund cannot race ahead of payouts.
		ordered := make([]poolsync.SettleOutboxItem, 0, len(items))
		var finals []poolsync.SettleOutboxItem
		for _, it := range items {
			k := strings.TrimSpace(strings.ToLower(it.Kind))
			if k == "finalize" || k == "close" {
				finals = append(finals, it)
			} else {
				ordered = append(ordered, it)
			}
		}
		ordered = append(ordered, finals...)
		payFailed := map[string]bool{}
		for _, it := range ordered {
			st, local := a.localFuzzEscrowStatus(ctx, it.CampaignID)
			if !local {
				undrainable++
				continue
			}
			localHits++
			kind := strings.TrimSpace(strings.ToLower(it.Kind))
			// Do not finalize/close a campaign in the same pass after a non-drainable
			// pay failure — that refunds while unpaid rows stay pending forever.
			if fuzzSettleSkipFinalizeAfterPayFail(kind, it.CampaignID, payFailed) {
				log.Printf("fuzz settle pull: skip %s for %s after pay failure in batch", kind, it.CampaignID)
				continue
			}
			apply, drain := fuzzSettleOutboxAction(st, it.Kind)
			if drain {
				acked = append(acked, it.ID)
				continue
			}
			if !apply {
				// Closed escrow + unpaid run/finding — must not ACK (miner pay).
				undrainable++
				continue
			}
			if err := a.applyLocalFuzzSettleOnce(ctx, it); err != nil {
				if fuzzSettleOutboxDrainOnErr(err) {
					acked = append(acked, it.ID)
					continue
				}
				log.Printf("fuzz settle pull: campaign %s kind %s: %v", it.CampaignID, it.Kind, err)
				if kind != "finalize" && kind != "close" {
					payFailed[it.CampaignID] = true
				}
				continue
			}
			acked = append(acked, it.ID)
		}
		if len(payFailed) > 0 && len(acked) == 0 {
			return fmt.Errorf("fuzz settle pull: pay failures blocked drain (%d campaign(s))", len(payFailed))
		}
		if len(acked) == 0 {
			if undrainable == len(items) {
				log.Printf("fuzz settle pull: %d undrainable outbox head row(s) (local_hits=%d) — not blocking other campaigns",
					len(items), localHits)
				return fmt.Errorf("%w (%d pending, local_hits=%d)", errSettleOutboxUndrainableHead, len(items), localHits)
			}
			if localHits == 0 {
				log.Printf("fuzz settle pull: %d outbox row(s) not local to this node (head blocked)", len(items))
			}
			return fmt.Errorf("fuzz settle pull: outbox not drained (%d pending, local_hits=%d)", len(items), localHits)
		}
		if err := poolsync.AckSettleOutbox(ctx, acked); err != nil {
			log.Printf("fuzz settle pull: ack: %v", err)
			return err
		}
		totalAcked += len(acked)
	}
	return fmt.Errorf("fuzz settle pull: outbox still pending after %d passes (acked=%d)", maxPasses, totalAcked)
}

func (a *app) localFuzzEscrowStatus(ctx context.Context, campaignID string) (status string, ok bool) {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return "", false
	}
	row, err := a.chain.GetFuzzEscrow(ctx, campaignID)
	if err != nil || row == nil {
		return "", false
	}
	return strings.TrimSpace(strings.ToLower(row.Status)), true
}

func (a *app) localFuzzEscrowOpen(ctx context.Context, campaignID string) bool {
	st, ok := a.localFuzzEscrowStatus(ctx, campaignID)
	return ok && (st == "open" || st == "bounty_paid")
}

// fuzzSettleOutboxAction decides whether to apply a coordinator outbox row locally
// or ack it as stale (origin node escrow already moved past this settlement).
func fuzzSettleOutboxAction(escrowStatus, kind string) (apply bool, drain bool) {
	kind = strings.TrimSpace(strings.ToLower(kind))
	switch escrowStatus {
	case "closed":
		switch kind {
		case "finalize", "close":
			// Already closed — safe to ACK.
			return false, true
		default:
			// Do NOT ACK unpaid run/run_floor/finding/crash_bonus after a premature finalize:
			// that permanently burns miner payouts. Leave pending for ops replay.
			return false, false
		}
	case "open":
		return true, false
	case "bounty_paid":
		switch kind {
		case "run", "run_floor", "crash_bonus", "unique_crash":
			return true, false
		case "finding", "bounty":
			return false, true
		case "finalize", "close":
			return true, false
		}
		return false, true
	default:
		return false, false
	}
}

func fuzzSettleOutboxDrainOnErr(err error) bool {
	// Do NOT drain ErrFuzzEscrowClosed: that permanently ACKs unpaid run/crash rows
	// (C-01). Depleted / already-paid are terminal no-ops safe to ACK.
	return errors.Is(err, chain.ErrFuzzEscrowDepleted) ||
		errors.Is(err, chain.ErrFuzzEscrowAlreadyPaid)
}

// fuzzSettleSkipFinalizeAfterPayFail blocks same-batch refund after a stuck payout.
func fuzzSettleSkipFinalizeAfterPayFail(kind, campaignID string, payFailed map[string]bool) bool {
	kind = strings.TrimSpace(strings.ToLower(kind))
	if kind != "finalize" && kind != "close" {
		return false
	}
	return payFailed[strings.TrimSpace(campaignID)]
}

// applyLocalFuzzSettleOnce credits at most once per stable outbox event ID.
// Pay + applied-event share one chain transaction (no crash underpay / no double-pay).
func (a *app) applyLocalFuzzSettleOnce(ctx context.Context, it poolsync.SettleOutboxItem) error {
	if a == nil || a.chain == nil {
		return fmt.Errorf("fuzz settle: no chain")
	}
	if it.ID <= 0 {
		return fmt.Errorf("fuzz settle: missing outbox event id")
	}
	eventID := chain.FuzzSettleEventID(it.CampaignID, it.ID)
	_, _, err := a.chain.ApplyFuzzSettleOnce(ctx, eventID, it.Kind, it.CampaignID, it.MinerAddress, it.Severity)
	return err
}

func (a *app) startFuzzSettlePullTicker() {
	if !poolSyncCoordinatorConfigured() {
		return
	}
	if !poolsync.FuzzSettlePullEnabled() {
		return
	}
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
			_ = a.pullFuzzSettleOutbox(ctx)
			cancel()
		}
	}()
}
