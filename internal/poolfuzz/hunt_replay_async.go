package poolfuzz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/hunt"
)

// errHuntReplayLostOwnership means stale reclaim reassigned this queue row while ASAN
// was still running — abandon quietly; the peer verifier owns finalize.
var errHuntReplayLostOwnership = errors.New("poolfuzz: hunt replay lost ownership")

const (
	huntReplayStatusPending    = "pending"
	huntReplayStatusProcessing = "processing"
	huntReplayStatusDone       = "done"
	huntReplayStatusFailed     = "failed"
	workStatusReplayPending    = "replay_pending"
)

// huntClaimRank ranks worker crash/sanitizer claims for report #25/#26 anti-burial.
// Higher is stronger; clean/empty is 0.
func huntClaimRank(checkResult int32, trap string) int {
	trap = strings.TrimSpace(trap)
	if checkResult != 0 && strings.HasPrefix(trap, "hunt_crash:") {
		return 2
	}
	if checkResult != 0 && strings.HasPrefix(trap, "hunt_sanitizer:") {
		return 1
	}
	return 0
}

// huntClaimWouldWeaken is true when a re-submit replaces a crash/sanitizer claim with a weaker one.
// Kept for tests/diagnostics; enqueue uses huntClaimChanged (report #26) so upgrades are sticky too.
func huntClaimWouldWeaken(oldCR int32, oldTrap string, newCR int32, newTrap string) bool {
	return huntClaimRank(newCR, newTrap) < huntClaimRank(oldCR, oldTrap)
}

// huntClaimChanged is true when check_result/trap differ (any upgrade or downgrade).
// Report #26: the #25 weaken-only guard let forged worker_id inject crash over clean, then
// lock the victim out of repair. Pending claims must be idempotent-only.
func huntClaimChanged(oldCR int32, oldTrap string, newCR int32, newTrap string) bool {
	return oldCR != newCR || strings.TrimSpace(oldTrap) != strings.TrimSpace(newTrap)
}

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
		// Default 2: ASAN replay is CPU+SQLite heavy; 6 starved fuzz claim/submit under load.
		return 2
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 2
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
	// Busy: poll quickly. Idle: back off so empty queues do not hammer SQLite.
	delay := 250 * time.Millisecond
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			busy := false
			for {
				ok, err := s.processNextHuntReplayJob(ctx, workerLabel)
				if err != nil || !ok {
					break
				}
				busy = true
			}
			if busy {
				delay = 250 * time.Millisecond
			} else if delay < 2*time.Second {
				delay *= 2
				if delay > 2*time.Second {
					delay = 2 * time.Second
				}
			} else {
				delay = 2 * time.Second
			}
			timer.Reset(delay)
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
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return SubmitOutcome{}, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`UPDATE fuzz_work_items
		 SET status=?, duration_ms=?, updated_at=?, lease_owner='', lease_until=0
		 WHERE id=? AND campaign_id=? AND status='leased' AND lease_owner=?`,
		workStatusReplayPending, req.DurationMS, now,
		req.ItemID, req.CampaignID, req.WorkerID)
	if err != nil {
		return SubmitOutcome{}, err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		var st string
		_ = tx.QueryRowContext(ctx,
			`SELECT status FROM fuzz_work_items WHERE id=? AND campaign_id=?`,
			req.ItemID, req.CampaignID).Scan(&st)
		switch st {
		case workStatusReplayPending:
			// Reject payout/trap hijack from a different worker while replay is pending.
			var qWorker, qMiner string
			var qCR int32
			var qTrap, qStatus string
			_ = tx.QueryRowContext(ctx,
				`SELECT COALESCE(worker_id,''), COALESCE(miner_address,''),
				        COALESCE(worker_check_result,0), COALESCE(worker_trap,''), COALESCE(status,'')
				 FROM fuzz_hunt_replay_queue
				 WHERE campaign_id=? AND item_id=?`, req.CampaignID, req.ItemID).
				Scan(&qWorker, &qMiner, &qCR, &qTrap, &qStatus)
			if qWorker != "" && qWorker != strings.TrimSpace(req.WorkerID) {
				return SubmitOutcome{}, fmt.Errorf("poolfuzz: hunt replay already claimed by another worker")
			}
			// Report #33: miner_address is sticky including empty→non-empty. A first
			// unsigned/empty-miner claim must not be late-bound to an attacker's payout.
			if qMiner != "" {
				if miner != "" && qMiner != miner {
					return SubmitOutcome{}, fmt.Errorf("poolfuzz: hunt replay miner_address mismatch")
				}
			} else if miner != "" {
				return SubmitOutcome{}, fmt.Errorf("poolfuzz: hunt replay refuse miner_address bind after claim")
			}
			// Report #25/#26: pending claim is sticky. Refuse any check_result/trap change
			// (downgrade burial AND upgrade injection). Processing rows already require equality.
			if huntClaimChanged(qCR, qTrap, req.CheckResult, req.Trap) {
				return SubmitOutcome{}, fmt.Errorf("poolfuzz: hunt replay refuse claim change")
			}
			qStatus = strings.TrimSpace(strings.ToLower(qStatus))
			if qStatus == huntReplayStatusProcessing {
				// Idempotent retry only; claim already verified equal above.
			}
			qid, err := s.ensureHuntReplayQueueRowTx(ctx, tx, req, inputN, miner, now)
			if err != nil {
				return SubmitOutcome{}, err
			}
			if err := tx.Commit(); err != nil {
				return SubmitOutcome{}, err
			}
			return SubmitOutcome{Async: true, ReplayStatus: huntReplayStatusPending, QueueID: qid}, nil
		case "done", "cancelled":
			_ = tx.Rollback()
			return SubmitOutcome{ReplayStatus: huntReplayStatusDone}, nil
		default:
			return SubmitOutcome{}, fmt.Errorf("poolfuzz: work item not leased by worker")
		}
	}
	qid, err := s.ensureHuntReplayQueueRowTx(ctx, tx, req, inputN, miner, now)
	if err != nil {
		return SubmitOutcome{}, err
	}
	if err := tx.Commit(); err != nil {
		return SubmitOutcome{}, err
	}
	return SubmitOutcome{Async: true, ReplayStatus: huntReplayStatusPending, QueueID: qid}, nil
}

func (s *Service) ensureHuntReplayQueueRowTx(ctx context.Context, tx *sql.Tx, req SubmitRequest, inputN uint64, miner string, now int64) (int64, error) {
	// First writer wins for miner/worker identity — never let a later submit hijack payout (H-01).
	res, err := tx.ExecContext(ctx,
		`INSERT INTO fuzz_hunt_replay_queue
		 (campaign_id, item_id, worker_id, miner_address, input_n, worker_check_result, worker_trap, segment_exec_done, duration_ms, status, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(campaign_id, item_id) DO UPDATE SET
		   worker_id=CASE
		     WHEN fuzz_hunt_replay_queue.worker_id != '' THEN fuzz_hunt_replay_queue.worker_id
		     ELSE excluded.worker_id
		   END,
		   -- Report #33: miner sticky even when first write was empty (no late bind).
		   miner_address=fuzz_hunt_replay_queue.miner_address,
		   -- Report #26: claim fields are sticky after first write (same worker_id
		   -- upgrade/downgrade must not rewrite via ON CONFLICT either).
		   worker_check_result=fuzz_hunt_replay_queue.worker_check_result,
		   worker_trap=fuzz_hunt_replay_queue.worker_trap,
		   segment_exec_done=fuzz_hunt_replay_queue.segment_exec_done,
		   duration_ms=fuzz_hunt_replay_queue.duration_ms,
		   status=CASE
		     WHEN fuzz_hunt_replay_queue.status IN ('done','failed','processing') THEN fuzz_hunt_replay_queue.status
		     ELSE 'pending'
		   END,
		   last_error=CASE
		     WHEN fuzz_hunt_replay_queue.status IN ('done','failed') THEN fuzz_hunt_replay_queue.last_error
		     ELSE ''
		   END,
		   updated_at=excluded.updated_at`,
		req.CampaignID, req.ItemID, req.WorkerID, miner, inputN,
		req.CheckResult, strings.TrimSpace(req.Trap), req.SegmentExecDone, req.DurationMS,
		huntReplayStatusPending, now, now)
	if err != nil {
		return 0, err
	}
	qid, _ := res.LastInsertId()
	if qid == 0 {
		_ = tx.QueryRowContext(ctx,
			`SELECT id FROM fuzz_hunt_replay_queue WHERE campaign_id=? AND item_id=?`,
			req.CampaignID, req.ItemID).Scan(&qid)
	}
	return qid, nil
}

const huntReplayStaleProcessingSec int64 = 15 * 60

// huntReplayReclaimEverySec throttles stale reclaim off the hot claim path.
const huntReplayReclaimEverySec int64 = 30

var huntReplayLastReclaimUnix atomic.Int64

func (s *Service) reclaimStaleHuntReplayJobs(ctx context.Context, now int64) {
	if s == nil || s.DB == nil || now <= 0 {
		return
	}
	cutoff := now - huntReplayStaleProcessingSec
	_, _ = s.DB.ExecContext(ctx,
		`UPDATE fuzz_hunt_replay_queue
		 SET status=?, verifier_id='', last_error=CASE WHEN last_error='' THEN 'reclaimed stale processing' ELSE last_error END, updated_at=?
		 WHERE status=? AND updated_at < ?`,
		huntReplayStatusPending, now, huntReplayStatusProcessing, cutoff)
}

func (s *Service) maybeReclaimStaleHuntReplayJobs(ctx context.Context, now int64) {
	prev := huntReplayLastReclaimUnix.Load()
	if prev > 0 && now-prev < huntReplayReclaimEverySec {
		return
	}
	if !huntReplayLastReclaimUnix.CompareAndSwap(prev, now) {
		return
	}
	s.reclaimStaleHuntReplayJobs(ctx, now)
}

func huntReplayRetryable(err error) bool {
	if err == nil {
		return false
	}
	low := strings.ToLower(err.Error())
	// Only transient verifier/DB stalls — missing toolchain / missing binary are definitive.
	for _, needle := range []string{
		"sqlite_busy", "database is locked", "context deadline", "context canceled",
		"exec timeout", "signal: killed", "text file busy", "resource temporarily",
		"poolfuzz: settle", "finalize escrow",
	} {
		if strings.Contains(low, needle) {
			return true
		}
	}
	return false
}

func huntReplayRetryCount(lastError string) int {
	// Format: "retry:N:<msg>"
	if !strings.HasPrefix(lastError, "retry:") {
		return 0
	}
	rest := strings.TrimPrefix(lastError, "retry:")
	i := strings.IndexByte(rest, ':')
	if i <= 0 {
		return 0
	}
	n, err := strconv.Atoi(rest[:i])
	if err != nil || n < 0 {
		return 0
	}
	return n
}

const huntReplayMaxRetries = 8

func (s *Service) processNextHuntReplayJob(ctx context.Context, verifierID string) (bool, error) {
	now := time.Now().Unix()
	s.maybeReclaimStaleHuntReplayJobs(ctx, now)

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var job huntReplayJob
	var prevErr string
	err = tx.QueryRowContext(ctx,
		`SELECT id, campaign_id, item_id, worker_id, miner_address, input_n, worker_check_result, worker_trap, segment_exec_done, duration_ms, COALESCE(last_error,'')
		 FROM fuzz_hunt_replay_queue
		 WHERE status=?
		 ORDER BY created_at ASC, id ASC
		 LIMIT 1`, huntReplayStatusPending).Scan(
		&job.ID, &job.CampaignID, &job.ItemID, &job.WorkerID, &job.MinerAddress, &job.InputN,
		&job.WorkerCheckResult, &job.WorkerTrap, &job.SegmentExecDone, &job.DurationMS, &prevErr)
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

	procErr := s.runHuntReplayJob(ctx, job, now, verifierID)
	if procErr != nil {
		var workSt string
		_ = s.DB.QueryRowContext(ctx,
			`SELECT status FROM fuzz_work_items WHERE campaign_id=? AND id=?`,
			job.CampaignID, job.ItemID).Scan(&workSt)
		if workSt == "done" {
			// Terminal work already committed; do not flip result_ok. Retry settle-only or close queue.
			if huntReplayRetryable(procErr) {
				n := huntReplayRetryCount(prevErr) + 1
				if n <= huntReplayMaxRetries {
					msg := fmt.Sprintf("retry:%d:%s", n, procErr.Error())
					_, _ = s.DB.ExecContext(ctx,
						`UPDATE fuzz_hunt_replay_queue SET status=?, last_error=?, verifier_id='', updated_at=? WHERE id=? AND status=?`,
						huntReplayStatusPending, msg, time.Now().Unix(), job.ID, huntReplayStatusProcessing)
					// Yield ticker — avoid tight retry storm on same row.
					return false, nil
				}
			}
			_, _ = s.DB.ExecContext(ctx,
				`UPDATE fuzz_hunt_replay_queue SET status=?, last_error=?, updated_at=? WHERE id=?`,
				huntReplayStatusDone, procErr.Error(), time.Now().Unix(), job.ID)
			return true, nil
		}
		if huntReplayRetryable(procErr) {
			n := huntReplayRetryCount(prevErr) + 1
			if n <= huntReplayMaxRetries {
				msg := fmt.Sprintf("retry:%d:%s", n, procErr.Error())
				_, _ = s.DB.ExecContext(ctx,
					`UPDATE fuzz_hunt_replay_queue SET status=?, last_error=?, verifier_id='', updated_at=? WHERE id=? AND status=?`,
					huntReplayStatusPending, msg, time.Now().Unix(), job.ID, huntReplayStatusProcessing)
				return false, nil
			}
		}
		_, _ = s.DB.ExecContext(ctx,
			`UPDATE fuzz_hunt_replay_queue SET status=?, last_error=?, updated_at=? WHERE id=?`,
			huntReplayStatusFailed, procErr.Error(), time.Now().Unix(), job.ID)
		_, _ = s.DB.ExecContext(ctx,
			`UPDATE fuzz_work_items SET status='done', result_ok=0, last_error=?, updated_at=? WHERE campaign_id=? AND id=? AND status=?`,
			procErr.Error(), time.Now().Unix(), job.CampaignID, job.ItemID, workStatusReplayPending)
		return true, nil
	}
	_, _ = s.DB.ExecContext(ctx,
		`UPDATE fuzz_hunt_replay_queue SET status=?, last_error='', updated_at=? WHERE id=?`,
		huntReplayStatusDone, time.Now().Unix(), job.ID)
	return true, nil
}

func (s *Service) touchHuntReplayJob(ctx context.Context, jobID int64) {
	if s == nil || s.DB == nil || jobID <= 0 {
		return
	}
	_, _ = s.DB.ExecContext(ctx,
		`UPDATE fuzz_hunt_replay_queue SET updated_at=? WHERE id=? AND status=?`,
		time.Now().Unix(), jobID, huntReplayStatusProcessing)
}

func (s *Service) huntReplayStillOwned(ctx context.Context, jobID int64, verifierID string) bool {
	if s == nil || s.DB == nil || jobID <= 0 {
		return false
	}
	var st, vid string
	err := s.DB.QueryRowContext(ctx,
		`SELECT status, COALESCE(verifier_id,'') FROM fuzz_hunt_replay_queue WHERE id=?`, jobID).
		Scan(&st, &vid)
	if err != nil {
		return false
	}
	return st == huntReplayStatusProcessing && vid == verifierID
}

func (s *Service) runHuntReplayJob(ctx context.Context, job huntReplayJob, now int64, verifierID string) error {
	s.touchHuntReplayJob(ctx, job.ID)
	var campStatus, cfgJSON string
	if err := s.DB.QueryRowContext(ctx, `SELECT status, config_json FROM fuzz_campaigns WHERE id=?`, job.CampaignID).Scan(&campStatus, &cfgJSON); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(campStatus)) {
	case "cancelled", "paused", "completed":
		_, _ = s.DB.ExecContext(ctx,
			`UPDATE fuzz_work_items SET status='cancelled', updated_at=? WHERE campaign_id=? AND id=? AND status=?`,
			now, job.CampaignID, job.ItemID, workStatusReplayPending)
		return fmt.Errorf("poolfuzz: campaign %s", campStatus)
	}
	cfg := parseConfigJSON(cfgJSON)
	if !IsHuntCampaign(cfg) {
		return fmt.Errorf("poolfuzz: replay job not hunt campaign")
	}
	// Match claim-time L2 merge so mutating execs that prefer seed_byte_corpus stay
	// byte-identical to workers that received the post-merge corpus on the claim.
	if hunt.HuntCorpusGuided(cfg) {
		if targetID := strings.TrimSpace(jsonString(cfg["upstream_target_id"])); targetID != "" {
			if _, err := hunt.MergeLibFuzzerSeedCorpus(cfg, hunt.RepoRoot(), targetID); err != nil {
				return err
			}
		}
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
	s.touchHuntReplayJob(ctx, job.ID)
	onProgress := func(int) {
		s.touchHuntReplayJob(ctx, job.ID)
	}
	checkResult, trap, pass, recordFinding, huntFindingB, huntOrigLen, err := s.evalHuntSubmitCheckProgress(ctx, job.CampaignID, job.InputN, cfg, req, expectedB, seeds, onProgress)
	if err != nil {
		return err
	}
	if !s.huntReplayStillOwned(ctx, job.ID, verifierID) {
		return errHuntReplayLostOwnership
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
		req:               req,
		cfg:               cfg,
		inputN:            job.InputN,
		expectedU:         expectedU,
		expectedB:         expectedB,
		pass:              pass,
		recordFinding:     recordFinding,
		findingU:          findingU,
		findingB:          findingB,
		huntOrigLen:       huntOrigLen,
		fromReplayPending: true,
		now:               now,
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
	// Report #26: bind miner for pass OR confirmed finding. Never bind on rejected
	// checks (fake_crash: pass=false, recordFinding=false).
	minerBind := ""
	if p.pass || p.recordFinding {
		minerBind = miner
	}
	wantRunSettle := s.Settler != nil && escrowEnabled(p.cfg) && minerBind != "" && p.pass
	runSettleStatus := ""
	if wantRunSettle {
		runSettleStatus = "pending"
	}
	wantAnySettle := s.Settler != nil && escrowEnabled(p.cfg) && minerBind != ""
	statusWhere := "leased"
	if p.fromReplayPending {
		statusWhere = workStatusReplayPending
	}
	// Durable local effects first while work is still replay_pending/leased so transient
	// failures can retry without a done/failed split brain.
	newEdge, newPath, err := s.recordCoverage(ctx, p.req.CampaignID, p.cfg, p.req.ActualInput, p.req.InputBytes, nil, p.now)
	if err != nil {
		return err
	}
	if hunt.HuntCorpusGuided(p.cfg) {
		obsU, obsB := p.req.ActualInput, p.req.InputBytes
		if p.recordFinding && len(p.findingB) > 0 {
			obsU = p.findingU
			obsB = p.findingB
		}
		ftHint := ""
		if p.recordFinding {
			if IsHuntCampaign(p.cfg) {
				ft, _, _ := classifyHuntFinding(p.cfg, p.req)
				ftHint = ft
			} else if fuzzengine.IsHangTrap(p.req.Trap) {
				ftHint = "timeout_hang"
			} else if strings.TrimSpace(p.req.Trap) != "" {
				ft, _, _ := fuzzengine.ClassifyWasmTrap(p.req.ActualInput, p.req.Trap, true)
				ftHint = ft
			}
		}
		if err := s.observePoolCorpusNovelty(ctx, p.req.CampaignID, obsU, obsB, p.recordFinding, p.now, true, newEdge, newPath, ftHint); err != nil {
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
	res, err := s.DB.ExecContext(ctx,
		`UPDATE fuzz_work_items
		 SET status='done', attempts=attempts+1, result_ok=?, duration_ms=?, last_error=?, lease_owner='', lease_until=0, updated_at=?,
		     miner_address=?,
		     settle_run_status=CASE WHEN ?!='' THEN ? ELSE settle_run_status END
		 WHERE id=? AND campaign_id=?
		   AND status=?
		   AND lease_owner=CASE WHEN ?='leased' THEN ? ELSE lease_owner END`,
		boolToInt(p.pass), p.req.DurationMS, strings.TrimSpace(p.req.Trap), p.now,
		minerBind, runSettleStatus, runSettleStatus,
		p.req.ItemID, p.req.CampaignID, statusWhere, statusWhere, p.req.WorkerID)
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		// Work already marked done (prior attempt crashed mid-settle). Drain settles only —
		// never leave pending payouts stranded behind a non-retryable "state changed".
		var st string
		_ = s.DB.QueryRowContext(ctx,
			`SELECT status FROM fuzz_work_items WHERE campaign_id=? AND id=?`,
			p.req.CampaignID, p.req.ItemID).Scan(&st)
		if strings.TrimSpace(st) == "done" && s.Settler != nil && escrowEnabled(p.cfg) {
			if err := s.flushPendingSettles(ctx, p.req.CampaignID, p.req.ItemID, p.cfg); err != nil {
				return fmt.Errorf("poolfuzz: settle flush: %w", err)
			}
			completed, err := s.recomputeProgress(ctx, p.req.CampaignID, p.now)
			if err != nil {
				return err
			}
			if completed {
				if _, err := s.Settler.Finalize(ctx, p.req.CampaignID, 0); err != nil {
					return fmt.Errorf("poolfuzz: finalize escrow: %w", err)
				}
			}
			return nil
		}
		return fmt.Errorf("poolfuzz: hunt finalize: work item state changed")
	}
	if wantAnySettle {
		if p.recordFinding && huntBountyEligible(p.cfg, findingSeverity) && s.bountyAllowed(ctx, p.cfg, findingID) {
			_, _ = s.DB.ExecContext(ctx,
				`UPDATE fuzz_work_items SET settle_finding_status='pending', settle_finding_severity=? WHERE id=? AND campaign_id=?`,
				findingSeverity, p.req.ItemID, p.req.CampaignID)
		}
		if err := s.flushPendingSettles(ctx, p.req.CampaignID, p.req.ItemID, p.cfg); err != nil {
			return fmt.Errorf("poolfuzz: settle flush: %w", err)
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
