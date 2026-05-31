package hms

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// OpenDB opens HMS lane SQLite (separate from HMC coordinator DB).
func OpenDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", filepath.ToSlash(path))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrateHMS(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func migrateHMS(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS hms_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS hms_workers (
			worker_id TEXT PRIMARY KEY,
			role TEXT NOT NULL,
			pubkey_hex TEXT NOT NULL,
			quota_gb INTEGER NOT NULL DEFAULT 0,
			committed_gb REAL NOT NULL DEFAULT 0,
			strikes INTEGER NOT NULL DEFAULT 0,
			banned_until_epoch INTEGER NOT NULL DEFAULT 0,
			last_seen_unix INTEGER NOT NULL DEFAULT 0,
			created_unix INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS hms_chunks (
			chunk_id TEXT PRIMARY KEY,
			ciphertext_sha256 BLOB NOT NULL,
			size INTEGER NOT NULL,
			erasure_meta BLOB,
			worker_id TEXT NOT NULL,
			epoch_id INTEGER NOT NULL,
			created_unix INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS hms_epochs (
			epoch_id INTEGER PRIMARY KEY,
			started_unix INTEGER NOT NULL,
			freeze_unix INTEGER NOT NULL,
			seal_end_unix INTEGER NOT NULL,
			manifest_root BLOB,
			seal_target BLOB NOT NULL,
			seal_nonce INTEGER NOT NULL DEFAULT 0,
			seal_worker_id TEXT,
			sealed INTEGER NOT NULL DEFAULT 0,
			seal_found_unix INTEGER NOT NULL DEFAULT 0,
			payouts_enabled INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS hms_challenges (
			challenge_id TEXT PRIMARY KEY,
			epoch_id INTEGER NOT NULL,
			worker_id TEXT NOT NULL,
			chunk_id TEXT NOT NULL,
			sector_offset INTEGER NOT NULL,
			expected_hash BLOB NOT NULL,
			expires_unix INTEGER NOT NULL,
			answered INTEGER NOT NULL DEFAULT 0,
			ok INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS hms_seal_nonces (
			epoch_id INTEGER NOT NULL,
			nonce INTEGER NOT NULL,
			worker_id TEXT NOT NULL,
			PRIMARY KEY (epoch_id, nonce)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_hms_chunks_worker ON hms_chunks(worker_id)`,
		`CREATE INDEX IF NOT EXISTS idx_hms_challenges_worker ON hms_challenges(worker_id, epoch_id)`,
		`CREATE TABLE IF NOT EXISTS hms_orders (
			order_id TEXT PRIMARY KEY,
			label TEXT NOT NULL DEFAULT '',
			client_ref TEXT NOT NULL DEFAULT '',
			upload_token TEXT NOT NULL,
			size_plan_bytes INTEGER NOT NULL DEFAULT 0,
			bytes_uploaded INTEGER NOT NULL DEFAULT 0,
			chunk_count INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'draft',
			quote_hash TEXT NOT NULL DEFAULT '',
			prepaid_hmc REAL NOT NULL DEFAULT 0,
			retention_days INTEGER NOT NULL DEFAULT 30,
			payment_id TEXT NOT NULL DEFAULT '',
			created_unix INTEGER NOT NULL DEFAULT 0,
			updated_unix INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_hms_orders_status ON hms_orders(status)`,
		`CREATE TABLE IF NOT EXISTS hms_order_chunks (
			order_id TEXT NOT NULL,
			chunk_index INTEGER NOT NULL,
			chunk_id TEXT NOT NULL,
			worker_id TEXT NOT NULL,
			size INTEGER NOT NULL,
			ciphertext_sha256 BLOB NOT NULL,
			replica_count INTEGER NOT NULL DEFAULT 1,
			created_unix INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (order_id, chunk_index)
		)`,
		`CREATE TABLE IF NOT EXISTS hms_order_chunk_replicas (
			order_id TEXT NOT NULL,
			chunk_index INTEGER NOT NULL,
			replica_index INTEGER NOT NULL,
			worker_id TEXT NOT NULL,
			PRIMARY KEY (order_id, chunk_index, replica_index)
		)`,
		`CREATE TABLE IF NOT EXISTS hms_seal_shares (
			epoch_id INTEGER NOT NULL,
			worker_id TEXT NOT NULL,
			shares_ok INTEGER NOT NULL DEFAULT 0,
			shares_fail INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (epoch_id, worker_id)
		)`,
		`CREATE TABLE IF NOT EXISTS hms_epoch_payouts (
			epoch_id INTEGER NOT NULL,
			worker_id TEXT NOT NULL,
			winner_units INTEGER NOT NULL DEFAULT 0,
			participation_units INTEGER NOT NULL DEFAULT 0,
			total_units INTEGER NOT NULL,
			breakdown_json TEXT NOT NULL DEFAULT '',
			finalized_unix INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (epoch_id, worker_id)
		)`,
		`CREATE TABLE IF NOT EXISTS hms_warm_accrual (
			worker_id TEXT PRIMARY KEY,
			accrual_units INTEGER NOT NULL DEFAULT 0,
			shares_total INTEGER NOT NULL DEFAULT 0,
			updated_unix INTEGER NOT NULL DEFAULT 0
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	for _, s := range []string{
		`ALTER TABLE hms_epochs ADD COLUMN seal_budget_units INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE hms_epochs ADD COLUMN payouts_finalized INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE hms_order_chunks ADD COLUMN replica_count INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE hms_orders ADD COLUMN health_status TEXT NOT NULL DEFAULT 'ok'`,
		`ALTER TABLE hms_orders ADD COLUMN health_detail TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE hms_orders ADD COLUMN alert_unix INTEGER NOT NULL DEFAULT 0`,
		`CREATE TABLE IF NOT EXISTS hms_replica_health (
			order_id TEXT NOT NULL,
			chunk_index INTEGER NOT NULL,
			worker_id TEXT NOT NULL,
			healthy INTEGER NOT NULL DEFAULT 1,
			fail_count INTEGER NOT NULL DEFAULT 0,
			slashed INTEGER NOT NULL DEFAULT 0,
			last_ok_unix INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (order_id, chunk_index, worker_id)
		)`,
		`CREATE TABLE IF NOT EXISTS hms_repair_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			order_id TEXT NOT NULL,
			chunk_index INTEGER NOT NULL,
			chunk_id TEXT NOT NULL,
			from_worker TEXT NOT NULL,
			to_worker TEXT NOT NULL,
			bytes INTEGER NOT NULL,
			created_unix INTEGER NOT NULL
		)`,
	} {
		_, _ = db.Exec(s)
	}
	return nil
}
