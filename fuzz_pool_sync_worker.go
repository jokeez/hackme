package main

import (
	"context"
	"log"
	"strings"
	"time"

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
		`SELECT id, budget_runs, budget_seconds, config_json FROM fuzz_campaigns
		 WHERE status IN ('planned','running')
		   AND json_extract(config_json, '$.pool_distributed') IN (1, 'true', '1')
		 ORDER BY created_at DESC LIMIT 64`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, cfgJSON string
		var budgetRuns, budgetSec int
		if err := rows.Scan(&id, &budgetRuns, &budgetSec, &cfgJSON); err != nil {
			continue
		}
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if failed := a.poolSyncFailedIDs(); len(failed) > 0 {
			if _, ok := failed[id]; ok {
				continue
			}
		}
		progCtx, progCancel := context.WithTimeout(ctx, 6*time.Second)
		_, ok := a.fetchCoordinatorPoolCampaignProgress(progCtx, id)
		progCancel()
		if ok {
			continue
		}
		a.poolSyncMu.Lock()
		delete(a.poolSyncQueued, id)
		a.poolSyncMu.Unlock()
		mode, warn := a.schedulePoolFuzzSync(ctx, fuzzAutoCampaign{
			ID: id, BudgetRuns: budgetRuns, BudgetSeconds: budgetSec, ConfigJSON: cfgJSON,
		})
		if warn != "" {
			log.Printf("pool sync reconcile: %s warn=%s", id, warn)
		} else {
			log.Printf("pool sync reconcile: %s mode=%s", id, mode)
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
		`SELECT id, budget_runs, budget_seconds, config_json FROM fuzz_campaigns
		 WHERE status IN ('planned','running')
		   AND json_extract(config_json, '$.pool_distributed') IN (1, 'true', '1')`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, cfgJSON string
		var budgetRuns, budgetSec int
		if err := rows.Scan(&id, &budgetRuns, &budgetSec, &cfgJSON); err != nil {
			continue
		}
		if _, ok := failed[id]; !ok {
			continue
		}
		a.poolSyncMu.Lock()
		delete(a.poolSyncQueued, id)
		a.poolSyncMu.Unlock()
		_, _ = a.schedulePoolFuzzSync(ctx, fuzzAutoCampaign{
			ID: id, BudgetRuns: budgetRuns, BudgetSeconds: budgetSec, ConfigJSON: cfgJSON,
		})
		log.Printf("pool sync: retry queued for %s", id)
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
	req := poolsync.RegisterRequest{
		ID:            job.campaign.ID,
		CampaignType:  ctype,
		Title:         job.title,
		Description:   job.desc,
		Status:        "running",
		BudgetRuns:    job.campaign.BudgetRuns,
		BudgetSeconds: job.campaign.BudgetSeconds,
		Config:        cfg,
	}
	err := poolsync.RegisterWithRetry(ctx, req)
	if err != nil {
		log.Printf("pool sync: campaign %s failed after retries: %v", job.campaign.ID, err)
		a.poolSyncMarkFailed(job.campaign.ID, err)
		return
	}
	log.Printf("pool sync: campaign %s registered on coordinator", job.campaign.ID)
	a.poolSyncMarkOK(job.campaign.ID)
}

func (a *app) poolSyncMarkOK(id string) {
	a.poolSyncMu.Lock()
	delete(a.poolSyncFailed, id)
	a.poolSyncMu.Unlock()
}

func (a *app) poolSyncMarkFailed(id string, err error) {
	a.poolSyncMu.Lock()
	if a.poolSyncFailed == nil {
		a.poolSyncFailed = make(map[string]string)
	}
	a.poolSyncFailed[id] = err.Error()
	a.poolSyncMu.Unlock()
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
	var title, desc, ctype string
	_ = a.db.QueryRowContext(ctx,
		`SELECT title, description, campaign_type FROM fuzz_campaigns WHERE id=?`, c.ID).
		Scan(&title, &desc, &ctype)
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
	return poolsync.RegisterWithRetry(ctx, poolsync.RegisterRequest{
		ID:            c.ID,
		CampaignType:  ctype,
		Title:         title,
		Description:   desc,
		Status:        "running",
		BudgetRuns:    c.BudgetRuns,
		BudgetSeconds: c.BudgetSeconds,
		Config:        cfg,
	})
}
