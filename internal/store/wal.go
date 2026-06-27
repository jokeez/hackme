package store

import (
	"context"
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"time"
)

const (
	// WALCheckpointTruncateBytes triggers an automatic TRUNCATE checkpoint when WAL exceeds this size.
	WALCheckpointTruncateBytes = 256 * 1024 * 1024
	// WALMetricsHeavyBytes skips expensive /api/metrics chain scans when WAL is bloated.
	WALMetricsHeavyBytes = 512 * 1024 * 1024
)

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

// CheckpointTruncate runs PRAGMA wal_checkpoint(TRUNCATE). Best-effort while the node is running;
// stop writers first for reliable shrink on multi-GB WAL files.
func CheckpointTruncate(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return nil
	}
	_, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

// StartWALMaintenance runs periodic WAL checkpoints when the sidecar grows large.
func StartWALMaintenance(ctx context.Context, dbPath string, db *sql.DB) {
	if db == nil || dbPath == "" {
		return
	}
	go func() {
		tick := time.NewTicker(5 * time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				wal := WALSizeBytes(dbPath)
				if wal < WALCheckpointTruncateBytes {
					continue
				}
				cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
				err := CheckpointTruncate(cctx, db)
				cancel()
				if err != nil {
					log.Printf("sqlite wal_checkpoint: %v (wal=%d bytes)", err, wal)
					continue
				}
				after := WALSizeBytes(dbPath)
				log.Printf("sqlite wal_checkpoint(TRUNCATE): wal %d -> %d bytes", wal, after)
			}
		}
	}()
}

// AbsDBPath resolves dbPath to an absolute path for WAL size checks.
func AbsDBPath(dbPath string) string {
	if p, err := filepath.Abs(dbPath); err == nil {
		return p
	}
	return dbPath
}
