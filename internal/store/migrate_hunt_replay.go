package store

import (
	"database/sql"
)

func migrateFuzzHuntReplayQueue(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS fuzz_hunt_replay_queue (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			campaign_id TEXT NOT NULL,
			item_id INTEGER NOT NULL,
			worker_id TEXT NOT NULL,
			miner_address TEXT NOT NULL DEFAULT '',
			input_n INTEGER NOT NULL DEFAULT 0,
			worker_check_result INTEGER NOT NULL DEFAULT 0,
			worker_trap TEXT NOT NULL DEFAULT '',
			segment_exec_done INTEGER NOT NULL DEFAULT 0,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'pending',
			verifier_id TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE(campaign_id, item_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_fuzz_hunt_replay_queue_status ON fuzz_hunt_replay_queue(status, created_at)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}
