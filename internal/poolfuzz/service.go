package poolfuzz

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"hackme/internal/fuzzartifacts"
	"hackme/internal/fuzzengine"
	"hackme/internal/fuzznative"
	"hackme/internal/sandbox"
)

// Service runs distributed fuzz work queues on the coordinator SQLite DB.
type Service struct {
	DB      *sql.DB
	Settler Settler
}

type Campaign struct {
	ID            string
	CampaignType  string
	Status        string
	Title         string
	Description   string
	BudgetRuns    int
	BudgetSeconds int
	Config        map[string]any
	Summary       map[string]any
}

type ClaimedWork struct {
	WorkID         string
	CampaignID     string
	ItemID         int64
	InputN         uint64
	ActualInput    uint64
	InputBytes     []byte
	InputMode      string
	WasmCheckHex   string
	CheckSemantics string
	DepthTier      string
	PerRunHMC      float64
}

type SubmitRequest struct {
	WorkerID     string
	MinerAddress string
	WorkID       string
	CampaignID   string
	ItemID       int64
	InputN       uint64
	ActualInput  uint64
	InputBytes   []byte
	CheckResult  int32
	DurationMS   int
	Trap         string
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
		 VALUES (?, ?, ?, ?, ?, '', '', '', ?, ?, ?, ?, ?, ?, 0)
		 ON CONFLICT(id) DO UPDATE SET
		   campaign_type=excluded.campaign_type,
		   status=excluded.status,
		   title=excluded.title,
		   description=excluded.description,
		   budget_runs=excluded.budget_runs,
		   budget_seconds=excluded.budget_seconds,
		   config_json=excluded.config_json,
		   summary_json=excluded.summary_json,
		   started_at=CASE WHEN fuzz_campaigns.started_at=0 THEN excluded.started_at ELSE fuzz_campaigns.started_at END`,
		c.ID, strings.TrimSpace(c.CampaignType), status, strings.TrimSpace(c.Title), strings.TrimSpace(c.Description),
		c.BudgetRuns, c.BudgetSeconds, marshalConfigJSON(cfg), marshalSummaryJSON(summary), now, now)
	return err
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
		if err := s.EnsureWorkItems(ctx, id, now); err != nil {
			return err
		}
	}
	if pins, err := fuzznative.LoadPins(""); err == nil {
		_, _ = fuzznative.ProcessPending(ctx, s.DB, pins, 5)
	}
	return rows.Err()
}

// Claim leases one work item for a pool worker.
func (s *Service) Claim(ctx context.Context, workerID string, now int64) (ClaimedWork, bool, error) {
	var out ClaimedWork
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return out, false, fmt.Errorf("poolfuzz: worker_id required")
	}
	if err := s.Tick(ctx); err != nil {
		return out, false, err
	}
	leaseSec := leaseSeconds()
	rows, err := s.DB.QueryContext(ctx,
		`SELECT c.id, c.config_json, w.id, w.input_n
		 FROM fuzz_campaigns c
		 JOIN fuzz_work_items w ON w.campaign_id = c.id
		 WHERE c.status IN ('planned','running')
		   AND (w.status='pending' OR (w.status='leased' AND w.lease_until < ?))
		 ORDER BY w.updated_at ASC
		 LIMIT 20`, now)
	if err != nil {
		return out, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var campaignID, cfgJSON string
		var itemID int64
		var inputN uint64
		if err := rows.Scan(&campaignID, &cfgJSON, &itemID, &inputN); err != nil {
			return out, false, err
		}
		cfg := parseConfigJSON(cfgJSON)
		if !poolDistributed(cfg) {
			continue
		}
		res, err := s.DB.ExecContext(ctx,
			`UPDATE fuzz_work_items
			 SET status='leased', lease_owner=?, lease_until=?, updated_at=?
			 WHERE id=? AND campaign_id=? AND (status='pending' OR (status='leased' AND lease_until < ?))`,
			workerID, now+leaseSec, now, itemID, campaignID, now)
		if err != nil {
			return out, false, err
		}
		aff, _ := res.RowsAffected()
		if aff == 0 {
			continue
		}
		wasmHex := wasmHexFromConfig(cfg)
		actualU, actualB := derivePoolInputs(inputN, cfg)
		sem := fuzzengine.ParseCheckSemantics(cfg)
		perRun := perRunHMCFromConfig(cfg)
		out = ClaimedWork{
			WorkID:         fmt.Sprintf("%s:%d", campaignID, itemID),
			CampaignID:     campaignID,
			ItemID:         itemID,
			InputN:         inputN,
			ActualInput:    actualU,
			InputBytes:     actualB,
			InputMode:      string(fuzzengine.ParseInputMode(cfg)),
			WasmCheckHex:   wasmHex,
			CheckSemantics: string(sem),
			DepthTier:      string(fuzzengine.ParseDepthTier(cfg)),
			PerRunHMC:      perRun,
		}
		return out, true, nil
	}
	return out, false, rows.Err()
}

// Submit records a completed fuzz work item from a pool worker.
func (s *Service) Submit(ctx context.Context, req SubmitRequest) error {
	now := time.Now().Unix()
	cfgJSON := ""
	_ = s.DB.QueryRowContext(ctx, `SELECT config_json FROM fuzz_campaigns WHERE id=?`, req.CampaignID).Scan(&cfgJSON)
	cfg := parseConfigJSON(cfgJSON)
	sem := fuzzengine.ParseCheckSemantics(cfg)
	hasWasm := wasmHexFromConfig(cfg) != ""

	var pass bool
	var recordFinding bool
	if strings.TrimSpace(req.Trap) != "" {
		pass = false
		recordFinding = true
	} else {
		pass, recordFinding = fuzzengine.EvalCheck(sem, req.CheckResult, nil)
	}

	workerID := strings.TrimSpace(req.WorkerID)
	res, err := s.DB.ExecContext(ctx,
		`UPDATE fuzz_work_items
		 SET status='done', attempts=attempts+1, result_ok=?, duration_ms=?, last_error=?, lease_owner='', lease_until=0, updated_at=?
		 WHERE id=? AND campaign_id=?
		   AND status IN ('pending','leased')
		   AND (lease_owner='' OR lease_owner=?)`,
		boolToInt(pass), req.DurationMS, strings.TrimSpace(req.Trap), now, req.ItemID, req.CampaignID, workerID)
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return nil
	}
	if err := s.recordCoverage(ctx, req.CampaignID, req.ActualInput, req.InputBytes, now); err != nil {
		return err
	}
	var findingSeverity string
	var findingID string
	if recordFinding {
		var err error
		findingID, findingSeverity, err = s.insertFinding(ctx, req, cfg, sem, hasWasm, now)
		if err != nil {
			return err
		}
	}
	if s.Settler != nil && escrowEnabled(cfg) {
		miner := strings.TrimSpace(req.MinerAddress)
		if miner != "" {
			_ = s.Settler.PayRun(ctx, req.CampaignID, miner)
			if recordFinding && bountySeverity(findingSeverity) && s.bountyAllowed(ctx, cfg, findingID) {
				_ = s.Settler.PayFinding(ctx, req.CampaignID, miner, findingSeverity)
			}
		}
	}
	completed, err := s.recomputeProgress(ctx, req.CampaignID, now)
	if err != nil {
		return err
	}
	if completed && s.Settler != nil && escrowEnabled(cfg) {
		_ = s.Settler.Finalize(ctx, req.CampaignID)
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

func (s *Service) bountyAllowed(ctx context.Context, cfg map[string]any, findingID string) bool {
	if !fuzzengine.BountyRequiresNative(cfg) {
		return true
	}
	if findingID == "" || s.DB == nil {
		return false
	}
	ok, err := fuzznative.IsFindingNativeConfirmed(ctx, s.DB, findingID)
	return err == nil && ok
}

func (s *Service) recordCoverage(ctx context.Context, campaignID string, input uint64, inputBytes []byte, now int64) error {
	var edge, path int
	if len(inputBytes) > 0 {
		edge, path = fuzzengine.CoverageBucketsFromBytes(inputBytes)
	} else {
		edge, path = fuzzengine.CoverageBuckets(input)
	}
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

func (s *Service) insertFinding(ctx context.Context, req SubmitRequest, cfg map[string]any, sem fuzzengine.CheckSemantics, hasWasm bool, now int64) (string, string, error) {
	inputBytes := req.InputBytes
	var inputSHA string
	var artifactPath string
	var repro string
	if len(inputBytes) > 0 {
		inputSHA = fuzzengine.InputBytesSHA256(inputBytes)
		artifactPath = fuzzartifacts.WriteInputBytes(req.CampaignID, inputSHA, inputBytes)
		wasmHex, _ := cfg["wasm_check_hex"].(string)
		wasmPath := fuzzartifacts.WriteWasmHex(req.CampaignID, wasmHex)
		repro = fuzzengine.ReproCmdBytes(wasmPath, inputBytes)
	} else {
		inputSHA = fuzzengine.InputSHA256(req.ActualInput)
		wasmHex, _ := cfg["wasm_check_hex"].(string)
		wasmPath := fuzzartifacts.WriteWasmHex(req.CampaignID, wasmHex)
		artifactPath = fuzzartifacts.WriteInput(req.CampaignID, inputSHA, req.ActualInput)
		repro = fuzzengine.ReproCmdTool(wasmPath, req.ActualInput)
	}
	ft, sev, title := fuzzengine.ClassifyCheckFail(req.ActualInput, hasWasm, sem)
	if strings.TrimSpace(req.Trap) != "" {
		ft, sev, title = "crash", "high", "WASM trap: "+truncate(req.Trap, 200)
	}
	findingID := fmt.Sprintf("finding-pool-%s-%d-%d", req.CampaignID, req.ItemID, now)
	op, itemID, qty := fuzzengine.WasmCheckInputParts(req.ActualInput)
	wasmHex, _ := cfg["wasm_check_hex"].(string)
	_ = fuzzartifacts.WriteWasmHex(req.CampaignID, wasmHex)
	triage := fuzzengine.ClassifyFinding(ft, sev)
	detail, _ := json.Marshal(map[string]any{
		"source":          "pool_fuzz_worker_v2",
		"worker_id":       req.WorkerID,
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
	})
	_, err := s.DB.ExecContext(ctx,
		`INSERT OR IGNORE INTO fuzz_findings
		 (id, campaign_id, finding_type, severity, title, input_sha256, artifact_path, repro_cmd, detail_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		findingID, req.CampaignID, ft, sev, title, inputSHA, artifactPath, repro, string(detail), now)
	if err != nil {
		return "", "", err
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
		ib := inputBytes
		if len(ib) == 0 {
			ib = make([]byte, 8)
			for i := 0; i < 8; i++ {
				ib[i] = byte(req.ActualInput >> (8 * i))
			}
		}
		_ = fuzznative.QueueJob(ctx, s.DB, findingID, req.CampaignID, inputSHA, ib, fuzzengine.UpstreamTarget(cfg), guard, now)
	}
	return findingID, sev, nil
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
	summary["unique_crashes"] = crashed
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
	if cfg == nil {
		return 0
	}
	budget := floatFromJSON(cfg["budget_hmc"])
	runs := intFromJSON(cfg["budget_runs"])
	if budget <= 0 || runs < 8 {
		return 0
	}
	return (budget * 0.20) / float64(runs)
}

func wasmHexFromConfig(cfg map[string]any) string {
	return strings.TrimSpace(jsonString(cfg["wasm_check_hex"]))
}

func leaseSeconds() int64 {
	return 30
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
