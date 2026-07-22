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

func (a *app) pullFuzzSettleOutbox(ctx context.Context) {
	if a.chain == nil || !poolSyncCoordinatorConfigured() {
		return
	}
	// Multi-pass catch-up: a few foreign/stale head rows used to pin the oldest-first
	// outbox window so local campaigns never drained (seen with leftover b2b-* rows).
	totalAcked := 0
	for pass := 0; pass < 6; pass++ {
		items, err := poolsync.FetchSettleOutbox(ctx, 256)
		if err != nil {
			log.Printf("fuzz settle pull: fetch outbox: %v", err)
			return
		}
		if len(items) == 0 {
			break
		}
		acked := make([]int64, 0, len(items))
		localHits := 0
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
		for _, it := range ordered {
			st, local := a.localFuzzEscrowStatus(ctx, it.CampaignID)
			if !local {
				continue
			}
			localHits++
			apply, drain := fuzzSettleOutboxAction(st, it.Kind)
			if drain {
				acked = append(acked, it.ID)
				continue
			}
			if !apply {
				continue
			}
			if err := a.applyLocalFuzzSettleOnce(ctx, it); err != nil {
				if fuzzSettleOutboxDrainOnErr(err) {
					acked = append(acked, it.ID)
					continue
				}
				log.Printf("fuzz settle pull: campaign %s kind %s: %v", it.CampaignID, it.Kind, err)
				continue
			}
			acked = append(acked, it.ID)
		}
		if len(acked) == 0 {
			// Head of queue is all foreign to this node — stop spinning this tick.
			if localHits == 0 {
				log.Printf("fuzz settle pull: %d outbox row(s) not local to this node (head blocked)", len(items))
			}
			break
		}
		if err := poolsync.AckSettleOutbox(ctx, acked); err != nil {
			log.Printf("fuzz settle pull: ack: %v", err)
			return
		}
		totalAcked += len(acked)
		if len(acked) < 64 {
			break
		}
	}
	if totalAcked > 0 {
		log.Printf("fuzz settle pull: applied %d outbox row(s)", totalAcked)
	}
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
			// Do NOT ACK unpaid run/finding/crash_bonus after a premature finalize:
			// that permanently burns miner payouts. Leave pending for ops replay.
			return false, false
		}
	case "open":
		return true, false
	case "bounty_paid":
		switch kind {
		case "run", "crash_bonus", "unique_crash":
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
	return errors.Is(err, chain.ErrFuzzEscrowClosed) ||
		errors.Is(err, chain.ErrFuzzEscrowDepleted) ||
		errors.Is(err, chain.ErrFuzzEscrowAlreadyPaid)
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

func (a *app) applyLocalFuzzSettle(ctx context.Context, it poolsync.SettleOutboxItem) error {
	switch strings.TrimSpace(strings.ToLower(it.Kind)) {
	case "run":
		_, err := a.chain.PayFuzzRun(ctx, it.CampaignID, it.MinerAddress)
		return err
	case "finding", "bounty":
		_, err := a.chain.PayFuzzBounty(ctx, it.CampaignID, it.MinerAddress, it.Severity)
		return err
	case "crash_bonus", "unique_crash":
		_, err := a.chain.PayFuzzCrashBonus(ctx, it.CampaignID, it.MinerAddress)
		return err
	case "finalize", "close":
		_, err := a.chain.FinalizeFuzzEscrow(ctx, it.CampaignID)
		return err
	default:
		return nil
	}
}

func (a *app) startFuzzSettlePullTicker() {
	if !poolSyncCoordinatorConfigured() {
		return
	}
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			a.pullFuzzSettleOutbox(ctx)
			cancel()
		}
	}()
}
