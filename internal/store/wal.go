package store

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

const (
	// WALCheckpointTruncateBytes triggers an automatic TRUNCATE checkpoint when WAL exceeds this size.
	// Used by the chain node (large DB; tolerate bigger WAL).
	WALCheckpointTruncateBytes = 256 * 1024 * 1024
	// CoordinatorWALCheckpointBytes is a tighter default for the pool coordinator
	// (hot write path; large WAL → claim/submit timeouts).
	CoordinatorWALCheckpointBytes = 64 * 1024 * 1024
	// WALMetricsHeavyBytes skips expensive /api/metrics chain scans when WAL is bloated.
	WALMetricsHeavyBytes = 512 * 1024 * 1024
)

// WALMaintenanceConfig tunes online WAL checkpointing.
type WALMaintenanceConfig struct {
	// Interval between size checks (default 5m).
	Interval time.Duration
	// ThresholdBytes: run checkpoint when WAL >= this (default WALCheckpointTruncateBytes).
	ThresholdBytes int64
	// Prefer PASSIVE first (non-blocking); fall back to TRUNCATE if still large.
	PassiveFirst bool
}

// WALPath returns the -wal sidecar path for a SQLite database file.
func WALPath(dbPath string) string {
	return dbPath + "-wal"
}

// WALSizeBytes returns the WAL file size in bytes (0 if missing).
func WALSizeBytes(dbPath string) int64 {
	st, err := os.Stat(WALPath(dbPath))
	if err != nil {
		return 0
	}
	return st.Size()
}

// WALHeavyForMetrics reports whether chain DB maintenance is overdue enough to skip heavy metrics queries.
func WALHeavyForMetrics(dbPath string) bool {
	return WALSizeBytes(dbPath) >= WALMetricsHeavyBytes
}

// CheckpointTruncate runs PRAGMA wal_checkpoint(TRUNCATE). Best-effort while writers are active;
// may not fully shrink under heavy write load.
func CheckpointTruncate(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return nil
	}
	_, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

// CheckpointPassive runs PRAGMA wal_checkpoint(PASSIVE) — never blocks writers.
func CheckpointPassive(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return nil
	}
	_, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`)
	return err
}

// SetWALAutocheckpoint sets SQLite wal_autocheckpoint (pages). Lower = more frequent auto-checkpoint.
func SetWALAutocheckpoint(db *sql.DB, pages int) error {
	if db == nil {
		return nil
	}
	if pages < 100 {
		pages = 100
	}
	if pages > 10000 {
		pages = 10000
	}
	_, err := db.Exec(fmt.Sprintf(`PRAGMA wal_autocheckpoint = %d`, pages))
	return err
}

// StartWALMaintenance runs periodic WAL checkpoints when the sidecar grows large
// (node defaults: 5m / 256MiB / TRUNCATE).
func StartWALMaintenance(ctx context.Context, dbPath string, db *sql.DB) {
	StartWALMaintenanceWithConfig(ctx, dbPath, db, WALMaintenanceConfig{})
}

// StartWALMaintenanceWithConfig is the configurable online WAL maintainer.
func StartWALMaintenanceWithConfig(ctx context.Context, dbPath string, db *sql.DB, cfg WALMaintenanceConfig) {
	if db == nil || dbPath == "" {
		return
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	thresh := cfg.ThresholdBytes
	if thresh <= 0 {
		thresh = WALCheckpointTruncateBytes
	}
	go func() {
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				runWALCheckpointOnce(ctx, dbPath, db, thresh, cfg.PassiveFirst)
			}
		}
	}()
}

func runWALCheckpointOnce(ctx context.Context, dbPath string, db *sql.DB, thresh int64, passiveFirst bool) {
	wal := WALSizeBytes(dbPath)
	if wal < thresh {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var err error
	mode := "TRUNCATE"
	if passiveFirst {
		mode = "PASSIVE"
		err = CheckpointPassive(cctx, db)
		after := WALSizeBytes(dbPath)
		if err == nil && after < thresh {
			log.Printf("sqlite wal_checkpoint(%s): wal %d -> %d bytes", mode, wal, after)
			return
		}
		if err != nil {
			log.Printf("sqlite wal_checkpoint(PASSIVE): %v (wal=%d bytes); trying TRUNCATE", err, wal)
		}
		mode = "TRUNCATE"
	}
	err = CheckpointTruncate(cctx, db)
	if err != nil {
		log.Printf("sqlite wal_checkpoint(%s): %v (wal=%d bytes)", mode, err, wal)
		return
	}
	after := WALSizeBytes(dbPath)
	log.Printf("sqlite wal_checkpoint(%s): wal %d -> %d bytes", mode, wal, after)
}

// AbsDBPath resolves dbPath to an absolute path for WAL size checks.
func AbsDBPath(dbPath string) string {
	if p, err := filepath.Abs(dbPath); err == nil {
		return p
	}
	return dbPath
}
