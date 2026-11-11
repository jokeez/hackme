package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

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
	})
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
		// Retry missing campaigns every tick — including previously failed (hub cap may rise).
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
	// Hunt: upload harness BEFORE registering the campaign so workers never claim
	// shards against a missing ASAN binary (yyjson-class lease spin).
	if poolfuzz.IsHuntCampaign(cfg) {
		if herr := a.syncHuntHarnessToCoordinator(ctx, cfg); herr != nil {
			log.Printf("pool sync: campaign %s harness upload failed: %s", logsafe.ID(job.campaign.ID), logsafe.Err(herr))
			a.poolSyncMarkFailed(job.campaign.ID, herr)
			return
		}
	}
	budgetHMC := 0.0
	if v, ok := cfg["budget_hmc"].(float64); ok {
		budgetHMC = v
	}
	escrowFP := ""
	if a.chain != nil && budgetHMC > 0 {
		row, err := a.chain.GetFuzzEscrow(ctx, job.campaign.ID)
		if err != nil || row == nil {
			log.Printf("pool sync: campaign %s missing open escrow before prepaid attest: %v", logsafe.ID(job.campaign.ID), err)
			a.poolSyncMarkFailed(job.campaign.ID, fmt.Errorf("open escrow required before pool register"))
			return
		}
		st := strings.ToLower(strings.TrimSpace(row.Status))
		if st != "open" {
			a.poolSyncMarkFailed(job.campaign.ID, fmt.Errorf("escrow status %q not open", row.Status))
			return
		}
		if row.BudgetHMC+1e-9 < budgetHMC || (job.campaign.BudgetRuns > 0 && row.BudgetRuns < job.campaign.BudgetRuns) {
			a.poolSyncMarkFailed(job.campaign.ID, fmt.Errorf("escrow budget/runs below publish claim"))
			return
		}
		// Fingerprint the *claimed* open escrow (not opaque row noise) so hub verify can recompute.
		escrowFP = poolsync.EscrowPrepaidFingerprintParts(job.campaign.ID, budgetHMC, job.campaign.BudgetRuns, "open")
	} else if poolsync.RequireHubPrepaidTicket() || poolsync.OrdersPrepaidHubSecret() != "" {
		a.poolSyncMarkFailed(job.campaign.ID, fmt.Errorf("open escrow required for hub prepaid ticket"))
		return
	}
	req := poolsync.RegisterRequest{
		ID:                job.campaign.ID,
		CampaignType:      ctype,
		Title:             job.title,
		Description:       job.desc,
		OwnerRef:          job.campaign.OwnerRef,
		Status:            "running",
		BudgetRuns:        job.campaign.BudgetRuns,
		BudgetSeconds:     job.campaign.BudgetSeconds,
		BudgetHMC:         budgetHMC,
		EscrowFingerprint: escrowFP,
		Config:            cfg,
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
	a.poolSyncMu.Unlock()
	if a.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		a.persistPoolRegisterStatus(ctx, id, "pool_register_failed", msg)
	}
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
		return "fail", err.Error()
	}
	return "ok", ""
}

func (a *app) syncPoolFuzzCampaignSync(ctx context.Context, c fuzzAutoCampaign, title, desc, ctype string) error {
	cfg := parseMapJSON(c.ConfigJSON)
	if ctype == "" {
		ctype = "property"
	}
	budgetHMC := 0.0
	if v, ok := cfg["budget_hmc"].(float64); ok {
		budgetHMC = v
	}
	res, err := poolsync.RegisterWithRetryResult(ctx, poolsync.RegisterRequest{
		ID:            c.ID,
		CampaignType:  ctype,
		Title:         title,
		Description:   desc,
		OwnerRef:      c.OwnerRef,
		Status:        "running",
		BudgetRuns:    c.BudgetRuns,
		BudgetSeconds: c.BudgetSeconds,
		BudgetHMC:     budgetHMC,
		Config:        cfg,
	})
	if err != nil {
		return err
	}
	if sec := strings.TrimSpace(res.OrdersOwnerSecret); sec != "" {
		a.persistOrdersOwnerSecret(ctx, c.ID, sec)
	}
	return nil
}
