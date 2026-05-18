package store

import (
	"context"
	"database/sql"
	"fmt"
)

// LANPeerRigRow is one persisted LAN worker (POST /api/push_work).
type LANPeerRigRow struct {
	WorkerID       string
	Name           string
	HashrateGHS    float64
	LastSeenUnix   int64
	IP             string
	SharesAccepted uint64
}

func migrateLANPeerRigs(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS lan_peer_rigs (
		worker_id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		hashrate_gh_s REAL NOT NULL DEFAULT 0,
		last_seen_unix INTEGER NOT NULL,
		ip TEXT NOT NULL DEFAULT '',
		shares_accepted INTEGER NOT NULL DEFAULT 0
	)`)
	return err
}

// UpsertLANPeerRig persists or updates a worker row (called after in-memory upsert).
func UpsertLANPeerRig(ctx context.Context, db *sql.DB, row LANPeerRigRow) error {
	if db == nil {
		return nil
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO lan_peer_rigs (worker_id, name, hashrate_gh_s, last_seen_unix, ip, shares_accepted)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(worker_id) DO UPDATE SET
			name = excluded.name,
			hashrate_gh_s = excluded.hashrate_gh_s,
			last_seen_unix = excluded.last_seen_unix,
			ip = excluded.ip,
			shares_accepted = excluded.shares_accepted
	`, row.WorkerID, row.Name, row.HashrateGHS, row.LastSeenUnix, row.IP, row.SharesAccepted)
	return err
}

// LoadLANPeerRigs returns all stored peers (for registry warm-start).
func LoadLANPeerRigs(ctx context.Context, db *sql.DB) ([]LANPeerRigRow, error) {
	if db == nil {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT worker_id, name, hashrate_gh_s, last_seen_unix, ip, shares_accepted FROM lan_peer_rigs`)
	if err != nil {
		return nil, fmt.Errorf("lan_peer_rigs: %w", err)
	}
	defer rows.Close()
	var out []LANPeerRigRow
	for rows.Next() {
		var r LANPeerRigRow
		if err := rows.Scan(&r.WorkerID, &r.Name, &r.HashrateGHS, &r.LastSeenUnix, &r.IP, &r.SharesAccepted); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteLANPeerRigsOlderThan removes rows with last_seen_unix strictly older than cutoff.
func DeleteLANPeerRigsOlderThan(ctx context.Context, db *sql.DB, cutoffUnix int64) (int64, error) {
	if db == nil {
		return 0, nil
	}
	res, err := db.ExecContext(ctx, `DELETE FROM lan_peer_rigs WHERE last_seen_unix < ?`, cutoffUnix)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return n, nil
}
