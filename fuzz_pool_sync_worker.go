package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"hackme/internal/chain"
	"hackme/internal/fuzzengine"
	"hackme/internal/logsafe"
	"hackme/internal/poolfuzz"
	"hackme/internal/poolsync"
)

type poolSyncJob struct {
	campaign fuzzAutoCampaign
	title    string
	desc     string
	ctype    string
}

func (a *app) poolSyncStatusPayload() map[string]any {
	snap := poolsync.Snapshot()
	failed := a.poolSyncFailedIDs()
	pending := int64(0)
	if a.poolSyncCh != nil {
		pending = int64(len(a.poolSyncCh))
	}
	snap.PendingCount = pending
	return map[string]any{
		"metrics":                  snap,
		"failed_campaigns":         failed,
		"coordinator_url_resolved": poolsync.ResolveCoordinatorURL(),
		"async_enabled":            poolsync.AsyncEnabled(),
	}
}

func (a *app) startPoolSyncWorker() {
	a.poolSyncOnce.Do(func() {
		a.poolSyncCh = make(chan poolSyncJob, 256)
		go a.poolSyncWorkerLoop()
		a.startFuzzSettlePullTicker()
		go a.retryFailedPoolSyncCampaigns()
		go a.reconcilePoolSyncCampaignsLoop()
		go a.mirrorPoolProgressLoop()
	})
}

// mirrorPoolProgressLoop refreshes local summary from coordinator progress on a short
// interval so Orders pulse/report stay honest while settle-pull is delayed (429/backoff).
// Register/reconcile stays on the slower loop; this path only calls progress sync.
func (a *app) mirrorPoolProgressLoop() {
	time.Sleep(8 * time.Second)
	a.mirrorPoolProgressOnce()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		a.mirrorPoolProgressOnce()
	}
}

func (a *app) mirrorPoolProgressOnce() {
	if a == nil || a.db == nil {
		return
	}
	if strings.TrimSpace(poolsync.ResolveCoordinatorURL()) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	rows, err := a.db.QueryContext(ctx,
		`SELECT id FROM fuzz_campaigns
		 WHERE status IN ('planned','running')
		   AND json_extract(config_json, '$.pool_distributed') IN (1, 'true', '1')
		 ORDER BY created_at DESC LIMIT 32`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		syncCtx, syncCancel := context.WithTimeout(ctx, 8*time.Second)
		if err := a.syncPoolCampaignProgressFromCoordinator(syncCtx, id); err != nil {
			log.Printf("pool progress mirror: %s: %v", logsafe.ID(id), err)
		}
		syncCancel()
	}
}

// reconcilePoolSyncCampaignsLoop re-queues pool campaigns missing on the coordinator.
func (a *app) reconcilePoolSyncCampaignsLoop() {
	time.Sleep(5 * time.Second)
	a.reconcilePoolSyncCampaigns()
	ticker := time.NewTicker(3 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		a.reconcilePoolSyncCampaigns()
	}
}

func (a *app) reconcilePoolSyncCampaigns() {
	if a == nil || a.db == nil {
		return
	}
	if strings.TrimSpace(poolsync.ResolveCoordinatorURL()) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	rows, err := a.db.QueryContext(ctx,
		`SELECT id, COALESCE(owner_ref,''), budget_runs, budget_seconds, config_json FROM fuzz_campaigns
		 WHERE status IN ('planned','running')
		   AND json_extract(config_json, '$.pool_distributed') IN (1, 'true', '1')
		 ORDER BY created_at DESC LIMIT 64`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, ownerRef, cfgJSON string
		var budgetRuns, budgetSec int
		if err := rows.Scan(&id, &ownerRef, &budgetRuns, &budgetSec, &cfgJSON); err != nil {
			continue
		}
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		progCtx, progCancel := context.WithTimeout(ctx, 6*time.Second)
		_, ok := a.fetchCoordinatorPoolCampaignProgress(progCtx, id)
		progCancel()
		if ok {
			a.poolSyncMarkOK(id)
			a.persistPoolRegisterStatus(ctx, id, "ok", "")
			syncCtx, syncCancel := context.WithTimeout(ctx, 20*time.Second)
			if err := a.syncPoolCampaignProgressFromCoordinator(syncCtx, id); err != nil {
				log.Printf("pool sync reconcile: %s progress: %v", logsafe.ID(id), err)
			}
			syncCancel()
			continue
		}
		// A paid pool order's mirror (customer node) is registered by the hub, never from here:
		// if progress is not readable yet, just wait for the next tick.
		if poolEscrowMirrorCampaign(parseMapJSON(cfgJSON)) {
			continue
		}
		// Retry missing campaigns — including previously failed (hub cap may rise), but
		// back off per campaign: a permanently stuck one (missing harness artifact,
		// hub-only prepaid ticket) otherwise re-clones and re-registers every tick
		// forever, burning coordinator rate budget that live orders need.
		if !a.poolSyncRetryDue(id) {
			continue
		}
		a.poolSyncMu.Lock()
		delete(a.poolSyncQueued, id)
		a.poolSyncMu.Unlock()
		mode, warn := a.schedulePoolFuzzSync(ctx, fuzzAutoCampaign{
			ID: id, OwnerRef: ownerRef, BudgetRuns: budgetRuns, BudgetSeconds: budgetSec, ConfigJSON: cfgJSON,
		})
		if warn != "" {
			log.Printf("pool sync reconcile: %s warn=%s", logsafe.ID(id), logsafe.ID(warn))
		} else {
			log.Printf("pool sync reconcile: %s mode=%s (retry_missing)", logsafe.ID(id), logsafe.ID(mode))
		}
	}
}

// retryFailedPoolSyncCampaigns re-queues pool campaigns that failed coordinator register.
func (a *app) retryFailedPoolSyncCampaigns() {
	time.Sleep(3 * time.Second)
	failed := a.poolSyncFailedIDs()
	if len(failed) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rows, err := a.db.QueryContext(ctx,
		`SELECT id, COALESCE(owner_ref,''), budget_runs, budget_seconds, config_json FROM fuzz_campaigns
		 WHERE status IN ('planned','running')
		   AND json_extract(config_json, '$.pool_distributed') IN (1, 'true', '1')`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, ownerRef, cfgJSON string
		var budgetRuns, budgetSec int
		if err := rows.Scan(&id, &ownerRef, &budgetRuns, &budgetSec, &cfgJSON); err != nil {
			continue
		}
		if _, ok := failed[id]; !ok {
			continue
		}
		// Startup retry runs 3s after the worker boots, so a campaign that failed inside
		// that window would otherwise jump its backoff here. Operator- and customer-driven
		// syncs stay unthrottled: only the automatic paths consult the deadline.
		if !a.poolSyncRetryDue(id) {
			continue
		}
		a.poolSyncMu.Lock()
		delete(a.poolSyncQueued, id)
		a.poolSyncMu.Unlock()
		_, _ = a.schedulePoolFuzzSync(ctx, fuzzAutoCampaign{
			ID: id, OwnerRef: ownerRef, BudgetRuns: budgetRuns, BudgetSeconds: budgetSec, ConfigJSON: cfgJSON,
		})
		log.Printf("pool sync: retry queued for %s", logsafe.ID(id))
	}
}

func (a *app) poolSyncWorkerLoop() {
	for job := range a.poolSyncCh {
		a.runPoolSyncJob(job)
	}
}

func (a *app) runPoolSyncJob(job poolSyncJob) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cfg := parseMapJSON(job.campaign.ConfigJSON)
	ctype := strings.TrimSpace(job.ctype)
	if ctype == "" {
		ctype = "property"
	}
	req, prepErr := a.preparePoolRegisterRequest(ctx, job.campaign, job.title, job.desc, ctype, cfg)
	if prepErr != nil {
		log.Printf("pool sync: campaign %s prep failed: %s", logsafe.ID(job.campaign.ID), logsafe.Err(prepErr))
		a.poolSyncMarkFailed(job.campaign.ID, prepErr)
		return
	}
	res, err := poolsync.RegisterWithRetryResult(ctx, req)
	if err != nil {
		log.Printf("pool sync: campaign %s failed after retries: %v", logsafe.ID(job.campaign.ID), err)
		a.poolSyncMarkFailed(job.campaign.ID, err)
		return
	}
	if sec := strings.TrimSpace(res.OrdersOwnerSecret); sec != "" {
		a.persistOrdersOwnerSecret(ctx, job.campaign.ID, sec)
	}
	if fuzzengine.CorpusPersistEnabled(cfg) {
		a.syncCorpusNamespaceToCoordinator(ctx, cfg)
	}
	log.Printf("pool sync: campaign %s registered on coordinator", logsafe.ID(job.campaign.ID))
	a.poolSyncMarkOK(job.campaign.ID)
}

// preparePoolRegisterRequest uploads Hunt harness (when needed) and attests open
// escrow fingerprint. Shared by async worker and sync register path.
func (a *app) preparePoolRegisterRequest(ctx context.Context, c fuzzAutoCampaign, title, desc, ctype string, cfg map[string]any) (poolsync.RegisterRequest, error) {
	if ctype == "" {
		ctype = "property"
	}
	// Hunt: upload harness BEFORE registering so workers never claim shards
	// against a missing ASAN binary (yyjson-class lease spin).
	if poolfuzz.IsHuntCampaign(cfg) {
		if herr := a.syncHuntHarnessToCoordinator(ctx, cfg); herr != nil {
			return poolsync.RegisterRequest{}, herr
		}
	}
	budgetHMC := 0.0
	if v, ok := cfg["budget_hmc"].(float64); ok {
		budgetHMC = v
	}
	escrowFP := ""
	if a.chain != nil && budgetHMC > 0 {
		row, err := a.chain.GetFuzzEscrow(ctx, c.ID)
		if err != nil {
			return poolsync.RegisterRequest{}, fmt.Errorf("open escrow required before pool register: %w", err)
		}
		if row == nil {
			return poolsync.RegisterRequest{}, fmt.Errorf("open escrow required before pool register")
		}
		st := strings.ToLower(strings.TrimSpace(row.Status))
		if st != "open" {
			return poolsync.RegisterRequest{}, fmt.Errorf("escrow status %q not open", row.Status)
		}
		// Economics v2: escrow is price minus order fee. Accept escrow >= price - fee and
		// fingerprint the escrow amount miners can actually be paid from.
		minEscrow := budgetHMC
		if tipH, _, terr := a.chain.Tip(ctx); terr == nil {
			if fee, _ := chain.OrderFeeV2FromPrice(tipH, chain.HMCToUnits(budgetHMC), chain.FeeAssetHMC); fee.HMCUnits() > 0 && chain.EconomicsV2ActiveForIndex(tipH+1) {
				minEscrow = chain.UnitsToHMC(chain.HMCToUnits(budgetHMC) - fee.HMCUnits())
			}
		}
		if row.BudgetHMC+1e-6 < minEscrow || (c.BudgetRuns > 0 && row.BudgetRuns < c.BudgetRuns) {
			return poolsync.RegisterRequest{}, fmt.Errorf("escrow budget/runs below publish claim")
		}
		if row.BudgetHMC < budgetHMC {
			budgetHMC = row.BudgetHMC
		}
		escrowFP = poolsync.EscrowPrepaidFingerprintParts(c.ID, budgetHMC, c.BudgetRuns, "open")
	} else if poolsync.RequireHubPrepaidTicket() || poolsync.OrdersPrepaidHubSecret() != "" {
		return poolsync.RegisterRequest{}, fmt.Errorf("open escrow required for hub prepaid ticket")
	}
	return poolsync.RegisterRequest{
		ID:                c.ID,
		CampaignType:      ctype,
		Title:             title,
		Description:       desc,
		OwnerRef:          c.OwnerRef,
		Status:            "running",
		BudgetRuns:        c.BudgetRuns,
		BudgetSeconds:     c.BudgetSeconds,
		BudgetHMC:         budgetHMC,
		EscrowFingerprint: escrowFP,
		Config:            cfg,
	}, nil
}

func (a *app) persistOrdersOwnerSecret(ctx context.Context, campaignID, secret string) {
	campaignID = strings.TrimSpace(campaignID)
	secret = strings.TrimSpace(secret)
	if a == nil || campaignID == "" || secret == "" {
		return
	}
	// Prefer 0600 file outside config_json (PUB-08); keep DB wipe of any prior plaintext.
	_ = poolsync.PersistOrdersOwnerSecret(campaignID, secret)
	if a.db == nil {
		return
	}
	var raw string
	if err := a.db.QueryRowContext(ctx, `SELECT COALESCE(config_json,'{}') FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&raw); err != nil {
		return
	}
	cfg := parseMapJSON(raw)
	if cfg == nil {
		return
	}
	if _, had := cfg["orders_owner_secret"]; !had {
		return
	}
	delete(cfg, "orders_owner_secret")
	_, _ = a.db.ExecContext(ctx, `UPDATE fuzz_campaigns SET config_json=? WHERE id=?`, marshalMapJSON(cfg), campaignID)
}

func (a *app) poolSyncMarkOK(id string) {
	a.poolSyncMu.Lock()
	delete(a.poolSyncFailed, id)
	delete(a.poolSyncQueued, id)
	delete(a.poolSyncFailStreak, id)
	delete(a.poolSyncRetryAt, id)
	a.poolSyncMu.Unlock()
	if a.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		a.persistPoolRegisterStatus(ctx, id, "ok", "")
	}
}

func (a *app) poolSyncMarkFailed(id string, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	a.poolSyncMu.Lock()
	if a.poolSyncFailed == nil {
		a.poolSyncFailed = make(map[string]string)
	}
	a.poolSyncFailed[id] = msg
	delete(a.poolSyncQueued, id)
	if a.poolSyncFailStreak == nil {
		a.poolSyncFailStreak = make(map[string]int)
	}
	if a.poolSyncRetryAt == nil {
		a.poolSyncRetryAt = make(map[string]int64)
	}
	a.poolSyncFailStreak[id]++
	a.poolSyncRetryAt[id] = time.Now().Unix() + poolSyncRetryBackoffSec(a.poolSyncFailStreak[id])
	a.poolSyncMu.Unlock()
	if a.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		a.persistPoolRegisterStatus(ctx, id, "pool_register_failed", msg)
		a.poolFallbackToLocal(ctx, id, msg)
	}
}

// poolFallbackToLocal runs a paid order on this machine when the pool would not take it.
// rc20 product path: default OFF — local_fallback breaks the "pool" promise. Lab/dev must
// set HACKME_ORDERS_ALLOW_LOCAL_FALLBACK=1 (or legacy HACKME_ORDERS_POOL_FALLBACK_LOCAL=1).
func (a *app) poolFallbackToLocal(ctx context.Context, id, reason string) {
	if a == nil || a.db == nil {
		return
	}
	legacy := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_ORDERS_POOL_FALLBACK_LOCAL")))
	allow := ordersAllowLocalFallback() || legacy == "1" || legacy == "true" || legacy == "yes" || legacy == "on"
	if !allow {
		return
	}
	var cfgRaw, sumRaw, status string
	if err := a.db.QueryRowContext(ctx,
		`SELECT COALESCE(config_json,'{}'), COALESCE(summary_json,'{}'), status FROM fuzz_campaigns WHERE id=?`, id).
		Scan(&cfgRaw, &sumRaw, &status); err != nil {
		return
	}
	if st := strings.ToLower(strings.TrimSpace(status)); st != "planned" && st != "running" {
		return
	}
	cfg := parseMapJSON(cfgRaw)
	if cfg == nil || !poolDistributedCampaign(cfg) || poolEscrowMirrorCampaign(cfg) {
		return // a paid pool order's mirror never turns into a local run
	}
	if envBool("HACKME_CHAIN_MINTING_HUB", false) {
		// The hub never fuzzes customer work on its own CPU (it would pay itself from the
		// escrow). A paid pool order the pool refused is cancelled instead: the escrow closes
		// and the unspent part goes back to the payer (ordersPoolRefundOnClose).
		if truthy(cfg["orders_pool_escrow"]) {
			// Persist the refusal beside the status: the log line below rotates away, and
			// the owner of a cancelled paid order has no other way to learn why.
			a.recordCampaignCancelReason(ctx, id, "pool_refused", reason)
			if _, err := a.db.ExecContext(ctx, `UPDATE fuzz_campaigns SET status='cancelled', completed_at=? WHERE id=? AND status IN ('planned','running')`,
				time.Now().Unix(), id); err == nil {
				log.Printf("pool sync: paid pool order %s refused by the pool (%s) — cancelled, refund follows", logsafe.ID(id), reason)
				_ = a.pushPoolCampaignStatus(ctx, id, "cancelled")
				a.tryCloseFuzzEscrowForStatus(ctx, id, "cancelled")
			}
		}
		return
	}
	cfg["pool_distributed"] = false
	cfg["pool_fallback_local"] = true
	cfg["auto_runner"] = "1" // pool Hunt is created with auto_runner=0; the local runner must pick it up
	if poolfuzz.IsHuntCampaign(cfg) {
		cfg["hunt_local_runner"] = true
	}
	sum := parseMapJSON(sumRaw)
	if sum == nil {
		sum = map[string]any{}
	}
	sum["execution"] = "local_fallback"
	sum["execution_note"] = "The public pool did not accept this order (" + reason + "); it runs on this machine instead. Budget and refunds stay in this node's prepaid balance."
	if _, err := a.db.ExecContext(ctx, `UPDATE fuzz_campaigns SET config_json=?, summary_json=? WHERE id=?`,
		marshalMapJSON(cfg), marshalMapJSON(sum), id); err != nil {
		log.Printf("pool sync: local fallback for %s failed: %v", logsafe.ID(id), err)
		return
	}
	log.Printf("pool sync: campaign %s runs locally (pool register failed)", logsafe.ID(id))
}

// persistPoolRegisterStatus stamps summary_json so the dashboard shows more than "running".
func (a *app) persistPoolRegisterStatus(ctx context.Context, id, status, errMsg string) {
	id = strings.TrimSpace(id)
	if id == "" || a == nil || a.db == nil {
		return
	}
	var raw string
	if err := a.db.QueryRowContext(ctx, `SELECT COALESCE(summary_json,'{}') FROM fuzz_campaigns WHERE id=?`, id).Scan(&raw); err != nil {
		return
	}
	sum := parseMapJSON(raw)
	if sum == nil {
		sum = map[string]any{}
	}
	sum["pool_register_status"] = status
	if errMsg != "" {
		sum["pool_register_error"] = errMsg
		sum["pool_register_failed_at"] = time.Now().Unix()
	} else {
		delete(sum, "pool_register_error")
		delete(sum, "pool_register_failed_at")
	}
	_, _ = a.db.ExecContext(ctx, `UPDATE fuzz_campaigns SET summary_json=? WHERE id=?`, marshalMapJSON(sum), id)
}

func (a *app) poolSyncFailedIDs() map[string]string {
	a.poolSyncMu.Lock()
	defer a.poolSyncMu.Unlock()
	out := make(map[string]string, len(a.poolSyncFailed))
	for k, v := range a.poolSyncFailed {
		out[k] = v
	}
	return out
}

func (a *app) schedulePoolFuzzSync(ctx context.Context, c fuzzAutoCampaign) (syncMode string, syncWarning string) {
	a.startPoolSyncWorker()
	var title, desc, ctype, ownerRef string
	_ = a.db.QueryRowContext(ctx,
		`SELECT title, description, campaign_type, COALESCE(owner_ref,'') FROM fuzz_campaigns WHERE id=?`, c.ID).
		Scan(&title, &desc, &ctype, &ownerRef)
	if strings.TrimSpace(c.OwnerRef) == "" {
		c.OwnerRef = ownerRef
	}
	job := poolSyncJob{campaign: c, title: title, desc: desc, ctype: ctype}

	// Dedupe: skip if already queued recently (fuzz_runner may call sync repeatedly).
	a.poolSyncMu.Lock()
	if a.poolSyncQueued == nil {
		a.poolSyncQueued = make(map[string]struct{})
	}
	if _, dup := a.poolSyncQueued[c.ID]; dup {
		a.poolSyncMu.Unlock()
		return "queued", ""
	}
	a.poolSyncQueued[c.ID] = struct{}{}
	a.poolSyncMu.Unlock()

	if poolsync.AsyncEnabled() {
		poolsync.RecordQueued(c.ID)
		select {
		case a.poolSyncCh <- job:
			return "queued", ""
		default:
			// channel full — run in dedicated goroutine so API still returns fast
			go func() { a.runPoolSyncJob(job) }()
			return "queued", "pool sync queue busy; running in background"
		}
	}

	if err := a.syncPoolFuzzCampaignSync(ctx, c, title, desc, ctype); err != nil {
		a.poolSyncMarkFailed(c.ID, err)
		return "fail", err.Error()
	}
	a.poolSyncMarkOK(c.ID)
	return "ok", ""
}

func (a *app) syncPoolFuzzCampaignSync(ctx context.Context, c fuzzAutoCampaign, title, desc, ctype string) error {
	cfg := parseMapJSON(c.ConfigJSON)
	req, err := a.preparePoolRegisterRequest(ctx, c, title, desc, ctype, cfg)
	if err != nil {
		return err
	}
	res, err := poolsync.RegisterWithRetryResult(ctx, req)
	if err != nil {
		return err
	}
	if sec := strings.TrimSpace(res.OrdersOwnerSecret); sec != "" {
		a.persistOrdersOwnerSecret(ctx, c.ID, sec)
	}
	if fuzzengine.CorpusPersistEnabled(cfg) {
		a.syncCorpusNamespaceToCoordinator(ctx, cfg)
	}
	return nil
}

// poolSyncRetryBackoffCapSec caps the per-campaign register backoff: an operator fix
// (restored harness artifact, minted prepaid ticket) is picked up within this window
// without a node restart.
const poolSyncRetryBackoffCapSec = int64(6 * 3600)

// poolSyncRetryBackoffSec grows 3m, 6m, 12m, ... from the reconcile tick up to the cap.
func poolSyncRetryBackoffSec(streak int) int64 {
	if streak <= 1 {
		return 180
	}
	d := int64(180)
	for i := 1; i < streak; i++ {
		d *= 2
		if d >= poolSyncRetryBackoffCapSec {
			return poolSyncRetryBackoffCapSec
		}
	}
	return d
}

// poolSyncRetryDue reports whether a previously failed campaign may be re-queued now.
func (a *app) poolSyncRetryDue(id string) bool {
	a.poolSyncMu.Lock()
	defer a.poolSyncMu.Unlock()
	at, ok := a.poolSyncRetryAt[id]
	if !ok {
		return true
	}
	return time.Now().Unix() >= at
}
