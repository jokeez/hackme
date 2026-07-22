package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"hackme/internal/fuzzartifacts"
	"hackme/internal/fuzzengine"
	"hackme/internal/sandbox"
)

type fuzzAutoCampaign struct {
	ID            string
	TaskID        string
	Status        string
	BudgetRuns    int
	BudgetSeconds int
	StartedAt     int64
	CreatedAt     int64
	ConfigJSON    string
	SummaryJSON   string
}

func (a *app) startFuzzAutoRunner(ctx context.Context) {
	if !envBool("HACKME_FUZZ_AUTORUN", true) {
		log.Printf("fuzz autorunner: disabled (HACKME_FUZZ_AUTORUN=0)")
		return
	}
	interval := 2 * time.Second
	if s := strings.TrimSpace(os.Getenv("HACKME_FUZZ_AUTORUN_TICK_SEC")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= 60 {
			interval = time.Duration(n) * time.Second
		}
	}
	log.Printf("fuzz autorunner: enabled tick=%s", interval)
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		lastCleanup := time.Now().Unix()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := a.fuzzAutoRunnerTick(ctx); err != nil {
					log.Printf("fuzz autorunner tick error: %v", err)
				}
				if err := a.fuzzNativeBridgeTick(ctx); err != nil {
					log.Printf("fuzz native bridge tick error: %v", err)
				}
				cleanupEverySec := int64(60)
				if s := strings.TrimSpace(os.Getenv("HACKME_FUZZ_RETENTION_INTERVAL_SEC")); s != "" {
					if n, err := strconv.ParseInt(s, 10, 64); err == nil && n >= 10 && n <= 86400 {
						cleanupEverySec = n
					}
				}
				now := time.Now().Unix()
				if now-lastCleanup >= cleanupEverySec {
					if err := a.fuzzAutoCleanup(ctx); err != nil {
						log.Printf("fuzz autorunner cleanup error: %v", err)
					}
					lastCleanup = now
				}
			}
		}
	}()
}

func (a *app) fuzzAutoRunnerTick(ctx context.Context) error {
	rows, err := a.db.QueryContext(ctx,
		`SELECT id, task_id, status, budget_runs, budget_seconds, started_at, created_at, config_json, summary_json
		 FROM fuzz_campaigns
		 WHERE status IN ('planned','running')
		 ORDER BY created_at ASC
		 LIMIT 200`)
	if err != nil {
		return err
	}
	defer rows.Close()
	now := time.Now().Unix()
	for rows.Next() {
		var c fuzzAutoCampaign
		if err := rows.Scan(&c.ID, &c.TaskID, &c.Status, &c.BudgetRuns, &c.BudgetSeconds, &c.StartedAt, &c.CreatedAt, &c.ConfigJSON, &c.SummaryJSON); err != nil {
			return err
		}
		cfg := parseMapJSON(c.ConfigJSON)
		if v, ok := cfg["auto_runner"]; ok {
			if strings.EqualFold(strings.TrimSpace(toString(v)), "0") || strings.EqualFold(strings.TrimSpace(toString(v)), "false") {
				continue
			}
		}
		if poolDistributedCampaign(cfg) {
			_ = a.syncPoolFuzzCampaign(ctx, c)
			continue
		}
		status := strings.TrimSpace(strings.ToLower(c.Status))
		if status == "planned" {
			status = "running"
			if c.StartedAt == 0 {
				c.StartedAt = now
			}
		}
		if status != "running" {
			continue
		}
		if _, err := a.db.ExecContext(ctx,
			`UPDATE fuzz_campaigns
			 SET status=?, started_at=CASE WHEN started_at=0 THEN ? ELSE started_at END
			 WHERE id=?`,
			status, now, c.ID); err != nil {
			return err
		}
		if err := a.ensureWorkItemsForCampaign(ctx, c, cfg, now); err != nil {
			return err
		}
		leaseOwner := "local-" + a.nodeID
		workBudget := 8
		if v, ok := cfg["worker_batch"]; ok {
			if n := intFromAny(v); n > 0 && n <= 256 {
				workBudget = n
			}
		}
		for i := 0; i < workBudget; i++ {
			it, ok, err := a.claimWorkItem(ctx, c.ID, leaseOwner, now)
			if err != nil {
				return err
			}
			if !ok {
				break
			}
			if err := a.executeWorkItem(ctx, c, cfg, it, now); err != nil {
				log.Printf("fuzz worker execute error campaign=%s item=%d: %v", c.ID, it.ID, err)
			}
		}
		if err := a.recomputeCampaignProgress(ctx, c.ID, now); err != nil {
			return err
		}
	}
	return rows.Err()
}

type fuzzWorkItem struct {
	ID       int64
	Campaign string
	InputN   uint64
	Attempts int
}

func (a *app) ensureWorkItemsForCampaign(ctx context.Context, c fuzzAutoCampaign, cfg map[string]any, now int64) error {
	if c.BudgetRuns <= 0 {
		return nil
	}
	queueDepth := 128
	if v, ok := cfg["queue_depth"]; ok {
		if n := intFromAny(v); n > 0 && n <= 10000 {
			queueDepth = n
		}
	}
	var existing int
	if err := a.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id=?`,
		c.ID).Scan(&existing); err != nil {
		return err
	}
	if existing >= c.BudgetRuns {
		return nil
	}
	toCreate := c.BudgetRuns - existing
	if toCreate > queueDepth {
		toCreate = queueDepth
	}
	for i := 0; i < toCreate; i++ {
		inputN := uint64(existing + i + 1)
		_, err := a.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO fuzz_work_items
			 (campaign_id, input_n, status, attempts, last_error, lease_owner, lease_until, result_ok, duration_ms, created_at, updated_at)
			 VALUES (?, ?, 'pending', 0, '', '', 0, 0, 0, ?, ?)`,
			c.ID, inputN, now, now)
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *app) claimWorkItem(ctx context.Context, campaignID, owner string, now int64) (fuzzWorkItem, bool, error) {
	var out fuzzWorkItem
	maxAttempts := retentionLimitFromEnv("HACKME_FUZZ_WORK_MAX_ATTEMPTS", 4, 20)
	var id int64
	var inputN uint64
	var attempts int
	err := a.db.QueryRowContext(ctx,
		`SELECT id, input_n, attempts
		 FROM fuzz_work_items
		 WHERE campaign_id=?
		   AND attempts < ?
		   AND (
		      status='pending' OR
		      (status='leased' AND lease_until < ?)
		   )
		 ORDER BY id ASC
		 LIMIT 1`,
		campaignID, maxAttempts, now).Scan(&id, &inputN, &attempts)
	if err == sql.ErrNoRows {
		return out, false, nil
	}
	if err != nil {
		return out, false, err
	}
	leaseSec := retentionLimitFromEnv("HACKME_FUZZ_WORK_LEASE_SEC", 10, 300)
	res, err := a.db.ExecContext(ctx,
		`UPDATE fuzz_work_items
		 SET status='leased', lease_owner=?, lease_until=?, updated_at=?
		 WHERE id=? AND (status='pending' OR (status='leased' AND lease_until < ?))`,
		owner, now+int64(leaseSec), now, id, now)
	if err != nil {
		return out, false, err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return out, false, nil
	}
	out = fuzzWorkItem{ID: id, Campaign: campaignID, InputN: inputN, Attempts: attempts}
	return out, true, nil
}

func decodeCampaignWasm(cfg map[string]any) []byte {
	hexStr := strings.TrimSpace(toString(cfg["wasm_check_hex"]))
	if hexStr == "" {
		return nil
	}
	b, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil
	}
	return b
}

func (a *app) loadCampaignWasm(ctx context.Context, c fuzzAutoCampaign, cfg map[string]any) []byte {
	if wb := decodeCampaignWasm(cfg); len(wb) > 0 {
		return wb
	}
	taskID := strings.TrimSpace(toString(cfg["task_id"]))
	if taskID == "" {
		taskID = strings.TrimSpace(c.TaskID)
	}
	if strings.TrimSpace(taskID) == "" {
		return nil
	}
	var manifest string
	if err := a.db.QueryRowContext(ctx, `SELECT manifest_json FROM tasks WHERE id=? LIMIT 1`, taskID).Scan(&manifest); err != nil {
		return nil
	}
	wb, err := a.chain.WasmCheckFromManifestJSON([]byte(manifest))
	if err != nil {
		return nil
	}
	return wb
}

func (a *app) executeWorkItem(ctx context.Context, c fuzzAutoCampaign, cfg map[string]any, it fuzzWorkItem, now int64) error {
	start := time.Now()
	timeoutMS := retentionLimitFromEnv("HACKME_FUZZ_WORK_TIMEOUT_MS", 350, 5000)
	if v, ok := cfg["work_timeout_ms"]; ok {
		if n := intFromAny(v); n >= 50 && n <= 10000 {
			timeoutMS = n
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()
	wasm := a.loadCampaignWasm(runCtx, c, cfg)
	wasmPath := ""
	if len(wasm) > 0 {
		wasmPath = fuzzartifacts.WriteWasmHex(c.ID, hex.EncodeToString(wasm))
	}
	actualInput := deriveFuzzInput(it.InputN, cfg)
	var inputBytes []byte
	if fuzzengine.ParseInputMode(cfg) == fuzzengine.InputModeBytes {
		inputBytes = fuzzengine.DeriveInputBytes(it.InputN, cfg)
		actualInput = fuzzengine.PackInputBytesToU64(inputBytes)
	}
	sem := fuzzengine.ParseCheckSemantics(cfg)
	pass := true
	var execErr error
	checkRet := int32(0)
	if len(wasm) > 0 {
		var invokeOK bool
		if len(inputBytes) > 0 {
			invokeOK, execErr = sandbox.InvokeCheckInput(runCtx, wasm, inputBytes)
		} else {
			invokeOK, execErr = sandbox.InvokeCheck(runCtx, wasm, actualInput)
		}
		if invokeOK {
			checkRet = 1
		}
		pass, _ = fuzzengine.EvalCheck(sem, checkRet, execErr)
	} else {
		pass = (actualInput%17 != 0)
	}
	durationMS := int(time.Since(start).Milliseconds())
	if durationMS < 0 {
		durationMS = 0
	}
	if execErr != nil {
		_ = a.insertWorkerWasmAnomaly(ctx, c.ID, it.InputN, actualInput, now, execErr, len(wasm) > 0, wasmPath)
		maxAttempts := retentionLimitFromEnv("HACKME_FUZZ_WORK_MAX_ATTEMPTS", 4, 20)
		nextStatus := "pending"
		if it.Attempts+1 >= maxAttempts {
			nextStatus = "failed"
		}
		_, _ = a.db.ExecContext(ctx,
			`UPDATE fuzz_work_items
			 SET status=?, attempts=attempts+1, last_error=?, lease_owner='', lease_until=0, updated_at=?
			 WHERE id=?`,
			nextStatus, strings.TrimSpace(execErr.Error()), now, it.ID)
		return nil
	}
	if _, err := a.db.ExecContext(ctx,
		`UPDATE fuzz_work_items
		 SET status='done', attempts=attempts+1, result_ok=?, duration_ms=?, last_error='', lease_owner='', lease_until=0, updated_at=?
		 WHERE id=?`,
		boolToInt(pass), durationMS, now, it.ID); err != nil {
		return err
	}
	if err := a.recordCoverageBuckets(ctx, c.ID, actualInput, inputBytes, now); err != nil {
		return err
	}
	recordFinding := false
	if len(wasm) > 0 {
		_, recordFinding = fuzzengine.EvalCheck(sem, checkRet, execErr)
	} else if !pass {
		recordFinding = true
	}
	if recordFinding {
		if err := a.insertWorkerWasmCheckFail(ctx, c.ID, it.InputN, actualInput, inputBytes, now, len(wasm) > 0, sem, wasmPath, cfg); err != nil {
			return err
		}
	}
	return nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (a *app) recordCoverageBuckets(ctx context.Context, campaignID string, input uint64, inputBytes []byte, now int64) error {
	var edgeBucket, pathBucket int
	if len(inputBytes) > 0 {
		edgeBucket, pathBucket = fuzzengine.CoverageBucketsFromBytes(inputBytes)
	} else {
		edgeBucket, pathBucket = fuzzCoverageBuckets(input)
	}
	_, err := a.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO fuzz_coverage_seen (campaign_id, kind, bucket, first_seen_at) VALUES (?, 'edge', ?, ?)`,
		campaignID, edgeBucket, now)
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO fuzz_coverage_seen (campaign_id, kind, bucket, first_seen_at) VALUES (?, 'path', ?, ?)`,
		campaignID, pathBucket, now)
	return err
}

func classifyWasmTrap(inputN uint64, execErr error, hasWasm bool) (findingType, severity, title string) {
	msg := strings.ToLower(execErr.Error())
	titleBase := strings.TrimSpace(execErr.Error())
	if len(titleBase) > 240 {
		titleBase = titleBase[:240]
	}
	if strings.Contains(msg, "divide by zero") {
		return "crash", "high", "WASM trap: integer divide by zero"
	}
	if strings.Contains(msg, "out of bounds") || strings.Contains(msg, "oob") {
		return "crash", "critical", "WASM trap: out-of-bounds memory access"
	}
	if strings.Contains(msg, "quarantined") || strings.Contains(msg, "trapped during validation") {
		return "sandbox_reject", "info", "Sandbox blocked WASM (invalid or trap-at-load module), not a target-code bug"
	}
	if hasWasm {
		op, itemID, qty := wasmCheckInputParts(inputN)
		if op == 2 && qty == 0 {
			return "crash", "high", "WASM trap: division by zero in op_type=2"
		}
		if op == 1 && itemID >= 3 {
			return "crash", "critical", fmt.Sprintf("WASM trap during OOB item lookup (item_id=%d)", itemID)
		}
	}
	if strings.Contains(msg, "check returned 0") || strings.Contains(msg, "property") {
		return "property_violation", "medium", titleBase
	}
	return "crash", "high", "WASM trap: " + titleBase
}

func (a *app) insertWorkerFindingClassified(ctx context.Context, campaignID string, inputN, actualInput uint64, inputBytes []byte, now int64, findingType, severity, title, wasmPath string, cfg map[string]any) error {
	var inputSHA, artifactPath, repro string
	if len(inputBytes) > 0 {
		inputSHA = fuzzengine.InputBytesSHA256(inputBytes)
		artifactPath = fuzzartifacts.WriteInputBytes(campaignID, inputSHA, inputBytes)
		repro = fuzzengine.ReproCmdBytes(wasmPath, inputBytes)
	} else {
		inputSHA = fuzzInputSHA256(actualInput)
		artifactPath = fuzzartifacts.WriteInput(campaignID, inputSHA, actualInput)
		repro = fuzzengine.ReproCmdTool(wasmPath, actualInput)
	}
	triage := fuzzengine.ClassifyFinding(findingType, severity)
	findingID := fmt.Sprintf("finding-worker-%s-%d-%d", campaignID, inputN, now)
	detail := map[string]any{
		"source":        "fuzz_worker_pipeline_v3",
		"input_n":       inputN,
		"actual_input":  actualInput,
		"input_len":     len(inputBytes),
		"timestamp":     now,
		"triage_class":  triage.Class,
		"triage_label":  triage.Label,
		"zero_day_hint": triage.ZeroDayHint,
	}
	op, itemID, qty := wasmCheckInputParts(actualInput)
	detail["op_type"] = op
	detail["item_id"] = itemID
	detail["quantity"] = qty
	if _, err := a.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO fuzz_findings
		 (id, campaign_id, finding_type, severity, title, input_sha256, artifact_path, repro_cmd, detail_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		findingID, campaignID, findingType, severity, title, inputSHA, artifactPath, repro, marshalMapJSON(detail), now); err != nil {
		return err
	}
	_, _ = a.db.ExecContext(ctx,
		`INSERT INTO fuzz_corpus (campaign_id, input_sha256, first_seen_at, last_seen_at, hits, last_finding_id, artifact_path)
		 VALUES (?, ?, ?, ?, 1, ?, ?)
		 ON CONFLICT(campaign_id, input_sha256) DO UPDATE SET
		   last_seen_at=excluded.last_seen_at,
		   hits=fuzz_corpus.hits+1,
		   last_finding_id=excluded.last_finding_id,
		   artifact_path=CASE WHEN excluded.artifact_path<>'' THEN excluded.artifact_path ELSE fuzz_corpus.artifact_path END`,
		campaignID, inputSHA, now, now, findingID, artifactPath)
	if cfg != nil {
		ib := inputBytes
		if len(ib) == 0 {
			ib = make([]byte, 8)
			for i := 0; i < 8; i++ {
				ib[i] = byte(actualInput >> (8 * i))
			}
		}
		a.enqueueNativeReproForFinding(ctx, campaignID, findingID, inputSHA, ib, cfg, now)
	}
	return nil
}

func (a *app) insertWorkerWasmCheckFail(ctx context.Context, campaignID string, inputN, actualInput uint64, inputBytes []byte, now int64, hasWasm bool, sem fuzzengine.CheckSemantics, wasmPath string, cfg map[string]any) error {
	ft, sev, title := fuzzengine.ClassifyCheckFail(actualInput, hasWasm, sem)
	return a.insertWorkerFindingClassified(ctx, campaignID, inputN, actualInput, inputBytes, now, ft, sev, title, wasmPath, cfg)
}

func (a *app) insertWorkerWasmAnomaly(ctx context.Context, campaignID string, inputN, actualInput uint64, now int64, execErr error, hasWasm bool, wasmPath string) error {
	ft, sev, title := classifyWasmTrap(actualInput, execErr, hasWasm)
	triage := fuzzengine.ClassifyFinding(ft, sev)
	detail := map[string]any{
		"source":        "fuzz_worker_pipeline_v2",
		"input_n":       inputN,
		"actual_input":  actualInput,
		"timestamp":     now,
		"trap":          execErr.Error(),
		"triage_class":  triage.Class,
		"triage_label":  triage.Label,
		"zero_day_hint": triage.ZeroDayHint,
	}
	inputSHA := fuzzInputSHA256(actualInput)
	artifactPath := fuzzartifacts.WriteInput(campaignID, inputSHA, actualInput)
	repro := fuzzengine.ReproCmdTool(wasmPath, actualInput)
	findingID := fmt.Sprintf("finding-worker-%s-%d-%d-trap", campaignID, inputN, now)
	if _, err := a.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO fuzz_findings
		 (id, campaign_id, finding_type, severity, title, input_sha256, artifact_path, repro_cmd, detail_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		findingID, campaignID, ft, sev, title, inputSHA, artifactPath, repro, marshalMapJSON(detail), now); err != nil {
		return err
	}
	_, _ = a.db.ExecContext(ctx,
		`INSERT INTO fuzz_corpus (campaign_id, input_sha256, first_seen_at, last_seen_at, hits, last_finding_id, artifact_path)
		 VALUES (?, ?, ?, ?, 1, ?, ?)
		 ON CONFLICT(campaign_id, input_sha256) DO UPDATE SET
		   last_seen_at=excluded.last_seen_at,
		   hits=fuzz_corpus.hits+1,
		   last_finding_id=excluded.last_finding_id,
		   artifact_path=CASE WHEN excluded.artifact_path<>'' THEN excluded.artifact_path ELSE fuzz_corpus.artifact_path END`,
		campaignID, inputSHA, now, now, findingID, artifactPath)
	return nil
}

func (a *app) recomputeCampaignProgress(ctx context.Context, campaignID string, now int64) error {
	var done, crashed, sumDuration int
	if err := a.db.QueryRowContext(ctx,
		`SELECT
		   COALESCE(SUM(CASE WHEN status='done' THEN 1 ELSE 0 END),0),
		   COALESCE(SUM(CASE WHEN status='done' AND result_ok=0 THEN 1 ELSE 0 END),0),
		   COALESCE(SUM(CASE WHEN status='done' THEN duration_ms ELSE 0 END),0)
		 FROM fuzz_work_items
		 WHERE campaign_id=?`, campaignID).Scan(&done, &crashed, &sumDuration); err != nil {
		return err
	}
	var edges, paths int
	_ = a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_coverage_seen WHERE campaign_id=? AND kind='edge'`, campaignID).Scan(&edges)
	_ = a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_coverage_seen WHERE campaign_id=? AND kind='path'`, campaignID).Scan(&paths)
	var firstFindingAt int64
	_ = a.db.QueryRowContext(ctx, `SELECT COALESCE(MIN(created_at),0) FROM fuzz_findings WHERE campaign_id=?`, campaignID).Scan(&firstFindingAt)
	var budgetRuns, budgetSeconds int
	var startedAt int64
	var status string
	var summaryJSON string
	if err := a.db.QueryRowContext(ctx,
		`SELECT budget_runs, budget_seconds, started_at, status, summary_json FROM fuzz_campaigns WHERE id=?`,
		campaignID).Scan(&budgetRuns, &budgetSeconds, &startedAt, &status, &summaryJSON); err != nil {
		return err
	}
	summary := parseMapJSON(summaryJSON)
	var cfgJSON string
	_ = a.db.QueryRowContext(ctx, `SELECT config_json FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&cfgJSON)
	cfg := parseMapJSON(cfgJSON)
	summary["fuzz_engine"] = fuzzEngineMetaFromConfig(cfg)
	summary["runs_done"] = done
	summary["new_edges"] = edges
	summary["new_paths"] = paths
	summary["failed_checks"] = crashed
	crashClass := 0
	if frows, err := a.db.QueryContext(ctx, `SELECT finding_type FROM fuzz_findings WHERE campaign_id=?`, campaignID); err == nil {
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
	if crashed > 0 && firstFindingAt > 0 && startedAt > 0 && firstFindingAt >= startedAt {
		summary["time_to_first_crash_sec"] = int(firstFindingAt - startedAt)
	}
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
	if _, err := a.db.ExecContext(ctx,
		`UPDATE fuzz_campaigns
		 SET status=?, summary_json=?, completed_at=CASE WHEN ?='completed' AND completed_at=0 THEN ? ELSE completed_at END
		 WHERE id=?`,
		nextStatus, marshalMapJSON(summary), nextStatus, completedAt, campaignID); err != nil {
		return err
	}
	_, err := a.db.ExecContext(ctx,
		`INSERT INTO fuzz_runtime_samples
		 (campaign_id, sampled_at, status, runs_done, new_edges, new_paths, unique_crashes, heartbeat_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		campaignID, now, nextStatus, done, edges, paths, crashed, now)
	return err
}

func retentionLimitFromEnv(key string, def int, max int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	if n < 0 {
		return 0
	}
	if n > max {
		return max
	}
	return n
}

func (a *app) runCampaignRetention(ctx context.Context, campaignID string, maxFindings, maxCorpus, maxSamples int) (int64, error) {
	var deleted int64
	if maxFindings > 0 {
		res, err := a.db.ExecContext(ctx,
			`DELETE FROM fuzz_findings
			 WHERE campaign_id=?
			   AND id IN (
			      SELECT id FROM fuzz_findings
			      WHERE campaign_id=?
			      ORDER BY created_at DESC, id DESC
			      LIMIT -1 OFFSET ?
			   )`,
			campaignID, campaignID, maxFindings)
		if err != nil {
			return deleted, err
		}
		n, _ := res.RowsAffected()
		deleted += n
	}
	if maxCorpus > 0 {
		res, err := a.db.ExecContext(ctx,
			`DELETE FROM fuzz_corpus
			 WHERE campaign_id=?
			   AND input_sha256 IN (
			      SELECT input_sha256 FROM fuzz_corpus
			      WHERE campaign_id=?
			      ORDER BY hits DESC, last_seen_at DESC
			      LIMIT -1 OFFSET ?
			   )`,
			campaignID, campaignID, maxCorpus)
		if err != nil {
			return deleted, err
		}
		n, _ := res.RowsAffected()
		deleted += n
	}
	if maxSamples > 0 {
		res, err := a.db.ExecContext(ctx,
			`DELETE FROM fuzz_runtime_samples
			 WHERE campaign_id=?
			   AND id IN (
			      SELECT id FROM fuzz_runtime_samples
			      WHERE campaign_id=?
			      ORDER BY sampled_at DESC, id DESC
			      LIMIT -1 OFFSET ?
			   )`,
			campaignID, campaignID, maxSamples)
		if err != nil {
			return deleted, err
		}
		n, _ := res.RowsAffected()
		deleted += n
		workCap := maxSamples * 2
		if workCap < 1000 {
			workCap = 1000
		}
		res, err = a.db.ExecContext(ctx,
			`DELETE FROM fuzz_work_items
			 WHERE campaign_id=?
			   AND id IN (
			      SELECT id FROM fuzz_work_items
			      WHERE campaign_id=?
			      ORDER BY updated_at DESC, id DESC
			      LIMIT -1 OFFSET ?
			   )`,
			campaignID, campaignID, workCap)
		if err != nil {
			return deleted, err
		}
		n, _ = res.RowsAffected()
		deleted += n
	}
	return deleted, nil
}

func (a *app) fuzzAutoCleanup(ctx context.Context) error {
	maxFindings := retentionLimitFromEnv("HACKME_FUZZ_RETENTION_FINDINGS_PER_CAMPAIGN", 5000, 100000)
	maxCorpus := retentionLimitFromEnv("HACKME_FUZZ_RETENTION_CORPUS_PER_CAMPAIGN", 2000, 100000)
	maxSamples := retentionLimitFromEnv("HACKME_FUZZ_RETENTION_RUNTIME_SAMPLES_PER_CAMPAIGN", 2000, 200000)
	if maxFindings == 0 && maxCorpus == 0 && maxSamples == 0 {
		return nil
	}
	rows, err := a.db.QueryContext(ctx, `SELECT id FROM fuzz_campaigns`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var totalDeleted int64
	for rows.Next() {
		var campaignID string
		if err := rows.Scan(&campaignID); err != nil {
			return err
		}
		n, err := a.runCampaignRetention(ctx, campaignID, maxFindings, maxCorpus, maxSamples)
		if err != nil {
			return err
		}
		totalDeleted += n
	}
	if totalDeleted > 0 {
		log.Printf("fuzz cleanup: deleted rows=%d", totalDeleted)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	ttlSec := fuzzArtifactTTLSeconds(7 * 24 * 3600)
	maxBytes := fuzzArtifactMaxBytes(512 * 1024 * 1024)
	artifactRes, err := a.runFuzzArtifactCleanup(ctx, ttlSec, maxBytes)
	if err != nil {
		return err
	}
	if artifactRes.DeletedFiles > 0 {
		log.Printf("fuzz artifact cleanup: deleted_files=%d deleted_bytes=%d kept_files=%d kept_bytes=%d",
			artifactRes.DeletedFiles, artifactRes.DeletedBytes, artifactRes.KeptFiles, artifactRes.KeptBytes)
	}
	return nil
}
