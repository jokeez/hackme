package poolfuzz

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"hackme/internal/fuzzartifacts"
	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzingcli"
	"hackme/internal/fuzznative"
	"hackme/internal/fuzzupstream"
	"hackme/internal/hunt"
	"hackme/internal/sandbox"
)

// Service runs distributed fuzz work queues on the coordinator SQLite DB.
type Service struct {
	DB      *sql.DB
	Settler Settler
	claimRR atomic.Uint64 // round-robin cursor across runnable campaigns
}

type Campaign struct {
	ID            string
	CampaignType  string
	Status        string
	Title         string
	Description   string
	OwnerRef      string
	BudgetRuns    int
	BudgetSeconds int
	Config        map[string]any
	Summary       map[string]any
}

type ClaimedWork struct {
	WorkID               string
	CampaignID           string
	ItemID               int64
	InputN               uint64
	ActualInput          uint64
	InputBytes           []byte
	InputMode            string
	WasmCheckHex         string
	CheckSemantics       string
	DepthTier            string
	PerRunHMC            float64
	ExecPerUnit          int
	MaxInputBytes        int
	CoverageKind         string
	CorpusSeeds          []fuzzengine.PoolCorpusSeed
	CorpusSnapshotSHA256 string
	TaskClass            string
	WorkKind             string
	HarnessHash          string
	UpstreamTargetID     string
	HuntSource           string
	HuntPinPath          string
	HuntSourceRel        string
	HarnessFetchURL      string
	IterationsPerShard   int
	HuntDetectLeaks      bool
}

type SubmitRequest struct {
	WorkerID        string
	MinerAddress    string
	WorkID          string
	CampaignID      string
	ItemID          int64
	InputN          uint64
	ActualInput     uint64
	InputBytes      []byte
	CheckResult     int32
	DurationMS      int
	Trap            string
	SegmentExecDone int
}

// RegisterCampaign upserts a pool-distributed fuzz campaign and marks it running.
func (s *Service) RegisterCampaign(ctx context.Context, c Campaign) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("poolfuzz: no database")
	}
	c.ID = strings.TrimSpace(c.ID)
	if c.ID == "" {
		return fmt.Errorf("poolfuzz: campaign id required")
	}
	cfg := fuzzengine.NormalizeCampaignConfig(c.Config, c.CampaignType)
	cfg["pool_distributed"] = true
	if _, ok := cfg["auto_runner"]; !ok {
		cfg["auto_runner"] = "0"
	}
	now := time.Now().Unix()
	status := strings.TrimSpace(strings.ToLower(c.Status))
	if status == "" {
		status = "running"
	}
	summary := map[string]any{
		"fuzz_engine": fuzzengine.MetaFromConfig(cfg),
		"pool":        true,
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO fuzz_campaigns
		 (id, campaign_type, status, title, description, owner_ref, task_id, target_ref,
		  budget_runs, budget_seconds, config_json, summary_json, created_at, started_at, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, '', '', ?, ?, ?, ?, ?, ?, 0)
		 ON CONFLICT(id) DO UPDATE SET
		   campaign_type=excluded.campaign_type,
		   status=excluded.status,
		   title=excluded.title,
		   description=excluded.description,
		   owner_ref=CASE WHEN excluded.owner_ref != '' THEN excluded.owner_ref ELSE fuzz_campaigns.owner_ref END,
		   budget_runs=excluded.budget_runs,
		   budget_seconds=excluded.budget_seconds,
		   config_json=excluded.config_json,
		   summary_json=excluded.summary_json,
		   completed_at=CASE WHEN excluded.status IN ('planned','running') THEN 0 ELSE fuzz_campaigns.completed_at END,
		   started_at=CASE WHEN fuzz_campaigns.started_at=0 THEN excluded.started_at ELSE fuzz_campaigns.started_at END`,
		c.ID, strings.TrimSpace(c.CampaignType), status, strings.TrimSpace(c.Title), strings.TrimSpace(c.Description),
		strings.TrimSpace(c.OwnerRef),
		c.BudgetRuns, c.BudgetSeconds, marshalConfigJSON(cfg), marshalSummaryJSON(summary), now, now)
	if err != nil {
		return err
	}
	// Seed work items once on register so claims do not need Tick-on-claim.
	if status == "running" || status == "planned" {
		if err := s.reconcileActiveCampaignWork(ctx, c.ID, now); err != nil {
			return err
		}
		_ = s.EnsureWorkItems(ctx, c.ID, now)
		if err := s.seedPoolCorpusFromConfig(ctx, c.ID, cfg, now); err != nil {
			return err
		}
		if err := s.importNamespaceCorpus(ctx, c.ID, cfg, now); err != nil {
			return err
		}
	}
	return nil
}

// reconcileActiveCampaignWork fixes running/planned pool campaigns that cannot claim:
// completed_at set while still active, or all work cancelled after a re-register/resync.
func (s *Service) reconcileActiveCampaignWork(ctx context.Context, campaignID string, now int64) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("poolfuzz: no database")
	}
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return fmt.Errorf("poolfuzz: campaign id required")
	}
	var status string
	var budgetRuns int
	var completedAt int64
	if err := s.DB.QueryRowContext(ctx,
		`SELECT status, budget_runs, completed_at FROM fuzz_campaigns WHERE id=?`, campaignID).
		Scan(&status, &budgetRuns, &completedAt); err != nil {
		return err
	}
	st := strings.TrimSpace(strings.ToLower(status))
	if st != "planned" && st != "running" {
		return nil
	}
	var doneCnt int
	_ = s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id=? AND status='done'`, campaignID).Scan(&doneCnt)
	if budgetRuns > 0 && doneCnt >= budgetRuns {
		return nil
	}
	if completedAt != 0 {
		if _, err := s.DB.ExecContext(ctx,
			`UPDATE fuzz_campaigns SET completed_at=0 WHERE id=? AND status IN ('planned','running')`, campaignID); err != nil {
			return err
		}
	}
	var claimable int
	_ = s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM fuzz_work_items
		 WHERE campaign_id=?
		   AND (status='pending' OR (status='leased' AND lease_until < ?))`,
		campaignID, now).Scan(&claimable)
	if claimable > 0 {
		return nil
	}
	var cancelled int
	_ = s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id=? AND status='cancelled'`, campaignID).Scan(&cancelled)
	if cancelled == 0 {
		return nil
	}
	_, err := s.DB.ExecContext(ctx,
		`UPDATE fuzz_work_items
		 SET status='pending', lease_owner='', lease_until=0, attempts=0, last_error='', updated_at=?
		 WHERE campaign_id=? AND status='cancelled'`,
		now, campaignID)
	return err
}

// RepairZombiePoolCampaigns scans active pool campaigns and reconciles claim queues.
func (s *Service) RepairZombiePoolCampaigns(ctx context.Context, limit int) (int, error) {
	if s == nil || s.DB == nil {
		return 0, fmt.Errorf("poolfuzz: no database")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT c.id FROM fuzz_campaigns c
		 WHERE c.status IN ('planned','running')
		   AND json_extract(c.config_json, '$.pool_distributed') IN (1, 'true', '1')
		   AND (
		     c.completed_at != 0
		     OR (
		       COALESCE((SELECT COUNT(*) FROM fuzz_work_items w WHERE w.campaign_id=c.id AND w.status IN ('pending','leased')),0) = 0
		       AND COALESCE((SELECT COUNT(*) FROM fuzz_work_items w WHERE w.campaign_id=c.id AND w.status='cancelled'),0) > 0
		       AND COALESCE((SELECT COUNT(*) FROM fuzz_work_items w WHERE w.campaign_id=c.id AND w.status='done'),0) < c.budget_runs
		     )
		   )
		 ORDER BY c.created_at ASC
		 LIMIT ?`, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	now := time.Now().Unix()
	n := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return n, err
		}
		if err := s.reconcileActiveCampaignWork(ctx, id, now); err != nil {
			return n, err
		}
		if err := s.EnsureWorkItems(ctx, id, now); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

// SetCampaignStatus updates campaign lifecycle and cancels pending work when stopping.
func (s *Service) SetCampaignStatus(ctx context.Context, id, status string) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("poolfuzz: no database")
	}
	id = strings.TrimSpace(id)
	status = strings.TrimSpace(strings.ToLower(status))
	if id == "" {
		return fmt.Errorf("poolfuzz: campaign id required")
	}
	switch status {
	case "planned", "running", "paused", "completed", "cancelled":
	default:
		return fmt.Errorf("poolfuzz: invalid status %q", status)
	}
	now := time.Now().Unix()
	completedAt := int64(0)
	if status == "completed" || status == "cancelled" {
		completedAt = now
	}
	res, err := s.DB.ExecContext(ctx,
		`UPDATE fuzz_campaigns
		 SET status=?, completed_at=CASE WHEN ?=0 THEN completed_at ELSE ? END
		 WHERE id=?`,
		status, completedAt, completedAt, id)
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return fmt.Errorf("poolfuzz: campaign not found")
	}
	if status == "completed" || status == "cancelled" || status == "paused" {
		_, _ = s.DB.ExecContext(ctx,
			`UPDATE fuzz_work_items
			 SET status='cancelled', updated_at=?
			 WHERE campaign_id=? AND status IN ('pending','leased')`,
			now, id)
	}
	return nil
}

// CancelInternalGateCampaigns stops health-check / probe pool campaigns.
func (s *Service) CancelInternalGateCampaigns(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, title, owner_ref, config_json
		 FROM fuzz_campaigns
		 WHERE status IN ('planned','running')
		   AND json_extract(config_json, '$.pool_distributed') IN (1, 'true', '1')
		 ORDER BY created_at DESC
		 LIMIT ?`, limit*4)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, title, ownerRef, cfgJSON string
		if err := rows.Scan(&id, &title, &ownerRef, &cfgJSON); err != nil {
			return n, err
		}
		cfg := parseConfigJSON(cfgJSON)
		if !IsInternalGateCampaign(id, title, ownerRef, cfg) {
			continue
		}
		if err := s.SetCampaignStatus(ctx, id, "cancelled"); err != nil {
			return n, err
		}
		n++
		if n >= limit {
			break
		}
	}
	return n, rows.Err()
}

// CancelZeroProgressPoolCampaigns stops pool campaigns that never completed a run.
func (s *Service) CancelZeroProgressPoolCampaigns(ctx context.Context, minAgeSec int64, limit int) (int, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	now := time.Now().Unix()
	if minAgeSec < 0 {
		minAgeSec = 3600
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT c.id, c.title, c.owner_ref, c.config_json, c.summary_json, c.created_at
		 FROM fuzz_campaigns c
		 WHERE c.status IN ('planned','running')
		   AND json_extract(c.config_json, '$.pool_distributed') IN (1, 'true', '1')
		   AND (? - c.created_at) >= ?
		 ORDER BY c.created_at ASC
		 LIMIT ?`, now, minAgeSec, limit*4)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, title, ownerRef, cfgJSON, summaryJSON string
		var createdAt int64
		if err := rows.Scan(&id, &title, &ownerRef, &cfgJSON, &summaryJSON, &createdAt); err != nil {
			return n, err
		}
		cfg := parseConfigJSON(cfgJSON)
		summary := parseConfigJSON(summaryJSON)
		runsDone := intFromJSON(summary["runs_done"])
		if runsDone > 0 {
			continue
		}
		if err := s.SetCampaignStatus(ctx, id, "cancelled"); err != nil {
			return n, err
		}
		n++
		if n >= limit {
			break
		}
		_ = cfg
		_ = ownerRef
	}
	return n, rows.Err()
}

// EnsureWorkItems tops up pending queue for active pool campaigns.
func (s *Service) EnsureWorkItems(ctx context.Context, campaignID string, now int64) error {
	var budgetRuns int
	var cfgJSON string
	if err := s.DB.QueryRowContext(ctx,
		`SELECT budget_runs, config_json FROM fuzz_campaigns WHERE id=? AND status IN ('planned','running')`,
		campaignID).Scan(&budgetRuns, &cfgJSON); err != nil {
		return err
	}
	cfg := parseConfigJSON(cfgJSON)
	queueDepth := 128
	if v, ok := cfg["queue_depth"]; ok {
		if n := intFromJSON(v); n > 0 && n <= 10000 {
			queueDepth = n
		}
	}
	var existing int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id=?`, campaignID).Scan(&existing); err != nil {
		return err
	}
	if existing >= budgetRuns {
		return nil
	}
	toCreate := budgetRuns - existing
	if toCreate > queueDepth {
		toCreate = queueDepth
	}
	for i := 0; i < toCreate; i++ {
		inputN := uint64(existing + i + 1)
		_, err := s.DB.ExecContext(ctx,
			`INSERT OR IGNORE INTO fuzz_work_items
			 (campaign_id, input_n, status, attempts, last_error, lease_owner, lease_until, result_ok, duration_ms, created_at, updated_at)
			 VALUES (?, ?, 'pending', 0, '', '', 0, 0, 0, ?, ?)`,
			campaignID, inputN, now, now)
		if err != nil {
			return err
		}
	}
	return nil
}

// Tick tops up queues for all pool campaigns (coordinator calls periodically).
func (s *Service) Tick(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id FROM fuzz_campaigns WHERE status IN ('planned','running') ORDER BY created_at ASC LIMIT 100`)
	if err != nil {
		return err
	}
	defer rows.Close()
	now := time.Now().Unix()
	if _, err := s.RepairZombiePoolCampaigns(ctx, 10); err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		cfg := map[string]any{}
		var cfgJSON string
		_ = s.DB.QueryRowContext(ctx, `SELECT config_json FROM fuzz_campaigns WHERE id=?`, id).Scan(&cfgJSON)
		cfg = parseConfigJSON(cfgJSON)
		if !poolDistributed(cfg) {
			continue
		}
		if err := s.reconcileActiveCampaignWork(ctx, id, now); err != nil {
			return err
		}
		if err := s.EnsureWorkItems(ctx, id, now); err != nil {
			return err
		}
		if _, err := s.recomputeProgress(ctx, id, now); err != nil {
			return err
		}
	}
	if pins, err := fuzznative.LoadPins(""); err == nil {
		_, _ = fuzznative.ProcessPending(ctx, s.DB, pins, 5)
	}
	_ = s.flushDeferredBounties(ctx)
	return rows.Err()
}

const claimCandidateLimit = 512 // retained for tests / callers; claim path is campaign-RR now

// Claim leases one work item for a pool worker.
// Phases (index-friendly, no global FIFO over huge pending walls):
//  1. customer campaigns (full sweep — always before bootstrap)
//  2. near-complete (throttled every 32nd claim)
//  3. expired lease reclaim
//  4. round-robin among other + bootstrap
func (s *Service) Claim(ctx context.Context, workerID string, now int64) (ClaimedWork, bool, error) {
	var out ClaimedWork
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return out, false, fmt.Errorf("poolfuzz: worker_id required")
	}
	// Tick runs on the coordinator background ticker (every ~3s). Calling it on every
	// claim path is campaign-RR now; lease duration is computed per campaign in claimOnePendingInCampaign.
	customers, rest, err := s.runnablePoolCampaignIDsByTier(ctx, now)
	if err != nil {
		return out, false, err
	}

	// Phase 1: customer pending always wins over bootstrap lease reclaim / RR.
	for _, cid := range customers {
		if work, ok, err := s.claimOnePendingInCampaign(ctx, workerID, cid, now); err != nil || ok {
			return work, ok, err
		}
	}

	// Phase 2: near-complete — throttle (every 32nd claim) to avoid SQLite lock storms.
	if s.claimRR.Load()%32 == 0 {
		nearIDs, nerr := s.nearCompleteCampaignIDs(ctx)
		if nerr == nil && len(nearIDs) > 0 && len(nearIDs) <= 8 {
			for _, cid := range nearIDs {
				if work, ok, err := s.claimOnePendingInCampaign(ctx, workerID, cid, now); err != nil || ok {
					return work, ok, err
				}
			}
		}
	}

	// Phase 3: reclaim expired leases (only when no customer pending above).
	{
		rows, err := s.DB.QueryContext(ctx, `
			SELECT id, campaign_id, input_n FROM fuzz_work_items
			 WHERE status='leased' AND lease_until < ?
			 ORDER BY lease_until ASC LIMIT 8`, now)
		if err != nil {
			return out, false, err
		}
		type exp struct {
			id, inputN uint64
			camp       string
		}
		var expired []exp
		for rows.Next() {
			var e exp
			var id int64
			if err := rows.Scan(&id, &e.camp, &e.inputN); err != nil {
				rows.Close()
				return out, false, err
			}
			e.id = uint64(id)
			expired = append(expired, e)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return out, false, err
		}
		for _, e := range expired {
			var title, ownerRef, cfgJSON string
			if err := s.DB.QueryRowContext(ctx,
				`SELECT title, COALESCE(owner_ref,''), config_json FROM fuzz_campaigns WHERE id=? AND status IN ('planned','running')`,
				e.camp).Scan(&title, &ownerRef, &cfgJSON); err != nil {
				continue
			}
			cfg := parseConfigJSON(cfgJSON)
			if !poolDistributed(cfg) || IsInternalGateCampaign(e.camp, title, ownerRef, cfg) {
				continue
			}
			leaseSec := leaseSecondsForConfig(cfg)
			res, err := s.DB.ExecContext(ctx,
				`UPDATE fuzz_work_items SET status='leased', lease_owner=?, lease_until=?, updated_at=?
				 WHERE id=? AND campaign_id=? AND status='leased' AND lease_until < ?`,
				workerID, now+leaseSec, now, int64(e.id), e.camp, now)
			if err != nil {
				return out, false, err
			}
			aff, _ := res.RowsAffected()
			if aff == 0 {
				continue
			}
			work, err := s.buildClaimedWork(ctx, e.camp, int64(e.id), e.inputN, cfg, workerID)
			if err != nil {
				return out, false, err
			}
			return work, true, nil
		}
	}

	// Phase 4: RR among other + bootstrap.
	if len(rest) == 0 {
		return out, false, nil
	}
	start := int(s.claimRR.Add(1) % uint64(len(rest)))
	for i := 0; i < len(rest); i++ {
		cid := rest[(start+i)%len(rest)]
		if work, ok, err := s.claimOnePendingInCampaign(ctx, workerID, cid, now); err != nil || ok {
			return work, ok, err
		}
	}
	return out, false, nil
}

// claimOnePendingInCampaign leases the oldest pending row in one campaign (index-friendly).
func (s *Service) claimOnePendingInCampaign(ctx context.Context, workerID, campaignID string, now int64) (ClaimedWork, bool, error) {
	var out ClaimedWork
	var itemID int64
	var inputN uint64
	var title, ownerRef, cfgJSON string
	err := s.DB.QueryRowContext(ctx, `
		SELECT w.id, w.input_n, c.title, COALESCE(c.owner_ref,''), c.config_json
		  FROM fuzz_work_items w
		  JOIN fuzz_campaigns c ON c.id = w.campaign_id
		 WHERE w.campaign_id = ?
		   AND w.status = 'pending'
		 ORDER BY w.id ASC
		 LIMIT 1`, campaignID).Scan(&itemID, &inputN, &title, &ownerRef, &cfgJSON)
	if err == sql.ErrNoRows {
		return out, false, nil
	}
	if err != nil {
		return out, false, err
	}
	cfg := parseConfigJSON(cfgJSON)
	if !poolDistributed(cfg) || IsInternalGateCampaign(campaignID, title, ownerRef, cfg) {
		return out, false, nil
	}
	leaseSec := leaseSecondsForConfig(cfg)
	res, err := s.DB.ExecContext(ctx,
		`UPDATE fuzz_work_items
		 SET status='leased', lease_owner=?, lease_until=?, updated_at=?
		 WHERE id=? AND campaign_id=? AND status='pending'`,
		workerID, now+leaseSec, now, itemID, campaignID)
	if err != nil {
		return out, false, err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return out, false, nil
	}
	work, err := s.buildClaimedWork(ctx, campaignID, itemID, inputN, cfg, workerID)
	if err != nil {
		return out, false, err
	}
	return work, true, nil
}

// runnablePoolCampaignIDs lists running/planned pool campaigns (no EXISTS scan — claim probes each).
func (s *Service) runnablePoolCampaignIDs(ctx context.Context, now int64) ([]string, error) {
	customers, rest, err := s.runnablePoolCampaignIDsByTier(ctx, now)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(customers)+len(rest))
	out = append(out, customers...)
	out = append(out, rest...)
	return out, nil
}

// runnablePoolCampaignIDsByTier splits customer vs other+bootstrap for strict order priority.
func (s *Service) runnablePoolCampaignIDsByTier(ctx context.Context, now int64) (customers, rest []string, err error) {
	_ = now
	rows, err := s.DB.QueryContext(ctx, `
		SELECT c.id, c.title, COALESCE(c.owner_ref,''), c.config_json
		  FROM fuzz_campaigns c
		 WHERE c.status IN ('planned','running')
		   AND lower(c.id) NOT LIKE '%probe%'
		   AND lower(c.id) NOT LIKE 'pool-sync-gate%'
		   AND lower(c.id) NOT LIKE 'pool-sync-node-%'
		   AND lower(c.id) NOT LIKE 'campaign-gate-%'
		   AND lower(c.id) NOT LIKE 'campaign-diag%'
		 ORDER BY c.created_at ASC`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	type row struct {
		id, title, owner, cfg string
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.title, &r.owner, &r.cfg); err != nil {
			return nil, nil, err
		}
		cfg := parseConfigJSON(r.cfg)
		if !poolDistributed(cfg) || IsInternalGateCampaign(r.id, r.title, r.owner, cfg) {
			continue
		}
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var other, bootstrap []string
	for _, r := range all {
		switch campaignClaimTier(r.id, r.title, r.owner) {
		case "customer":
			customers = append(customers, r.id)
		case "bootstrap":
			bootstrap = append(bootstrap, r.id)
		default:
			other = append(other, r.id)
		}
	}
	rest = append(rest, other...)
	rest = append(rest, bootstrap...)
	return customers, rest, nil
}

func campaignClaimTier(id, title, ownerRef string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	title = strings.ToLower(strings.TrimSpace(title))
	owner := strings.ToLower(strings.TrimSpace(ownerRef))
	if strings.Contains(id, "bootstrap") || strings.Contains(title, "bootstrap") || strings.HasPrefix(owner, "bootstrap:") {
		return "bootstrap"
	}
	if owner != "" &&
		!strings.HasPrefix(owner, "qa:") &&
		!strings.HasPrefix(owner, "e2e:") &&
		!strings.HasPrefix(owner, "fleet:") &&
		!strings.HasPrefix(owner, "diag:") &&
		!strings.HasPrefix(owner, "test:") &&
		!strings.HasPrefix(owner, "matrix:") {
		return "customer"
	}
	return "other"
}

// nearCompleteCampaignIDsFast — only campaigns already near done (cheap budget compare via summary).
func (s *Service) nearCompleteCampaignIDs(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT c.id, c.budget_runs, COALESCE(c.summary_json,'{}')
		  FROM fuzz_campaigns c
		 WHERE c.status IN ('planned','running')
		   AND c.budget_runs > 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id, sum string
		var budget int
		if err := rows.Scan(&id, &budget, &sum); err != nil {
			return nil, err
		}
		done := 0
		var m map[string]any
		if json.Unmarshal([]byte(sum), &m) == nil {
			switch v := m["runs_done"].(type) {
			case float64:
				done = int(v)
			case int:
				done = v
			}
		}
		if budget > 0 && done >= budget-32 {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

// Submit records a completed fuzz work item from a pool worker.
func (s *Service) Submit(ctx context.Context, req SubmitRequest) error {
	now := time.Now().Unix()
	cfgJSON := ""
	_ = s.DB.QueryRowContext(ctx, `SELECT config_json FROM fuzz_campaigns WHERE id=?`, req.CampaignID).Scan(&cfgJSON)
	cfg := parseConfigJSON(cfgJSON)
	isHunt := IsHuntCampaign(cfg)
	sem := fuzzengine.ParseCheckSemantics(cfg)
	hasWasm := !isHunt && wasmHexFromConfig(cfg) != ""

	var inputN uint64
	if err := s.DB.QueryRowContext(ctx,
		`SELECT input_n FROM fuzz_work_items WHERE id=? AND campaign_id=?`, req.ItemID, req.CampaignID).Scan(&inputN); err != nil {
		return err
	}
	expectedU, expectedB, err := s.expectedInputsForSubmit(ctx, req.CampaignID, req.ItemID, inputN, cfg)
	if err != nil {
		return err
	}
	if req.InputN != 0 && req.InputN != inputN {
		return fmt.Errorf("poolfuzz: input_n mismatch")
	}
	if req.ActualInput != expectedU {
		return fmt.Errorf("poolfuzz: actual_input mismatch")
	}
	if len(expectedB) > 0 {
		if len(req.InputBytes) != len(expectedB) || !bytes.Equal(req.InputBytes, expectedB) {
			return fmt.Errorf("poolfuzz: input_bytes mismatch")
		}
	} else if len(req.InputBytes) > 0 {
		return fmt.Errorf("poolfuzz: unexpected input_bytes")
	}
	maxB := fuzzengine.ParseMaxInputBytes(cfg)
	if len(expectedB) > maxB {
		return fmt.Errorf("poolfuzz: input_bytes exceed max_input_bytes")
	}
	execPer := PoolExecPerUnit(cfg)
	if isHunt {
		execPer = huntIterationsPerShard(cfg)
	}
	if execPer > 1 {
		if req.SegmentExecDone != execPer {
			return fmt.Errorf("poolfuzz: segment_exec_done mismatch want %d got %d", execPer, req.SegmentExecDone)
		}
	} else if req.SegmentExecDone > 0 && req.SegmentExecDone != 1 {
		return fmt.Errorf("poolfuzz: unexpected segment_exec_done for single-exec unit")
	}
	var seeds []fuzzengine.PoolCorpusSeed
	if (!isHunt && (fuzzengine.GuidedSchedulingEnabled(cfg) || execPer > 1)) || (isHunt && hunt.HuntCorpusGuided(cfg)) {
		var err error
		seeds, err = s.SeedsForWorkItem(ctx, req.CampaignID, req.ItemID, cfg)
		if err != nil {
			return err
		}
	}
	var checkResult int32
	var trap string
	var pass bool
	var recordFinding bool
	var findingU uint64
	var findingB []byte
	var seg fuzzengine.SegmentResult
	if isHunt {
		var err error
		var huntFindingB []byte
		checkResult, trap, pass, recordFinding, huntFindingB, err = s.evalHuntSubmitCheck(ctx, req.CampaignID, inputN, cfg, req, expectedB, seeds)
		if err != nil {
			return err
		}
		findingU = expectedU
		findingB = expectedB
		if recordFinding && len(huntFindingB) > 0 {
			findingB = huntFindingB
			findingU = fuzzengine.PackInputBytesToU64(findingB)
		}
	} else {
		var err error
		checkResult, trap, pass, recordFinding, findingU, findingB, seg, err = s.evalSubmitCheck(ctx, cfg, sem, inputN, expectedU, expectedB, seeds)
		if err != nil {
			return err
		}
		if hasWasm && execPer > 1 && seg.ExecDone != seg.ExecExpected {
			return fmt.Errorf("poolfuzz: incomplete segment replay %d/%d", seg.ExecDone, seg.ExecExpected)
		}
	}
	req.InputN = inputN
	req.ActualInput = expectedU
	req.InputBytes = expectedB
	req.CheckResult = checkResult
	req.Trap = trap

	workerID := strings.TrimSpace(req.WorkerID)
	miner := strings.TrimSpace(req.MinerAddress)
	wantRunSettle := s.Settler != nil && escrowEnabled(cfg) && miner != ""
	runSettleStatus := ""
	if wantRunSettle {
		runSettleStatus = "pending"
	}
	if workerID == "" {
		return fmt.Errorf("poolfuzz: worker_id required")
	}
	// H03: only the active lease owner may complete work (no pending/empty-owner harvest).
	res, err := s.DB.ExecContext(ctx,
		`UPDATE fuzz_work_items
		 SET status='done', attempts=attempts+1, result_ok=?, duration_ms=?, last_error=?, lease_owner='', lease_until=0, updated_at=?,
		     miner_address=CASE WHEN ?!='' THEN ? ELSE miner_address END,
		     settle_run_status=CASE WHEN ?!='' THEN ? ELSE settle_run_status END
		 WHERE id=? AND campaign_id=?
		   AND status='leased'
		   AND lease_owner=?`,
		boolToInt(pass), req.DurationMS, strings.TrimSpace(req.Trap), now,
		miner, miner, runSettleStatus, runSettleStatus,
		req.ItemID, req.CampaignID, workerID)
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		var st, owner string
		_ = s.DB.QueryRowContext(ctx,
			`SELECT status, COALESCE(lease_owner,'') FROM fuzz_work_items WHERE id=? AND campaign_id=?`,
			req.ItemID, req.CampaignID).Scan(&st, &owner)
		switch st {
		case "pending":
			return fmt.Errorf("poolfuzz: work item not leased (claim first)")
		case "leased":
			if owner != "" && owner != workerID {
				return fmt.Errorf("poolfuzz: work item leased by another worker")
			}
			return fmt.Errorf("poolfuzz: work item not leased by worker")
		case "done", "cancelled":
			// Already finished — still flush unsettled payment intents (PayRun may have failed earlier).
		default:
			if st == "" {
				return fmt.Errorf("poolfuzz: work item not found")
			}
		}
		if err := s.flushPendingSettles(ctx, req.CampaignID, req.ItemID, cfg); err != nil {
			return err
		}
		completed, err := s.recomputeProgress(ctx, req.CampaignID, now)
		if err != nil {
			return err
		}
		if completed && s.Settler != nil && escrowEnabled(cfg) {
			if _, err := s.Settler.Finalize(ctx, req.CampaignID, 0); err != nil {
				return fmt.Errorf("poolfuzz: finalize escrow: %w", err)
			}
		}
		return nil
	}
	if !isHunt {
		if len(seg.ExecCoverage) > 0 {
			if err := s.recordSegmentCoverage(ctx, req.CampaignID, inputN, cfg, seeds, seg, now); err != nil {
				return err
			}
		} else if err := s.recordCoverage(ctx, req.CampaignID, cfg, req.ActualInput, req.InputBytes, nil, now); err != nil {
			return err
		}
		if err := s.observePoolCorpus(ctx, req.CampaignID, req.ActualInput, req.InputBytes, recordFinding, now); err != nil {
			return err
		}
	} else {
		if err := s.recordCoverage(ctx, req.CampaignID, cfg, req.ActualInput, req.InputBytes, nil, now); err != nil {
			return err
		}
		if hunt.HuntCorpusGuided(cfg) {
			obsU, obsB := req.ActualInput, req.InputBytes
			if recordFinding && len(findingB) > 0 {
				obsU = findingU
				obsB = findingB
			}
			if err := s.observePoolCorpus(ctx, req.CampaignID, obsU, obsB, recordFinding, now); err != nil {
				return err
			}
		}
	}
	var findingSeverity string
	var findingType string
	var findingID string
	if recordFinding {
		submitReq := req
		submitReq.ActualInput = findingU
		if len(findingB) > 0 {
			submitReq.InputBytes = findingB
		} else {
			submitReq.InputBytes = expectedB
		}
		var err error
		findingID, findingSeverity, findingType, err = s.insertFinding(ctx, submitReq, cfg, sem, hasWasm, now)
		if err != nil {
			return err
		}
	}
	if wantRunSettle {
		if recordFinding && huntBountyEligible(cfg, findingSeverity) && s.bountyAllowed(ctx, cfg, findingID) {
			_, _ = s.DB.ExecContext(ctx,
				`UPDATE fuzz_work_items SET settle_finding_status='pending', settle_finding_severity=? WHERE id=? AND campaign_id=?`,
				findingSeverity, req.ItemID, req.CampaignID)
		}
		if err := s.flushPendingSettles(ctx, req.CampaignID, req.ItemID, cfg); err != nil {
			return err
		}
		// One-shot unique-crash micro-bonus (does not close the confirmed-native bounty).
		// Crash-class only — detector/property noise must not skim the bonus pool.
		// workItemID=0 so outbox dedupes campaign-wide (first crash wins).
		if recordFinding && miner != "" && fuzzengine.IsCrashClass(findingType) {
			if _, err := s.Settler.PayCrashBonus(ctx, req.CampaignID, miner, 0, 0); err != nil {
				// already paid / depleted / closed are non-fatal for the submit path
				low := strings.ToLower(err.Error())
				if !strings.Contains(low, "already paid") && !strings.Contains(low, "depleted") && !strings.Contains(low, "closed") {
					return fmt.Errorf("poolfuzz: settle crash bonus: %w", err)
				}
			}
		}
	}
	completed, err := s.recomputeProgress(ctx, req.CampaignID, now)
	if err != nil {
		return err
	}
	if completed && s.Settler != nil && escrowEnabled(cfg) {
		if _, err := s.Settler.Finalize(ctx, req.CampaignID, 0); err != nil {
			return fmt.Errorf("poolfuzz: finalize escrow: %w", err)
		}
	}
	return nil
}

// flushPendingSettles pays unsettled run/finding intents exactly once (idempotent status transitions).
// Local status stays queued until origin ACK / durable applied confirmation — never mark paid on enqueue-only.
func (s *Service) flushPendingSettles(ctx context.Context, campaignID string, itemID int64, cfg map[string]any) error {
	if s == nil || s.DB == nil || s.Settler == nil || !escrowEnabled(cfg) {
		return nil
	}
	var miner, runSt, findSt, findSev string
	var runOutbox, findOutbox int64
	err := s.DB.QueryRowContext(ctx,
		`SELECT COALESCE(miner_address,''), COALESCE(settle_run_status,''), COALESCE(settle_finding_status,''), COALESCE(settle_finding_severity,''),
		        COALESCE(settle_run_outbox_id,0), COALESCE(settle_finding_outbox_id,0)
		 FROM fuzz_work_items WHERE campaign_id=? AND id=?`, campaignID, itemID).Scan(&miner, &runSt, &findSt, &findSev, &runOutbox, &findOutbox)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	miner = strings.TrimSpace(miner)
	if miner == "" {
		return nil
	}
	runSt = strings.TrimSpace(strings.ToLower(runSt))
	findSt = strings.TrimSpace(strings.ToLower(findSt))
	if runSt == "pending" || runSt == "queued" {
		res, err := s.Settler.PayRun(ctx, campaignID, miner, itemID, runOutbox)
		if err != nil {
			return fmt.Errorf("poolfuzz: settle run: %w", err)
		}
		next := "queued"
		if res.Applied {
			next = "paid"
		}
		if _, err := s.DB.ExecContext(ctx,
			`UPDATE fuzz_work_items SET settle_run_status=?, settle_run_outbox_id=?
			 WHERE campaign_id=? AND id=? AND settle_run_status IN ('pending','queued')`,
			next, res.OutboxID, campaignID, itemID); err != nil {
			return err
		}
	}
	if findSt == "pending" || findSt == "queued" {
		sev := strings.TrimSpace(findSev)
		if huntBountyEligible(cfg, sev) {
			res, err := s.Settler.PayFinding(ctx, campaignID, miner, sev, itemID, findOutbox)
			if err != nil {
				return fmt.Errorf("poolfuzz: settle finding: %w", err)
			}
			next := "queued"
			if res.Applied {
				next = "paid"
			}
			if _, err := s.DB.ExecContext(ctx,
				`UPDATE fuzz_work_items SET settle_finding_status=?, settle_finding_outbox_id=?
				 WHERE campaign_id=? AND id=? AND settle_finding_status IN ('pending','queued')`,
				next, res.OutboxID, campaignID, itemID); err != nil {
				return err
			}
		} else if _, err := s.DB.ExecContext(ctx,
			`UPDATE fuzz_work_items SET settle_finding_status='paid' WHERE campaign_id=? AND id=? AND settle_finding_status IN ('pending','queued')`,
			campaignID, itemID); err != nil {
			return err
		}
	}
	return nil
}

func bountySeverity(sev string) bool {
	switch strings.TrimSpace(strings.ToLower(sev)) {
	case "medium", "high", "critical":
		return true
	default:
		return false
	}
}

func (s *Service) evalSubmitCheck(ctx context.Context, cfg map[string]any, sem fuzzengine.CheckSemantics, inputN, inputU uint64, inputB []byte, seeds []fuzzengine.PoolCorpusSeed) (checkResult int32, trap string, pass bool, recordFinding bool, findingU uint64, findingB []byte, seg fuzzengine.SegmentResult, err error) {
	wasmHex := wasmHexFromConfig(cfg)
	if wasmHex == "" {
		return 0, "", true, false, inputU, inputB, seg, nil
	}
	wasm, err := hex.DecodeString(wasmHex)
	if err != nil || len(wasm) == 0 {
		return 0, "", false, false, 0, nil, seg, fmt.Errorf("poolfuzz: invalid campaign wasm")
	}
	if vErr := sandbox.ValidateCheckWasm(ctx, wasm); vErr != nil {
		return 0, "", false, false, 0, nil, seg, fmt.Errorf("poolfuzz: invalid campaign wasm: %w", vErr)
	}
	timeoutMS := sandbox.Policy().CheckTimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = 5000
	}
	runOne := func(runCtx context.Context, inU uint64, inB []byte) (int32, string, error, []byte) {
		inB = fuzzengine.ClampInputBytes(inB, cfg)
		if len(inB) > 0 {
			out, execErr := sandbox.InvokeCheckOutcomeInput(runCtx, wasm, inB)
			if execErr != nil {
				return 0, execErr.Error(), execErr, nil
			}
			if out.OK {
				return 1, "", nil, out.EdgeBitmap
			}
			return 0, "", nil, out.EdgeBitmap
		}
		out, execErr := sandbox.InvokeCheckOutcome(runCtx, wasm, inU)
		if execErr != nil {
			return 0, execErr.Error(), execErr, nil
		}
		if out.OK {
			return 1, "", nil, out.EdgeBitmap
		}
		return 0, "", nil, out.EdgeBitmap
	}
	replayCfg := poolReplayConfig(cfg)
	execPer := PoolExecPerUnit(cfg)
	if execPer <= 1 {
		runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
		defer cancel()
		checkResult, trap, execErr, edgeBitmap := runOne(runCtx, inputU, inputB)
		if execErr != nil {
			pass, recordFinding = fuzzengine.EvalCheck(sem, 0, execErr)
			return 0, trap, pass, recordFinding, inputU, inputB, seg, nil
		}
		pass, recordFinding = fuzzengine.EvalCheck(sem, checkResult, nil)
		edge, path := fuzzengine.CoverageBucketsForExec(cfg, inputU, inputB, edgeBitmap)
		seg.ExecCoverage = []fuzzengine.CoverageSample{{Edge: edge, Path: path}}
		if recordFinding {
			return checkResult, trap, pass, true, inputU, inputB, seg, nil
		}
		return checkResult, trap, pass, false, inputU, inputB, seg, nil
	}
	segCtx, cancel := context.WithTimeout(ctx, time.Duration(int(timeoutMS)*execPer)*time.Millisecond)
	defer cancel()
	seg = fuzzengine.EvalSegment(segCtx, inputN, replayCfg, seeds, sem, runOne)
	findingU = inputU
	findingB = inputB
	if seg.RecordFinding {
		findingU = seg.FindingInputU
		findingB = seg.FindingInputB
	}
	return seg.CheckResult, seg.Trap, seg.Pass, seg.RecordFinding, findingU, findingB, seg, nil
}

func (s *Service) recordSegmentCoverage(ctx context.Context, campaignID string, inputN uint64, cfg map[string]any, seeds []fuzzengine.PoolCorpusSeed, seg fuzzengine.SegmentResult, now int64) error {
	if len(seg.ExecCoverage) > 0 {
		for _, c := range seg.ExecCoverage {
			if err := s.recordCoverageBuckets(ctx, campaignID, c.Edge, c.Path, now); err != nil {
				return err
			}
		}
		return nil
	}
	execPer := fuzzengine.ExecPerUnit(cfg)
	for execIdx := uint64(0); execIdx < uint64(execPer); execIdx++ {
		inU, inB := fuzzengine.SegmentExecInput(inputN, execIdx, cfg, seeds)
		if err := s.recordCoverage(ctx, campaignID, cfg, inU, inB, nil, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) recordCoverageBuckets(ctx context.Context, campaignID string, edge, path int, now int64) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT OR IGNORE INTO fuzz_coverage_seen (campaign_id, kind, bucket, first_seen_at) VALUES (?, 'edge', ?, ?)`,
		campaignID, edge, now)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx,
		`INSERT OR IGNORE INTO fuzz_coverage_seen (campaign_id, kind, bucket, first_seen_at) VALUES (?, 'path', ?, ?)`,
		campaignID, path, now)
	return err
}

func (s *Service) recordCoverage(ctx context.Context, campaignID string, cfg map[string]any, input uint64, inputBytes []byte, edgeBitmap []byte, now int64) error {
	edge, path := fuzzengine.CoverageBucketsForExec(cfg, input, inputBytes, edgeBitmap)
	return s.recordCoverageBuckets(ctx, campaignID, edge, path, now)
}

// ExecuteLocally runs sandbox check on coordinator (used by tests); workers normally submit results.
func ExecuteLocally(ctx context.Context, wasmHex string, input uint64, timeoutMS int) (checkResult int32, durationMS int, trap string, err error) {
	start := time.Now()
	wasm, err := hex.DecodeString(strings.TrimSpace(wasmHex))
	if err != nil || len(wasm) == 0 {
		return 0, 0, "", fmt.Errorf("invalid wasm")
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()
	ok, execErr := sandbox.InvokeCheck(runCtx, wasm, input)
	durationMS = int(time.Since(start).Milliseconds())
	if execErr != nil {
		return 0, durationMS, execErr.Error(), nil
	}
	if ok {
		return 1, durationMS, "", nil
	}
	return 0, durationMS, "", nil
}

// ExecuteLocallyBytes runs sandbox check with byte input (P4 pool tests).
func ExecuteLocallyBytes(ctx context.Context, wasmHex string, input []byte, timeoutMS int) (checkResult int32, durationMS int, trap string, err error) {
	start := time.Now()
	wasm, err := hex.DecodeString(strings.TrimSpace(wasmHex))
	if err != nil || len(wasm) == 0 {
		return 0, 0, "", fmt.Errorf("invalid wasm")
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()
	ok, execErr := sandbox.InvokeCheckInput(runCtx, wasm, input)
	durationMS = int(time.Since(start).Milliseconds())
	if execErr != nil {
		return 0, durationMS, execErr.Error(), nil
	}
	if ok {
		return 1, durationMS, "", nil
	}
	return 0, durationMS, "", nil
}

func (s *Service) bountyAllowed(ctx context.Context, cfg map[string]any, findingID string) bool {
	if !fuzzengine.BountyRequiresNative(cfg) {
		return true
	}
	if findingID == "" || s.DB == nil {
		return false
	}
	ok, err := fuzznative.IsFindingNativeEligibleForBounty(ctx, s.DB, findingID)
	return err == nil && ok
}

func (s *Service) insertFinding(ctx context.Context, req SubmitRequest, cfg map[string]any, sem fuzzengine.CheckSemantics, hasWasm bool, now int64) (findingID, severity, findingType string, err error) {
	inputBytes := req.InputBytes
	var inputSHA string
	var artifactPath string
	var repro string
	if len(inputBytes) > 0 {
		inputSHA = fuzzengine.InputBytesSHA256(inputBytes)
		artifactPath = fuzzartifacts.WriteInputBytes(req.CampaignID, inputSHA, inputBytes)
		if IsHuntCampaign(cfg) {
			repro = fuzzupstream.ReproCmdHuntNative(inputBytes)
		} else {
			wasmHex, _ := cfg["wasm_check_hex"].(string)
			wasmPath := fuzzartifacts.WriteWasmHex(req.CampaignID, wasmHex)
			repro = fuzzengine.ReproCmdBytes(wasmPath, inputBytes)
		}
	} else {
		inputSHA = fuzzengine.InputSHA256(req.ActualInput)
		wasmHex, _ := cfg["wasm_check_hex"].(string)
		wasmPath := fuzzartifacts.WriteWasmHex(req.CampaignID, wasmHex)
		artifactPath = fuzzartifacts.WriteInput(req.CampaignID, inputSHA, req.ActualInput)
		repro = fuzzengine.ReproCmdTool(wasmPath, req.ActualInput)
	}
	ft, sev, title := fuzzengine.ClassifyCheckFail(req.ActualInput, hasWasm, sem)
	if IsHuntCampaign(cfg) {
		ft, sev, title = classifyHuntFinding(cfg, req)
	} else if strings.TrimSpace(req.Trap) != "" {
		ft, sev, title = fuzzengine.ClassifyWasmTrap(req.ActualInput, req.Trap, hasWasm)
	}
	if len(inputBytes) > 0 && sem == fuzzengine.SemanticsDetector && strings.TrimSpace(req.Trap) == "" {
		title = fuzzengine.BytesDetectorTitle(inputBytes)
	}
	findingType = ft
	severity = sev
	findingID = fmt.Sprintf("finding-pool-%s-%d-%d", req.CampaignID, req.ItemID, now)
	op, itemID, qty := fuzzengine.WasmCheckInputParts(req.ActualInput)
	wasmHex, _ := cfg["wasm_check_hex"].(string)
	_ = fuzzartifacts.WriteWasmHex(req.CampaignID, wasmHex)
	triage := fuzzengine.ClassifyFinding(ft, sev)
	detailMap := map[string]any{
		"source":          "pool_fuzz_worker_v2",
		"worker_id":       req.WorkerID,
		"miner_address":   strings.TrimSpace(req.MinerAddress),
		"input_n":         req.InputN,
		"actual_input":    req.ActualInput,
		"input_mode":      fuzzengine.ParseInputMode(cfg),
		"input_len":       len(inputBytes),
		"check_result":    req.CheckResult,
		"op_type":         op,
		"item_id":         itemID,
		"quantity":        qty,
		"trap":            req.Trap,
		"check_semantics": string(sem),
		"triage_class":    triage.Class,
		"triage_label":    triage.Label,
		"zero_day_hint":   triage.ZeroDayHint,
	}
	if IsHuntCampaign(cfg) {
		if info, ok := fuzzupstream.ParseHuntTrap(strings.TrimSpace(req.Trap)); ok {
			detailMap["sanitizer_class"] = info.Class
			detailMap["sanitizer_subtype"] = info.Subtype
			detailMap["sanitizer_label"] = info.Label
		}
	}
	if len(inputBytes) > 0 {
		detailMap["input_hex"] = hex.EncodeToString(inputBytes)
		detailMap["input_len"] = len(inputBytes)
		if IsHuntCampaign(cfg) {
			detailMap["hunt_trimmed"] = true
			detailMap["repro_kind"] = "hunt_native"
		}
		if gp := strings.TrimSpace(jsonString(cfg["guard_pack"])); gp != "" {
			detailMap["guard_pack"] = gp
			preview := string(inputBytes)
			detailMap["explain"] = fuzzingcli.ExplainPackFinding(gp, preview, title)
		}
	} else if gp := strings.TrimSpace(jsonString(cfg["guard_pack"])); gp != "" {
		detailMap["guard_pack"] = gp
		detailMap["explain"] = fuzzingcli.ExplainPackFinding(gp, title, title)
	}
	detail, _ := json.Marshal(detailMap)
	_, err = s.DB.ExecContext(ctx,
		`INSERT OR IGNORE INTO fuzz_findings
		 (id, campaign_id, finding_type, severity, title, input_sha256, artifact_path, repro_cmd, detail_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		findingID, req.CampaignID, ft, sev, title, inputSHA, artifactPath, repro, string(detail), now)
	if err != nil {
		return "", "", "", err
	}
	_, _ = s.DB.ExecContext(ctx,
		`INSERT INTO fuzz_corpus (campaign_id, input_sha256, first_seen_at, last_seen_at, hits, last_finding_id, artifact_path)
		 VALUES (?, ?, ?, ?, 1, ?, ?)
		 ON CONFLICT(campaign_id, input_sha256) DO UPDATE SET
		   last_seen_at=excluded.last_seen_at,
		   hits=fuzz_corpus.hits+1,
		   last_finding_id=excluded.last_finding_id,
		   artifact_path=CASE WHEN excluded.artifact_path<>'' THEN excluded.artifact_path ELSE fuzz_corpus.artifact_path END`,
		req.CampaignID, inputSHA, now, now, findingID, artifactPath)
	if fuzzengine.NativeReproEnabled(cfg) {
		guard := strings.TrimSpace(jsonString(cfg["guard_name"]))
		if guard == "" {
			guard = strings.TrimSpace(jsonString(cfg["upstream_guard"]))
		}
		upstream := fuzzengine.UpstreamTarget(cfg)
		if IsHuntCampaign(cfg) {
			upstream = strings.TrimSpace(jsonString(cfg["upstream_target_id"]))
			if guard == "" {
				guard = upstream
			}
		}
		ib := inputBytes
		if len(ib) == 0 {
			ib = make([]byte, 8)
			for i := 0; i < 8; i++ {
				ib[i] = byte(req.ActualInput >> (8 * i))
			}
		}
		if IsHuntCampaign(cfg) && ft == "native_crash" {
			_ = fuzznative.QueueJobVerified(ctx, s.DB, findingID, req.CampaignID, inputSHA, ib, upstream, guard, fuzznative.StatusNativeCrash, now)
		} else {
			_ = fuzznative.QueueJob(ctx, s.DB, findingID, req.CampaignID, inputSHA, ib, upstream, guard, now)
		}
	}
	return findingID, severity, findingType, nil
}

func (s *Service) recomputeProgress(ctx context.Context, campaignID string, now int64) (completed bool, err error) {
	var done, crashed, sumDuration int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT
		   COALESCE(SUM(CASE WHEN status='done' THEN 1 ELSE 0 END),0),
		   COALESCE(SUM(CASE WHEN status='done' AND result_ok=0 THEN 1 ELSE 0 END),0),
		   COALESCE(SUM(CASE WHEN status='done' THEN duration_ms ELSE 0 END),0)
		 FROM fuzz_work_items WHERE campaign_id=?`, campaignID).Scan(&done, &crashed, &sumDuration); err != nil {
		return false, err
	}
	var edges, paths int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_coverage_seen WHERE campaign_id=? AND kind='edge'`, campaignID).Scan(&edges)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_coverage_seen WHERE campaign_id=? AND kind='path'`, campaignID).Scan(&paths)
	var budgetRuns, budgetSeconds int
	var startedAt int64
	var status, summaryJSON, cfgJSON string
	if err := s.DB.QueryRowContext(ctx,
		`SELECT budget_runs, budget_seconds, started_at, status, summary_json, config_json FROM fuzz_campaigns WHERE id=?`,
		campaignID).Scan(&budgetRuns, &budgetSeconds, &startedAt, &status, &summaryJSON, &cfgJSON); err != nil {
		return false, err
	}
	cfg := parseConfigJSON(cfgJSON)
	summary := parseConfigJSON(summaryJSON)
	summary["fuzz_engine"] = fuzzengine.MetaFromConfig(cfg)
	summary["runs_done"] = done
	summary["new_edges"] = edges
	summary["new_paths"] = paths
	// failed_checks = work items with result_ok=0 (includes detector rejects).
	// unique_crashes = crash-class findings only (honest customer metric).
	summary["failed_checks"] = crashed
	crashClass := 0
	if frows, err := s.DB.QueryContext(ctx, `SELECT finding_type FROM fuzz_findings WHERE campaign_id=?`, campaignID); err == nil {
		for frows.Next() {
			var ft string
			if err := frows.Scan(&ft); err == nil && fuzzengine.IsCrashClass(ft) {
				crashClass++
			}
		}
		_ = frows.Close()
	}
	summary["unique_crashes"] = crashClass
	summary["crash_count"] = crashClass
	summary["heartbeat_at"] = now
	summary["pool_workers"] = true
	if done > 0 {
		summary["avg_duration_ms"] = sumDuration / done
	}
	nextStatus := strings.TrimSpace(strings.ToLower(status))
	completedAt := int64(0)
	if budgetRuns > 0 && done >= budgetRuns {
		nextStatus = "completed"
		completedAt = now
	}
	if budgetSeconds > 0 && startedAt > 0 && now-startedAt >= int64(budgetSeconds) {
		nextStatus = "completed"
		completedAt = now
	}
	_, err = s.DB.ExecContext(ctx,
		`UPDATE fuzz_campaigns
		 SET status=?, summary_json=?, completed_at=CASE WHEN ?='completed' AND completed_at=0 THEN ? ELSE completed_at END
		 WHERE id=?`,
		nextStatus, marshalSummaryJSON(summary), nextStatus, completedAt, campaignID)
	return nextStatus == "completed", err
}

// PoolStats returns aggregate stats for public/coordinator metrics.
func (s *Service) PoolStats(ctx context.Context) (map[string]any, error) {
	var campaigns, running, workPending, workDone int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_campaigns`).Scan(&campaigns)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_campaigns WHERE status='running'`).Scan(&running)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_work_items WHERE status='pending'`).Scan(&workPending)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_work_items WHERE status='done'`).Scan(&workDone)
	return map[string]any{
		"ok":                true,
		"pool_fuzz":         true,
		"campaigns_total":   campaigns,
		"campaigns_running": running,
		"work_pending":      workPending,
		"work_done":         workDone,
	}, nil
}

// CampaignProgress returns live pool progress for one campaign (public read).
func (s *Service) CampaignProgress(ctx context.Context, campaignID string) (map[string]any, error) {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return nil, fmt.Errorf("poolfuzz: campaign id required")
	}
	var status, title, summaryJSON string
	var budgetRuns int
	var completedAt int64
	err := s.DB.QueryRowContext(ctx,
		`SELECT status, title, budget_runs, summary_json, completed_at FROM fuzz_campaigns WHERE id=?`,
		campaignID).Scan(&status, &title, &budgetRuns, &summaryJSON, &completedAt)
	if err == sql.ErrNoRows {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	summary := parseConfigJSON(summaryJSON)
	runsDone := runsDoneForCampaign(ctx, s.DB, campaignID, summary)
	var findings int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, campaignID).Scan(&findings)
	displayStatus := status
	if budgetRuns > 0 && runsDone >= budgetRuns && displayStatus == "running" {
		displayStatus = "completed"
	}
	return map[string]any{
		"ok":           true,
		"id":           campaignID,
		"title":        title,
		"status":       displayStatus,
		"budget_runs":  budgetRuns,
		"runs_done":    runsDone,
		"findings":     findings,
		"completed_at": completedAt,
	}, nil
}

func derivePoolInputs(inputN uint64, cfg map[string]any) (uint64, []byte) {
	if fuzzengine.ParseInputMode(cfg) == fuzzengine.InputModeBytes {
		b := fuzzengine.DeriveInputBytes(inputN, cfg)
		return fuzzengine.PackInputBytesToU64(b), b
	}
	seeds := fuzzengine.ParseSeedCorpus(cfg)
	if fuzzengine.MutationRounds(cfg) == 0 && len(seeds) > 0 {
		u := seeds[inputN%uint64(len(seeds))]
		return u, nil
	}
	u := fuzzengine.DeriveInput(inputN, cfg)
	return u, nil
}

func derivePoolInput(inputN uint64, cfg map[string]any) uint64 {
	u, _ := derivePoolInputs(inputN, cfg)
	return u
}

func perRunHMCFromConfig(cfg map[string]any) float64 {
	if IsHuntCampaign(cfg) {
		return perShardHMCFromConfig(cfg)
	}
	if cfg == nil {
		return 0
	}
	budget := floatFromJSON(cfg["budget_hmc"])
	runs := intFromJSON(cfg["budget_runs"])
	if budget <= 0 || runs < 8 {
		return 0
	}
	share := 0.20
	if strings.TrimSpace(jsonString(cfg["escrow_split"])) == "50_50" {
		share = 0.50
	}
	return (budget * share) / float64(runs)
}

func wasmHexFromConfig(cfg map[string]any) string {
	return strings.TrimSpace(jsonString(cfg["wasm_check_hex"]))
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func intFromJSON(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	default:
		return 0
	}
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n]
}
