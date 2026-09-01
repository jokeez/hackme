package poolfuzz

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/hunt"
)

const (
	huntReplayStatusPending    = "pending"
	huntReplayStatusProcessing = "processing"
	huntReplayStatusDone       = "done"
	huntReplayStatusFailed     = "failed"
	workStatusReplayPending    = "replay_pending"
)

type huntReplayJob struct {
	ID                int64
	CampaignID        string
	ItemID            int64
	WorkerID          string
	MinerAddress      string
	InputN            uint64
	WorkerCheckResult int32
	WorkerTrap        string
	SegmentExecDone   int
	DurationMS        int
}

var huntReplayWorkersOnce sync.Once

func huntReplayAsyncEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_POOL_HUNT_REPLAY_ASYNC")))
	if v == "" {
		return true
	}
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func huntReplayWorkerCount() int {
	v := strings.TrimSpace(os.Getenv("HACKME_POOL_HUNT_REPLAY_WORKERS"))
	if v == "" {
		return 4
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 4
	}
	if n > 32 {
		return 32
	}
	return n
}

func huntVerifierID() string {
	if v := strings.TrimSpace(os.Getenv("HACKME_HUNT_VERIFIER_ID")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("HOSTNAME")); v != "" {
		return v
	}
	return "coordinator"
}

// StartHuntReplayWorkers launches background Hunt ASAN replay consumers (coordinator or verifier node).
func StartHuntReplayWorkers(ctx context.Context, s *Service) {
	if s == nil || s.DB == nil || !huntReplayAsyncEnabled() {
		return
	}
	huntReplayWorkersOnce.Do(func() {
		n := huntReplayWorkerCount()
		vid := huntVerifierID()
		for i := 0; i < n; i++ {
			go huntReplayWorkerLoop(ctx, s, fmt.Sprintf("%s-%d", vid, i+1))
		}
	})
}

func huntReplayWorkerLoop(ctx context.Context, s *Service, workerLabel string) {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for {
				ok, err := s.processNextHuntReplayJob(ctx, workerLabel)
				if err != nil || !ok {
					break
				}
			}
		}
	}
}

// DrainHuntReplayQueue processes all pending Hunt replay jobs (tests / gate drain).
func (s *Service) DrainHuntReplayQueue(ctx context.Context) error {
	if s == nil || s.DB == nil {
		return nil
	}
	for {
		ok, err := s.processNextHuntReplayJob(ctx, "drain")
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
	}
}

// HuntReplayQueueStats returns queue depth metrics for coordinator dashboards.
func (s *Service) HuntReplayQueueStats(ctx context.Context) (pending, processing, failed int, err error) {
	if s == nil || s.DB == nil {
		return 0, 0, 0, nil
	}
	err = s.DB.QueryRowContext(ctx,
		`SELECT
		 COALESCE(SUM(CASE WHEN status='pending' THEN 1 ELSE 0 END),0),
		 COALESCE(SUM(CASE WHEN status='processing' THEN 1 ELSE 0 END),0),
		 COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0)
		 FROM fuzz_hunt_replay_queue`).Scan(&pending, &processing, &failed)
	return pending, processing, failed, err
}

// HuntReplayStatus returns async replay state for one work item.
func (s *Service) HuntReplayStatus(ctx context.Context, campaignID string, itemID int64) (map[string]any, error) {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" || itemID <= 0 {
		return nil, fmt.Errorf("poolfuzz: campaign_id and item_id required")
	}
	var wiStatus string
	var resultOK int
	var lastErr string
	err := s.DB.QueryRowContext(ctx,
		`SELECT status, result_ok, COALESCE(last_error,'') FROM fuzz_work_items WHERE campaign_id=? AND id=?`,
		campaignID, itemID).Scan(&wiStatus, &resultOK, &lastErr)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("poolfuzz: work item not found")
	}
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"ok":           true,
		"campaign_id":  campaignID,
		"item_id":      itemID,
		"work_status":  wiStatus,
		"result_ok":    resultOK != 0,
		"replay_async": wiStatus == workStatusReplayPending,
	}
	var qStatus, qErr string
	var qID int64
	qErrRow := s.DB.QueryRowContext(ctx,
		`SELECT id, status, COALESCE(last_error,'') FROM fuzz_hunt_replay_queue WHERE campaign_id=? AND item_id=?`,
		campaignID, itemID).Scan(&qID, &qStatus, &qErr)
	if qErrRow == nil {
		out["queue_id"] = qID
		out["replay_status"] = qStatus
		out["queue_error"] = qErr
	} else if wiStatus == "done" {
		out["replay_status"] = huntReplayStatusDone
	}
	return out, nil
}

func (s *Service) enqueueHuntReplay(ctx context.Context, req SubmitRequest, inputN uint64, now int64) (SubmitOutcome, error) {
	miner := strings.TrimSpace(req.MinerAddress)
	res, err := s.DB.ExecContext(ctx,
		`UPDATE fuzz_work_items
		 SET status=?, miner_address=CASE WHEN ?!='' THEN ? ELSE miner_address END,
		     duration_ms=?, updated_at=?, lease_owner='', lease_until=0
		 WHERE id=? AND campaign_id=? AND status='leased' AND lease_owner=?`,
		workStatusReplayPending, miner, miner, req.DurationMS, now,
		req.ItemID, req.CampaignID, req.WorkerID)
	if err != nil {
		return SubmitOutcome{}, err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		var st string
		_ = s.DB.QueryRowContext(ctx,
			`SELECT status FROM fuzz_work_items WHERE id=? AND campaign_id=?`,
			req.ItemID, req.CampaignID).Scan(&st)
		switch st {
		case workStatusReplayPending:
			var qid int64
			_ = s.DB.QueryRowContext(ctx,
				`SELECT id FROM fuzz_hunt_replay_queue WHERE campaign_id=? AND item_id=?`,
				req.CampaignID, req.ItemID).Scan(&qid)
			return SubmitOutcome{Async: true, ReplayStatus: huntReplayStatusPending, QueueID: qid}, nil
		case "done", "cancelled":
			return SubmitOutcome{ReplayStatus: huntReplayStatusDone}, nil
		default:
			return SubmitOutcome{}, fmt.Errorf("poolfuzz: work item not leased by worker")
		}
	}
	res2, err := s.DB.ExecContext(ctx,
		`INSERT INTO fuzz_hunt_replay_queue
		 (campaign_id, item_id, worker_id, miner_address, input_n, worker_check_result, worker_trap, segment_exec_done, duration_ms, status, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(campaign_id, item_id) DO UPDATE SET
		   worker_id=excluded.worker_id,
		   miner_address=excluded.miner_address,
		   worker_check_result=excluded.worker_check_result,
		   worker_trap=excluded.worker_trap,
		   segment_exec_done=excluded.segment_exec_done,
		   duration_ms=excluded.duration_ms,
		   status='pending',
		   last_error='',
		   updated_at=excluded.updated_at`,
		req.CampaignID, req.ItemID, req.WorkerID, miner, inputN,
		req.CheckResult, strings.TrimSpace(req.Trap), req.SegmentExecDone, req.DurationMS,
		huntReplayStatusPending, now, now)
	if err != nil {
		return SubmitOutcome{}, err
	}
	qid, _ := res2.LastInsertId()
	return SubmitOutcome{Async: true, ReplayStatus: huntReplayStatusPending, QueueID: qid}, nil
}

func (s *Service) processNextHuntReplayJob(ctx context.Context, verifierID string) (bool, error) {
	now := time.Now().Unix()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var job huntReplayJob
	err = tx.QueryRowContext(ctx,
		`SELECT id, campaign_id, item_id, worker_id, miner_address, input_n, worker_check_result, worker_trap, segment_exec_done, duration_ms
		 FROM fuzz_hunt_replay_queue
		 WHERE status=?
		 ORDER BY created_at ASC, id ASC
		 LIMIT 1`, huntReplayStatusPending).Scan(
		&job.ID, &job.CampaignID, &job.ItemID, &job.WorkerID, &job.MinerAddress, &job.InputN,
		&job.WorkerCheckResult, &job.WorkerTrap, &job.SegmentExecDone, &job.DurationMS)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE fuzz_hunt_replay_queue SET status=?, verifier_id=?, updated_at=? WHERE id=? AND status=?`,
		huntReplayStatusProcessing, verifierID, now, job.ID, huntReplayStatusPending)
	if err != nil {
		return false, err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return false, nil
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}

	procErr := s.runHuntReplayJob(ctx, job, now)
	if procErr != nil {
		_, _ = s.DB.ExecContext(ctx,
			`UPDATE fuzz_hunt_replay_queue SET status=?, last_error=?, updated_at=? WHERE id=?`,
			huntReplayStatusFailed, procErr.Error(), time.Now().Unix(), job.ID)
		_, _ = s.DB.ExecContext(ctx,
			`UPDATE fuzz_work_items SET status='done', result_ok=0, last_error=?, updated_at=? WHERE campaign_id=? AND id=? AND status=?`,
			procErr.Error(), time.Now().Unix(), job.CampaignID, job.ItemID, workStatusReplayPending)
		return true, nil
	}
	_, _ = s.DB.ExecContext(ctx,
		`UPDATE fuzz_hunt_replay_queue SET status=?, updated_at=? WHERE id=?`,
		huntReplayStatusDone, time.Now().Unix(), job.ID)
	return true, nil
}

func (s *Service) runHuntReplayJob(ctx context.Context, job huntReplayJob, now int64) error {
	cfgJSON := ""
	if err := s.DB.QueryRowContext(ctx, `SELECT config_json FROM fuzz_campaigns WHERE id=?`, job.CampaignID).Scan(&cfgJSON); err != nil {
		return err
	}
	cfg := parseConfigJSON(cfgJSON)
	if !IsHuntCampaign(cfg) {
		return fmt.Errorf("poolfuzz: replay job not hunt campaign")
	}
	expectedU, expectedB, err := s.expectedInputsForSubmit(ctx, job.CampaignID, job.ItemID, job.InputN, cfg)
	if err != nil {
		return err
	}
	var seeds []fuzzengine.PoolCorpusSeed
	if hunt.HuntCorpusGuided(cfg) {
		seeds, err = s.SeedsForWorkItem(ctx, job.CampaignID, job.ItemID, cfg)
		if err != nil {
			return err
		}
	}
	req := SubmitRequest{
		WorkerID:        job.WorkerID,
		MinerAddress:    job.MinerAddress,
		CampaignID:      job.CampaignID,
		ItemID:          job.ItemID,
		InputN:          job.InputN,
		ActualInput:     expectedU,
		InputBytes:      expectedB,
		CheckResult:     job.WorkerCheckResult,
		Trap:            job.WorkerTrap,
		SegmentExecDone: job.SegmentExecDone,
		DurationMS:      job.DurationMS,
	}
	checkResult, trap, pass, recordFinding, huntFindingB, huntOrigLen, err := s.evalHuntSubmitCheck(ctx, job.CampaignID, job.InputN, cfg, req, expectedB, seeds)
	if err != nil {
		return err
	}
	findingU := expectedU
	findingB := expectedB
	if recordFinding && len(huntFindingB) > 0 {
		findingB = huntFindingB
		findingU = fuzzengine.PackInputBytesToU64(findingB)
	}
	req.CheckResult = checkResult
	req.Trap = trap
	return s.finalizeHuntSubmit(ctx, finalizeHuntSubmitParams{
		req:            req,
		cfg:            cfg,
		inputN:         job.InputN,
		expectedU:      expectedU,
		expectedB:      expectedB,
		pass:           pass,
		recordFinding:  recordFinding,
		findingU:       findingU,
		findingB:       findingB,
		huntOrigLen:    huntOrigLen,
		fromReplayPending: true,
		now:            now,
	})
}

type finalizeHuntSubmitParams struct {
	req               SubmitRequest
	cfg               map[string]any
	inputN            uint64
	expectedU         uint64
	expectedB         []byte
	pass              bool
	recordFinding     bool
	findingU          uint64
	findingB          []byte
	huntOrigLen       int
	fromReplayPending bool
	now               int64
}

func (s *Service) finalizeHuntSubmit(ctx context.Context, p finalizeHuntSubmitParams) error {
	sem := fuzzengine.ParseCheckSemantics(p.cfg)
	hasWasm := false
	miner := strings.TrimSpace(p.req.MinerAddress)
	wantRunSettle := s.Settler != nil && escrowEnabled(p.cfg) && miner != ""
	runSettleStatus := ""
	if wantRunSettle {
		runSettleStatus = "pending"
	}
	statusWhere := "leased"
	if p.fromReplayPending {
		statusWhere = workStatusReplayPending
	}
	res, err := s.DB.ExecContext(ctx,
		`UPDATE fuzz_work_items
		 SET status='done', attempts=attempts+1, result_ok=?, duration_ms=?, last_error=?, lease_owner='', lease_until=0, updated_at=?,
		     miner_address=CASE WHEN ?!='' THEN ? ELSE miner_address END,
		     settle_run_status=CASE WHEN ?!='' THEN ? ELSE settle_run_status END
		 WHERE id=? AND campaign_id=?
		   AND status=?
		   AND lease_owner=CASE WHEN ?='leased' THEN ? ELSE lease_owner END`,
		boolToInt(p.pass), p.req.DurationMS, strings.TrimSpace(p.req.Trap), p.now,
		miner, miner, runSettleStatus, runSettleStatus,
		p.req.ItemID, p.req.CampaignID, statusWhere, statusWhere, p.req.WorkerID)
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return fmt.Errorf("poolfuzz: hunt finalize: work item state changed")
	}
	if err := s.recordCoverage(ctx, p.req.CampaignID, p.cfg, p.req.ActualInput, p.req.InputBytes, nil, p.now); err != nil {
		return err
	}
	if hunt.HuntCorpusGuided(p.cfg) {
		obsU, obsB := p.req.ActualInput, p.req.InputBytes
		if p.recordFinding && len(p.findingB) > 0 {
			obsU = p.findingU
			obsB = p.findingB
		}
		if err := s.observePoolCorpus(ctx, p.req.CampaignID, obsU, obsB, p.recordFinding, p.now); err != nil {
			return err
		}
	}
	var findingSeverity, findingType, findingID string
	if p.recordFinding {
		submitReq := p.req
		submitReq.ActualInput = p.findingU
		submitReq.InputOriginalLen = p.huntOrigLen
		if len(p.findingB) > 0 {
			submitReq.InputBytes = p.findingB
		} else {
			submitReq.InputBytes = p.expectedB
		}
		findingID, findingSeverity, findingType, err = s.insertFinding(ctx, submitReq, p.cfg, sem, hasWasm, p.now)
		if err != nil {
			return err
		}
	}
	if wantRunSettle {
		if p.recordFinding && huntBountyEligible(p.cfg, findingSeverity) && s.bountyAllowed(ctx, p.cfg, findingID) {
			_, _ = s.DB.ExecContext(ctx,
				`UPDATE fuzz_work_items SET settle_finding_status='pending', settle_finding_severity=? WHERE id=? AND campaign_id=?`,
				findingSeverity, p.req.ItemID, p.req.CampaignID)
		}
		if err := s.flushPendingSettles(ctx, p.req.CampaignID, p.req.ItemID, p.cfg); err != nil {
			return err
		}
		if p.recordFinding && miner != "" && fuzzengine.IsCrashClass(findingType) {
			if _, err := s.Settler.PayCrashBonus(ctx, p.req.CampaignID, miner, 0, 0); err != nil {
				low := strings.ToLower(err.Error())
				if !strings.Contains(low, "already paid") && !strings.Contains(low, "depleted") && !strings.Contains(low, "closed") {
					return fmt.Errorf("poolfuzz: settle crash bonus: %w", err)
				}
			}
		}
	}
	completed, err := s.recomputeProgress(ctx, p.req.CampaignID, p.now)
	if err != nil {
		return err
	}
	if completed && s.Settler != nil && escrowEnabled(p.cfg) {
		if _, err := s.Settler.Finalize(ctx, p.req.CampaignID, 0); err != nil {
			return fmt.Errorf("poolfuzz: finalize escrow: %w", err)
		}
	}
	return nil
}
