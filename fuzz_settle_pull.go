package main

import (
	"context"
	"errors"
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
	items, err := poolsync.FetchSettleOutbox(ctx, 64)
	if err != nil {
		log.Printf("fuzz settle pull: fetch outbox: %v", err)
		return
	}
	if len(items) == 0 {
		return
	}
	acked := make([]int64, 0, len(items))
	for _, it := range items {
		st, local := a.localFuzzEscrowStatus(ctx, it.CampaignID)
		if !local {
			continue
		}
		apply, drain := fuzzSettleOutboxAction(st, it.Kind)
		if drain {
			acked = append(acked, it.ID)
			continue
		}
		if !apply {
			continue
		}
		if err := a.applyLocalFuzzSettle(ctx, it); err != nil {
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
		return
	}
	if err := poolsync.AckSettleOutbox(ctx, acked); err != nil {
		log.Printf("fuzz settle pull: ack: %v", err)
		return
	}
	log.Printf("fuzz settle pull: applied %d outbox row(s)", len(acked))
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
		return false, true
	case "open":
		return true, false
	case "bounty_paid":
		switch kind {
		case "run", "finding", "bounty":
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

func (a *app) applyLocalFuzzSettle(ctx context.Context, it poolsync.SettleOutboxItem) error {
	switch strings.TrimSpace(strings.ToLower(it.Kind)) {
	case "run":
		_, err := a.chain.PayFuzzRun(ctx, it.CampaignID, it.MinerAddress)
		return err
	case "finding", "bounty":
		_, err := a.chain.PayFuzzBounty(ctx, it.CampaignID, it.MinerAddress, it.Severity)
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
