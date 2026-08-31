package fuzznative

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// QueueJob enqueues a finding for native repro (idempotent per finding_id).
func QueueJob(ctx context.Context, db *sql.DB, findingID, campaignID, inputSHA string, input []byte, upstreamTarget, guardName string, now int64) error {
	if db == nil || strings.TrimSpace(findingID) == "" {
		return fmt.Errorf("fuzznative: invalid queue args")
	}
	var exists int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM fuzz_native_queue WHERE finding_id=? LIMIT 1`, findingID).Scan(&exists)
	if err == nil {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO fuzz_native_queue
		 (finding_id, campaign_id, input_sha256, input_bytes, status, upstream_target, guard_name, detail_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'pending', ?, ?, '{}', ?, ?)`,
		findingID, campaignID, strings.TrimSpace(strings.ToLower(inputSHA)), input,
		strings.TrimSpace(upstreamTarget), strings.TrimSpace(guardName), now, now)
	return err
}

// QueueJobVerified enqueues a finding already confirmed by coordinator replay (Hunt path).
func QueueJobVerified(ctx context.Context, db *sql.DB, findingID, campaignID, inputSHA string, input []byte, upstreamTarget, guardName string, status ReproStatus, now int64) error {
	if db == nil || strings.TrimSpace(findingID) == "" {
		return fmt.Errorf("fuzznative: invalid queue args")
	}
	st := strings.TrimSpace(string(status))
	if st == "" {
		st = string(StatusNativeCrash)
	}
	var exists int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM fuzz_native_queue WHERE finding_id=? LIMIT 1`, findingID).Scan(&exists)
	if err == nil {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	detail, _ := json.Marshal(ReproResult{Status: ReproStatus(st), UpstreamTarget: upstreamTarget, Note: "coordinator hunt replay verified"})
	_, err = db.ExecContext(ctx,
		`INSERT INTO fuzz_native_queue
		 (finding_id, campaign_id, input_sha256, input_bytes, status, upstream_target, guard_name, detail_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		findingID, campaignID, strings.TrimSpace(strings.ToLower(inputSHA)), input,
		st, strings.TrimSpace(upstreamTarget), strings.TrimSpace(guardName), string(detail), now, now)
	return err
}

// ProcessPending runs up to limit native repro jobs and updates campaign summary native_status.
func ProcessPending(ctx context.Context, db *sql.DB, pins *PinManifest, limit int) (processed int, err error) {
	if db == nil {
		return 0, fmt.Errorf("fuzznative: no database")
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	repoRoot := strings.TrimSpace(os.Getenv("HACKME_REPO_ROOT"))
	if repoRoot == "" {
		repoRoot = "."
	}
	now := time.Now().Unix()
	rows, err := db.QueryContext(ctx,
		`SELECT id, finding_id, campaign_id, input_bytes, upstream_target, guard_name
		 FROM fuzz_native_queue WHERE status='pending' ORDER BY created_at ASC LIMIT ?`, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var findingID, campaignID, upstream, guard string
		var input []byte
		if err := rows.Scan(&id, &findingID, &campaignID, &input, &upstream, &guard); err != nil {
			return processed, err
		}
		_, _ = db.ExecContext(ctx, `UPDATE fuzz_native_queue SET status='running', updated_at=? WHERE id=?`, now, id)
		mode := campaignReproMode(ctx, db, campaignID)
		result := EvalReproEx(mode, upstream, guard, input, pins, repoRoot)
		detail, _ := json.Marshal(result)
		_, err = db.ExecContext(ctx,
			`UPDATE fuzz_native_queue SET status=?, detail_json=?, updated_at=? WHERE id=?`,
			string(result.Status), string(detail), now, id)
		if err != nil {
			return processed, err
		}
		_ = updateCampaignNativeSummary(ctx, db, campaignID, result, now)
		_ = patchFindingNativeDetail(ctx, db, findingID, result)
		processed++
	}
	return processed, rows.Err()
}

func patchFindingNativeDetail(ctx context.Context, db *sql.DB, findingID string, result ReproResult) error {
	var detailJSON string
	if err := db.QueryRowContext(ctx, `SELECT detail_json FROM fuzz_findings WHERE id=?`, findingID).Scan(&detailJSON); err != nil {
		return err
	}
	m := map[string]any{}
	_ = json.Unmarshal([]byte(detailJSON), &m)
	m["native_repro"] = result
	b, _ := json.Marshal(m)
	_, err := db.ExecContext(ctx, `UPDATE fuzz_findings SET detail_json=? WHERE id=?`, string(b), findingID)
	return err
}

func updateCampaignNativeSummary(ctx context.Context, db *sql.DB, campaignID string, result ReproResult, now int64) error {
	var summaryJSON string
	if err := db.QueryRowContext(ctx, `SELECT summary_json FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&summaryJSON); err != nil {
		return err
	}
	summary := map[string]any{}
	_ = json.Unmarshal([]byte(summaryJSON), &summary)
	native := map[string]any{
		"status":          string(result.Status),
		"last_updated_at": now,
		"upstream_target": result.UpstreamTarget,
		"upstream_commit": result.UpstreamCommit,
	}
	if result.Status == StatusConfirmed {
		native["confirmed_count"] = intFromSummary(summary, "native.confirmed_count") + 1
	}
	if result.Status == StatusRejected {
		native["rejected_count"] = intFromSummary(summary, "native.rejected_count") + 1
	}
	if result.Status == StatusNativeCrash {
		native["crash_count"] = intFromSummary(summary, "native.crash_count") + 1
	}
	summary["native"] = native
	b, _ := json.Marshal(summary)
	_, err := db.ExecContext(ctx, `UPDATE fuzz_campaigns SET summary_json=? WHERE id=?`, string(b), campaignID)
	return err
}

func intFromSummary(summary map[string]any, path string) int {
	parts := strings.Split(path, ".")
	cur := any(summary)
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return 0
		}
		cur = m[p]
	}
	switch x := cur.(type) {
	case float64:
		return int(x)
	case int:
		return x
	default:
		return 0
	}
}

// IsFindingNativeConfirmed returns true when queue row is confirmed for a finding.
func IsFindingNativeConfirmed(ctx context.Context, db *sql.DB, findingID string) (bool, error) {
	ok, _, err := findingNativeStatus(ctx, db, findingID)
	return ok, err
}

// IsFindingNativeEligibleForBounty returns true when native repro confirms guard or reports ASAN crash.
func IsFindingNativeEligibleForBounty(ctx context.Context, db *sql.DB, findingID string) (bool, error) {
	ok, _, err := findingNativeStatus(ctx, db, findingID)
	return ok, err
}

func findingNativeStatus(ctx context.Context, db *sql.DB, findingID string) (eligible bool, status string, err error) {
	err = db.QueryRowContext(ctx,
		`SELECT status FROM fuzz_native_queue WHERE finding_id=? ORDER BY id DESC LIMIT 1`, findingID).Scan(&status)
	if err == sql.ErrNoRows {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	switch strings.ToLower(status) {
	case string(StatusConfirmed), string(StatusNativeCrash):
		return true, status, nil
	default:
		return false, status, nil
	}
}

func campaignReproMode(ctx context.Context, db *sql.DB, campaignID string) ReproMode {
	if db == nil || strings.TrimSpace(campaignID) == "" {
		return ReproModeGoPort
	}
	var configJSON string
	if err := db.QueryRowContext(ctx, `SELECT config_json FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&configJSON); err != nil {
		return ReproModeGoPort
	}
	cfg := map[string]any{}
	_ = json.Unmarshal([]byte(configJSON), &cfg)
	return ParseReproMode(cfg)
}

// CampaignNativeStatus reads summary_json native.status for a campaign.
func CampaignNativeStatus(ctx context.Context, db *sql.DB, campaignID string) (string, error) {
	var summaryJSON string
	if err := db.QueryRowContext(ctx, `SELECT summary_json FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&summaryJSON); err != nil {
		return "", err
	}
	summary := map[string]any{}
	_ = json.Unmarshal([]byte(summaryJSON), &summary)
	native, _ := summary["native"].(map[string]any)
	if native == nil {
		return "n/a", nil
	}
	return strings.TrimSpace(fmt.Sprint(native["status"])), nil
}
