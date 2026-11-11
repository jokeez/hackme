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
	// Scoped publish token cannot list/ACK the global outbox — drain per local escrow.
	if poolsync.OrdersPublishToken() != "" {
		return a.pullLocalFuzzSettleEscrows(ctx)
	}
	// Admin global pull: Soft-OK returns nil on a foreign-only fair window, which used
	// to skip this node's campaigns forever when ≥32 older foreign camps filled the
	// window (audit H5). Always follow with scoped drains of local open escrows.
	globalErr := a.pullFuzzSettleOutboxWindow(ctx, "", false)
	localErr := a.pullLocalFuzzSettleEscrows(ctx)
	if globalErr != nil {
		return globalErr
	}
	return localErr
}

func (a *app) pullLocalFuzzSettleEscrows(ctx context.Context) error {
	ids, err := a.localOpenFuzzEscrowCampaignIDs(ctx)
	if err != nil {
		return err
	}
	var firstErr error
	for _, cid := range ids {
		if err := a.pullFuzzSettleOutboxWindow(ctx, cid, true); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// pullFuzzSettleOutboxWindow drains coordinator outbox (global admin or one campaign).
// When scoped is true, campaignID is required and ACKs include campaign_id.
// Multi-pass catch-up until the outbox window is empty (or hard-stuck).
// Early "acked < 64 → break" used to return nil while unpaid rows remained,
// then cancel/finalize refunded the payer (report #23 residual).
func (a *app) pullFuzzSettleOutboxWindow(ctx context.Context, campaignID string, scoped bool) error {
	const maxPasses = 32
	totalAcked := 0
	for pass := 0; pass < maxPasses; pass++ {
		var (
			items []poolsync.SettleOutboxItem
			err   error
		)
		if scoped {
			items, err = poolsync.FetchSettleOutboxForCampaign(ctx, campaignID, 256)
		} else {
			items, err = poolsync.FetchSettleOutbox(ctx, 256)
		}
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
		receipts := make([]poolsync.SettleApplyReceipt, 0, len(items))
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
				if rc, ok := a.settleApplyReceipt(ctx, it, poolsync.SettleOutcomeDrainClose, nil); ok {
					receipts = append(receipts, rc)
				}
				continue
			}
			if !apply {
				// Closed escrow + unpaid run/finding — must not ACK (miner pay).
				undrainable++
				continue
			}
			if err := a.applyLocalFuzzSettleOnce(ctx, it); err != nil {
				if fuzzSettleOutboxDrainOnErr(err) {
					outcome := poolsync.SettleOutcomeDepleted
					if errors.Is(err, chain.ErrFuzzEscrowAlreadyPaid) {
						outcome = poolsync.SettleOutcomeAlreadyPaid
					}
					if rc, ok := a.settleApplyReceipt(ctx, it, outcome, nil); ok {
						receipts = append(receipts, rc)
					}
					continue
				}
				log.Printf("fuzz settle pull: campaign %s kind %s: %v", it.CampaignID, it.Kind, err)
				if kind != "finalize" && kind != "close" {
					payFailed[it.CampaignID] = true
				}
				continue
			}
			row, _ := a.chain.GetFuzzEscrow(ctx, it.CampaignID)
			if rc, ok := a.settleApplyReceipt(ctx, it, poolsync.SettleOutcomeApplied, row); ok {
				receipts = append(receipts, rc)
			}
		}
		if len(payFailed) > 0 && len(receipts) == 0 {
			return fmt.Errorf("fuzz settle pull: pay failures blocked drain (%d campaign(s))", len(payFailed))
		}
		if len(receipts) == 0 {
			if undrainable == len(items) {
				log.Printf("fuzz settle pull: %d undrainable outbox head row(s) (local_hits=%d) — not blocking other campaigns",
					len(items), localHits)
				// Foreign-only window (no local escrow on this node): soft-OK so
				// reopen/finalize ops are not wedged by another customer's findings.
				if localHits == 0 {
					return nil
				}
				return fmt.Errorf("%w (%d pending, local_hits=%d)", errSettleOutboxUndrainableHead, len(items), localHits)
			}
			if localHits == 0 {
				log.Printf("fuzz settle pull: %d outbox row(s) not local to this node (head blocked)", len(items))
			}
			return fmt.Errorf("fuzz settle pull: outbox not drained (%d pending, local_hits=%d)", len(items), localHits)
		}
		var ackErr error
		if scoped || poolsync.OrdersPublishToken() != "" {
			ackCamp := campaignID
			if ackCamp == "" && len(receipts) > 0 {
				ackCamp = receipts[0].CampaignID
			}
			ackErr = poolsync.AckSettleOutboxWithReceipts(ctx, ackCamp, receipts)
		} else {
			ids := make([]int64, 0, len(receipts))
			for _, rc := range receipts {
				ids = append(ids, rc.OutboxID)
			}
			ackErr = poolsync.AckSettleOutbox(ctx, ids)
		}
		if ackErr != nil {
			log.Printf("fuzz settle pull: ack: %v", ackErr)
			return ackErr
		}
		totalAcked += len(receipts)
	}
	return fmt.Errorf("fuzz settle pull: outbox still pending after %d passes (acked=%d)", maxPasses, totalAcked)
}

func (a *app) localOpenFuzzEscrowCampaignIDs(ctx context.Context) ([]string, error) {
	if a.db == nil {
		return nil, nil
	}
	rows, err := a.db.QueryContext(ctx,
		`SELECT campaign_id FROM fuzz_campaign_escrow
		 WHERE status IN ('open','bounty_paid')
		 ORDER BY campaign_id ASC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return out, err
		}
		id = strings.TrimSpace(id)
		if id != "" {
			out = append(out, id)
		}
	}
	return out, rows.Err()
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

// settleApplyReceipt builds an origin-verified ACK receipt after ApplyFuzzSettleOnce
// (or a terminal drain outcome). Requires the per-publish owner secret for MAC.
// units_hash is hub outbox-bound (PUB-07) — never a tenant-inventable escrow snapshot.
func (a *app) settleApplyReceipt(ctx context.Context, it poolsync.SettleOutboxItem, outcome string, row *chain.FuzzEscrowRow) (poolsync.SettleApplyReceipt, bool) {
	_ = row // escrow snapshot is local-only; ACK binds to outbox, not inventable units
	eventID := chain.FuzzSettleEventID(it.CampaignID, it.ID)
	rc := poolsync.SettleApplyReceipt{
		OutboxID:   it.ID,
		EventID:    eventID,
		CampaignID: it.CampaignID,
		Kind:       it.Kind,
		Outcome:    outcome,
		UnitsHash: poolsync.UnitsHashFromOutbox(
			eventID, it.CampaignID, it.Kind, it.MinerAddress, it.Severity, it.WorkItemID, outcome),
	}
	owner := ""
	secret := poolsync.LoadOrdersOwnerSecret(it.CampaignID)
	if a != nil && a.db != nil {
		_ = a.db.QueryRowContext(ctx,
			`SELECT COALESCE(owner_ref,'') FROM fuzz_campaigns WHERE id=?`, it.CampaignID).Scan(&owner)
		// SYNC-01: never fall back to plaintext orders_owner_secret in config_json.
	}
	pubTok := poolsync.OrdersPublishToken()
	if secret == "" {
		// Admin settle-pull: hub_applied path does not need receipts; publish path does.
		if pubTok != "" {
			return rc, false
		}
		return rc, true // caller uses hub_applied admin ACK without MAC
	}
	poolsync.SignSettleReceipt(pubTok, owner, secret, &rc)
	return rc, true
}

func (a *app) startFuzzSettlePullTicker() {
	if !poolSyncCoordinatorConfigured() {
		return
	}
	if !poolsync.FuzzSettlePullEnabled() {
		return
	}
	poolsync.SetSettleOwnerRefLookup(func(campaignID string) string {
		if a == nil || a.db == nil || strings.TrimSpace(campaignID) == "" {
			return ""
		}
		var owner string
		_ = a.db.QueryRow(
			`SELECT COALESCE(owner_ref,'') FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&owner)
		return strings.TrimSpace(owner)
	})
	poolsync.SetSettleOwnerSecretLookup(func(campaignID string) string {
		if strings.TrimSpace(campaignID) == "" {
			return ""
		}
		// SYNC-01: disk/env secret only — no config_json plaintext fallback.
		return poolsync.LoadOrdersOwnerSecret(campaignID)
	})
	go func() {
		interval := 15 * time.Second
		authBackoff := time.Duration(0)
		timer := time.NewTimer(interval)
		defer timer.Stop()
		for {
			<-timer.C
			ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
			err := a.pullFuzzSettleOutbox(ctx)
			cancel()
			next := interval
			if poolsync.SettleOutboxErrAuth(err) {
				// 401/403 are permanent for a given token/URL — exponential cooldown
				// so desktop Orders nodes do not look like abuse to rate limiters.
				if authBackoff == 0 {
					authBackoff = time.Minute
				} else if authBackoff < 15*time.Minute {
					authBackoff *= 2
				}
				if authBackoff > 15*time.Minute {
					authBackoff = 15 * time.Minute
				}
				next = authBackoff
				log.Printf("fuzz settle pull: auth error — backing off %s (%v)", next, err)
			} else {
				authBackoff = 0
			}
			timer.Reset(next)
		}
	}()
}
