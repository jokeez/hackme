package poolfuzz

import (
	"context"
	"database/sql"
)

// runsDoneForCampaign prefers live work-item counts over stale summary JSON.
func runsDoneForCampaign(ctx context.Context, db *sql.DB, campaignID string, summary map[string]any) int {
	runsDone := intFromJSON(summary["runs_done"])
	if db == nil || campaignID == "" {
		return runsDone
	}
	var done int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id=? AND status='done'`,
		campaignID).Scan(&done); err == nil && done > runsDone {
		return done
	}
	return runsDone
}
