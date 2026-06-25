package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// CurrentSchemaVersion is the value written to PRAGMA user_version after migrate() completes.
// Bump when adding a new migration step (and document in README / MASTER_PLAN).
const CurrentSchemaVersion = 13

// Open opens SQLite at path (directories created as needed).
func Open(dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil && filepath.Dir(dbPath) != "." {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", filepath.ToSlash(dbPath))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS blocks (
			block_index INTEGER NOT NULL UNIQUE,
			hash TEXT NOT NULL UNIQUE,
			prev_hash TEXT NOT NULL,
			json TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS wallet (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			address TEXT NOT NULL,
			balance_hmc REAL NOT NULL,
			balance_units INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			artifact_hash TEXT NOT NULL,
			reward REAL NOT NULL,
			status TEXT NOT NULL DEFAULT 'open',
			created_at INTEGER NOT NULL,
			manifest_json TEXT NOT NULL,
			kind TEXT NOT NULL DEFAULT 'synthetic_poh_v1',
			target_solves INTEGER NOT NULL DEFAULT 1,
			progress_count INTEGER NOT NULL DEFAULT 0
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	if err := migrateTasksEscrowColumns(db); err != nil {
		return err
	}
	if err := migrateLANPeerRigs(db); err != nil {
		return err
	}
	if err := migrateTransferLedger(db); err != nil {
		return err
	}
	if err := migrateSUPLedger(db); err != nil {
		return err
	}
	if err := migrateHMSLedger(db); err != nil {
		return err
	}
	if err := migrateWalletUnits(db); err != nil {
		return err
	}
	if err := migrateUnitsScaleToSatoshi(db); err != nil {
		return err
	}
	if err := migrateEconomicsMetaUnits(db); err != nil {
		return err
	}
	if err := migrateP2PSyncStage(db); err != nil {
		return err
	}
	if err := migrateFuzzCampaigns(db); err != nil {
		return err
	}
	if err := migrateFuzzEscrow(db); err != nil {
		return err
	}
	if err := migrateFuzzNativeQueue(db); err != nil {
		return err
	}
	return bumpUserVersion(db)
}

func bumpUserVersion(db *sql.DB) error {
	v, err := readUserVersion(db)
	if err != nil {
		return err
	}
	if v >= CurrentSchemaVersion {
		return nil
	}
	_, err = db.Exec(fmt.Sprintf("PRAGMA user_version = %d", CurrentSchemaVersion))
	return err
}

// readUserVersion returns SQLite PRAGMA user_version (0 on fresh DB before first bump).
func readUserVersion(db *sql.DB) (int, error) {
	var v int
	err := db.QueryRow("PRAGMA user_version").Scan(&v)
	return v, err
}

func migrateTasksEscrowColumns(db *sql.DB) error {
	for _, q := range []string{
		`ALTER TABLE tasks ADD COLUMN payer_ref TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tasks ADD COLUMN prepaid_hmc REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE tasks ADD COLUMN expires_at INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE tasks ADD COLUMN cancelled_at INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE tasks ADD COLUMN cancel_reason TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tasks ADD COLUMN refunded_hmc REAL NOT NULL DEFAULT 0`,
	} {
		if _, err := db.Exec(q); err != nil {
			// SQLite: duplicate column when migration already applied
			if strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
				continue
			}
			return err
		}
	}
	return nil
}

func migrateTransferLedger(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS accounts (
			address TEXT PRIMARY KEY,
			balance_units INTEGER NOT NULL DEFAULT 0,
			next_nonce INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
		)`,
		`CREATE TABLE IF NOT EXISTS tx_pool (
			tx_hash TEXT PRIMARY KEY,
			tx_json TEXT NOT NULL,
			from_address TEXT NOT NULL,
			to_address TEXT NOT NULL,
			nonce INTEGER NOT NULL,
			fee_units INTEGER NOT NULL,
			amount_units INTEGER NOT NULL,
			received_at INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			reject_code TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tx_history (
			tx_hash TEXT PRIMARY KEY,
			tx_json TEXT NOT NULL,
			from_address TEXT NOT NULL,
			to_address TEXT NOT NULL,
			nonce INTEGER NOT NULL,
			fee_units INTEGER NOT NULL,
			amount_units INTEGER NOT NULL,
			status TEXT NOT NULL,
			block_index INTEGER NOT NULL DEFAULT -1,
			block_hash TEXT NOT NULL DEFAULT '',
			applied_at INTEGER NOT NULL,
			reject_code TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tx_pool_from_nonce ON tx_pool(from_address, nonce)`,
		`CREATE INDEX IF NOT EXISTS idx_tx_pool_status_received ON tx_pool(status, received_at)`,
		`CREATE INDEX IF NOT EXISTS idx_tx_history_from_applied ON tx_history(from_address, applied_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_tx_history_to_applied ON tx_history(to_address, applied_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_tx_history_status_applied ON tx_history(status, applied_at DESC)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	if _, err := db.Exec(
		`INSERT INTO accounts (address, balance_units, next_nonce, updated_at)
		 SELECT address, CAST(ROUND(balance_hmc * 1000000.0) AS INTEGER), 0, strftime('%s','now')
		 FROM wallet WHERE id = 1
		 ON CONFLICT(address) DO NOTHING`,
	); err != nil {
		return err
	}
	return nil
}

func migrateSUPLedger(db *sql.DB) error {
	alters := []string{
		`ALTER TABLE accounts ADD COLUMN balance_sup_units INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE accounts ADD COLUMN sup_next_nonce INTEGER NOT NULL DEFAULT 0`,
	}
	for _, s := range alters {
		if _, err := db.Exec(s); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
				return err
			}
		}
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sup_tx_pool (
			tx_hash TEXT PRIMARY KEY,
			tx_json TEXT NOT NULL,
			from_address TEXT NOT NULL,
			to_address TEXT NOT NULL,
			nonce INTEGER NOT NULL,
			fee_units INTEGER NOT NULL,
			amount_units INTEGER NOT NULL,
			received_at INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			reject_code TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS sup_tx_history (
			tx_hash TEXT PRIMARY KEY,
			tx_json TEXT NOT NULL,
			from_address TEXT NOT NULL,
			to_address TEXT NOT NULL,
			nonce INTEGER NOT NULL,
			fee_units INTEGER NOT NULL,
			amount_units INTEGER NOT NULL,
			status TEXT NOT NULL,
			block_index INTEGER NOT NULL DEFAULT -1,
			block_hash TEXT NOT NULL DEFAULT '',
			applied_at INTEGER NOT NULL,
			reject_code TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sup_tx_pool_from_nonce ON sup_tx_pool(from_address, nonce)`,
		`CREATE INDEX IF NOT EXISTS idx_sup_tx_history_to_applied ON sup_tx_history(to_address, applied_at DESC)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

func migrateHMSLedger(db *sql.DB) error {
	alters := []string{
		`ALTER TABLE accounts ADD COLUMN balance_hms_units INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE accounts ADD COLUMN hms_next_nonce INTEGER NOT NULL DEFAULT 0`,
	}
	for _, s := range alters {
		if _, err := db.Exec(s); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
				return err
			}
		}
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS hms_tx_pool (
			tx_hash TEXT PRIMARY KEY,
			tx_json TEXT NOT NULL,
			from_address TEXT NOT NULL,
			to_address TEXT NOT NULL,
			nonce INTEGER NOT NULL,
			fee_units INTEGER NOT NULL,
			amount_units INTEGER NOT NULL,
			received_at INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			reject_code TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS hms_tx_history (
			tx_hash TEXT PRIMARY KEY,
			tx_json TEXT NOT NULL,
			from_address TEXT NOT NULL,
			to_address TEXT NOT NULL,
			nonce INTEGER NOT NULL,
			fee_units INTEGER NOT NULL,
			amount_units INTEGER NOT NULL,
			status TEXT NOT NULL,
			block_index INTEGER NOT NULL DEFAULT -1,
			block_hash TEXT NOT NULL DEFAULT '',
			applied_at INTEGER NOT NULL,
			reject_code TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_hms_tx_pool_from_nonce ON hms_tx_pool(from_address, nonce)`,
		`CREATE INDEX IF NOT EXISTS idx_hms_tx_history_to_applied ON hms_tx_history(to_address, applied_at DESC)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

func migrateWalletUnits(db *sql.DB) error {
	if _, err := db.Exec(`ALTER TABLE wallet ADD COLUMN balance_units INTEGER NOT NULL DEFAULT 0`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	if _, err := db.Exec(`UPDATE wallet SET balance_units = CAST(ROUND(balance_hmc * 1000000.0) AS INTEGER) WHERE balance_units <= 0`); err != nil {
		return err
	}
	return nil
}

func migrateUnitsScaleToSatoshi(db *sql.DB) error {
	const (
		oldUnits = int64(1_000_000)
		newUnits = int64(100_000_000)
	)
	var cur sql.NullString
	err := db.QueryRow(`SELECT value FROM meta WHERE key='units_per_hmc'`).Scan(&cur)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if cur.Valid && strings.TrimSpace(cur.String) == fmt.Sprintf("%d", newUnits) {
		return nil
	}
	// Existing deployments before this migration used 1e6 units/HMC.
	// Scale integer-ledger columns by x100 once, then pin meta units_per_hmc=1e8.
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	factor := newUnits / oldUnits
	stmts := []string{
		fmt.Sprintf(`UPDATE wallet SET balance_units = balance_units * %d`, factor),
		fmt.Sprintf(`UPDATE accounts SET balance_units = balance_units * %d`, factor),
		fmt.Sprintf(`UPDATE tx_pool SET amount_units = amount_units * %d, fee_units = fee_units * %d`, factor, factor),
		fmt.Sprintf(`UPDATE tx_history SET amount_units = amount_units * %d, fee_units = fee_units * %d`, factor, factor),
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE wallet SET balance_hmc = balance_units / 100000000.0`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO meta(key, value) VALUES('units_per_hmc', ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, fmt.Sprintf("%d", newUnits)); err != nil {
		return err
	}
	return tx.Commit()
}

func migrateP2PSyncStage(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS p2p_sync_stage (
			block_hash TEXT PRIMARY KEY,
			block_index INTEGER NOT NULL,
			prev_hash TEXT NOT NULL,
			peer_url TEXT NOT NULL,
			block_json TEXT NOT NULL,
			fetched_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_p2p_sync_stage_index ON p2p_sync_stage(block_index ASC)`,
		`CREATE INDEX IF NOT EXISTS idx_p2p_sync_stage_fetched ON p2p_sync_stage(fetched_at DESC)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

func migrateEconomicsMetaUnits(db *sql.DB) error {
	_, _ = db.Exec(`INSERT INTO meta(key, value)
		SELECT 'econ_total_minted_units', CAST(ROUND(CAST(value AS REAL) * 100000000.0) AS INTEGER)
		  FROM meta
		 WHERE key='econ_total_minted_hmc'
		ON CONFLICT(key) DO NOTHING`)
	_, _ = db.Exec(`INSERT INTO meta(key, value)
		SELECT 'econ_total_burned_units', CAST(ROUND(CAST(value AS REAL) * 100000000.0) AS INTEGER)
		  FROM meta
		 WHERE key='econ_total_burned_hmc'
		ON CONFLICT(key) DO NOTHING`)
	return nil
}

func migrateFuzzCampaigns(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS fuzz_campaigns (
			id TEXT PRIMARY KEY,
			campaign_type TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'planned',
			title TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			owner_ref TEXT NOT NULL DEFAULT '',
			task_id TEXT NOT NULL DEFAULT '',
			target_ref TEXT NOT NULL DEFAULT '',
			budget_runs INTEGER NOT NULL DEFAULT 0,
			budget_seconds INTEGER NOT NULL DEFAULT 0,
			config_json TEXT NOT NULL DEFAULT '{}',
			summary_json TEXT NOT NULL DEFAULT '{}',
			created_at INTEGER NOT NULL,
			started_at INTEGER NOT NULL DEFAULT 0,
			completed_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_fuzz_campaigns_status_created ON fuzz_campaigns(status, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_fuzz_campaigns_type_created ON fuzz_campaigns(campaign_type, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS fuzz_findings (
			id TEXT PRIMARY KEY,
			campaign_id TEXT NOT NULL,
			finding_type TEXT NOT NULL,
			severity TEXT NOT NULL DEFAULT 'info',
			title TEXT NOT NULL DEFAULT '',
			input_sha256 TEXT NOT NULL DEFAULT '',
			artifact_path TEXT NOT NULL DEFAULT '',
			repro_cmd TEXT NOT NULL DEFAULT '',
			detail_json TEXT NOT NULL DEFAULT '{}',
			created_at INTEGER NOT NULL,
			FOREIGN KEY(campaign_id) REFERENCES fuzz_campaigns(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_fuzz_findings_campaign_created ON fuzz_findings(campaign_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_fuzz_findings_campaign_severity ON fuzz_findings(campaign_id, severity, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS fuzz_corpus (
			campaign_id TEXT NOT NULL,
			input_sha256 TEXT NOT NULL,
			first_seen_at INTEGER NOT NULL,
			last_seen_at INTEGER NOT NULL,
			hits INTEGER NOT NULL DEFAULT 1,
			last_finding_id TEXT NOT NULL DEFAULT '',
			artifact_path TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(campaign_id, input_sha256),
			FOREIGN KEY(campaign_id) REFERENCES fuzz_campaigns(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_fuzz_corpus_campaign_last_seen ON fuzz_corpus(campaign_id, last_seen_at DESC)`,
		`CREATE TABLE IF NOT EXISTS fuzz_runtime_samples (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			campaign_id TEXT NOT NULL,
			sampled_at INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT '',
			runs_done INTEGER NOT NULL DEFAULT 0,
			new_edges INTEGER NOT NULL DEFAULT 0,
			new_paths INTEGER NOT NULL DEFAULT 0,
			unique_crashes INTEGER NOT NULL DEFAULT 0,
			heartbeat_at INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY(campaign_id) REFERENCES fuzz_campaigns(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_fuzz_runtime_samples_campaign_time ON fuzz_runtime_samples(campaign_id, sampled_at DESC)`,
		`CREATE TABLE IF NOT EXISTS fuzz_work_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			campaign_id TEXT NOT NULL,
			input_n INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			attempts INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			lease_owner TEXT NOT NULL DEFAULT '',
			lease_until INTEGER NOT NULL DEFAULT 0,
			result_ok INTEGER NOT NULL DEFAULT 0,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			FOREIGN KEY(campaign_id) REFERENCES fuzz_campaigns(id) ON DELETE CASCADE
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_fuzz_work_items_campaign_input ON fuzz_work_items(campaign_id, input_n)`,
		`CREATE INDEX IF NOT EXISTS idx_fuzz_work_items_campaign_status ON fuzz_work_items(campaign_id, status, updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS fuzz_coverage_seen (
			campaign_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			bucket INTEGER NOT NULL,
			first_seen_at INTEGER NOT NULL,
			PRIMARY KEY(campaign_id, kind, bucket),
			FOREIGN KEY(campaign_id) REFERENCES fuzz_campaigns(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS fuzz_report_access_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			campaign_id TEXT NOT NULL,
			actor_type TEXT NOT NULL,
			access_kind TEXT NOT NULL,
			remote_ip TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			accessed_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_fuzz_report_access_campaign_time ON fuzz_report_access_log(campaign_id, accessed_at DESC)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	// Existing databases may already have fuzz_campaigns without report token columns.
	for _, q := range []string{
		`ALTER TABLE fuzz_campaigns ADD COLUMN report_token_hash TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE fuzz_campaigns ADD COLUMN report_token_issued_at INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := db.Exec(q); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
				continue
			}
			return err
		}
	}
	return nil
}

func migrateFuzzNativeQueue(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS fuzz_native_queue (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		finding_id TEXT NOT NULL,
		campaign_id TEXT NOT NULL,
		input_sha256 TEXT NOT NULL DEFAULT '',
		input_bytes BLOB,
		status TEXT NOT NULL DEFAULT 'pending',
		upstream_target TEXT NOT NULL DEFAULT '',
		guard_name TEXT NOT NULL DEFAULT '',
		detail_json TEXT NOT NULL DEFAULT '{}',
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_fuzz_native_queue_status ON fuzz_native_queue(status, created_at ASC)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_fuzz_native_queue_finding ON fuzz_native_queue(finding_id)`)
	return err
}

func migrateFuzzEscrow(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS fuzz_campaign_escrow (
		campaign_id TEXT PRIMARY KEY,
		budget_units INTEGER NOT NULL,
		runs_pool_units INTEGER NOT NULL,
		bounty_pool_units INTEGER NOT NULL,
		runs_paid_units INTEGER NOT NULL DEFAULT 0,
		bounty_paid_units INTEGER NOT NULL DEFAULT 0,
		runs_done INTEGER NOT NULL DEFAULT 0,
		budget_runs INTEGER NOT NULL,
		per_run_units INTEGER NOT NULL,
		finding_winner TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'open',
		refunded_bounty_units INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL
	)`)
	return err
}
