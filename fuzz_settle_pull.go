package main

import (
	"context"
	"log"
	"strings"
	"time"

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
		if !a.localFuzzEscrowOpen(ctx, it.CampaignID) {
			continue
		}
		if err := a.applyLocalFuzzSettle(ctx, it); err != nil {
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

func (a *app) localFuzzEscrowOpen(ctx context.Context, campaignID string) bool {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return false
	}
	row, err := a.chain.GetFuzzEscrow(ctx, campaignID)
	if err != nil || row == nil {
		return false
	}
	st := strings.TrimSpace(strings.ToLower(row.Status))
	return st == "open" || st == "bounty_paid"
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
