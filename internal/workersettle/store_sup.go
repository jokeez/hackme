package workersettle

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SUP settlement-by-transfer ledger (Economics v2). Mirrors the HMC pending → tx_hash → apply
// cycle on its own columns/table: settled_sup_units / session_anchor_sup_units watermarks,
// pending_sup_json, settle_sup_outbox. HMC rows are never read or written here.

func (s *Store) inImmediateTx(ctx context.Context, fn func(conn *sql.Conn) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	if err := fn(conn); err != nil {
		_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		return err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		return err
	}
	return nil
}

// SetPendingSUPUnits records a durable SUP payout intent before the send (refuses a second one).
func (s *Store) SetPendingSUPUnits(ctx context.Context, workerID, payoutAddr string, deltaUnits uint64, nonce int64) error {
	workerID = strings.TrimSpace(workerID)
	payoutAddr = strings.TrimSpace(payoutAddr)
	if workerID == "" || payoutAddr == "" || deltaUnits == 0 {
		return fmt.Errorf("workersettle: invalid SUP pending args")
	}
	now := time.Now().Unix()
	raw, _ := json.Marshal(map[string]any{
		"delta_units": deltaUnits, "payout_address": payoutAddr, "started_unix": now, "nonce": nonce,
	})
	return s.inImmediateTx(ctx, func(conn *sql.Conn) error {
		var existing string
		_ = conn.QueryRowContext(ctx, `SELECT COALESCE(pending_sup_json,'') FROM settle_workers WHERE worker_id=?`, workerID).Scan(&existing)
		if strings.TrimSpace(existing) != "" {
			return fmt.Errorf("workersettle: SUP pending already set for %s — refuse second intent", workerID)
		}
		if _, err := conn.ExecContext(ctx, `
INSERT INTO settle_workers (worker_id, payout_address, pending_sup_json, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT(worker_id) DO UPDATE SET pending_sup_json=excluded.pending_sup_json, updated_at=excluded.updated_at`,
			workerID, payoutAddr, string(raw), now); err != nil {
			return err
		}
		_, err := conn.ExecContext(ctx, `
INSERT INTO settle_sup_outbox (worker_id, payout_address, delta_units, status, nonce, created_at)
VALUES (?, ?, ?, 'pending', ?, ?)`, workerID, payoutAddr, deltaUnits, nonce, now)
		return err
	})
}

// AttachSUPTxHash stamps the SUP pending intent and its outbox row with the tx hash.
func (s *Store) AttachSUPTxHash(ctx context.Context, workerID, txHash string) error {
	workerID = strings.TrimSpace(workerID)
	txHash = strings.TrimSpace(txHash)
	if workerID == "" || txHash == "" {
		return fmt.Errorf("workersettle: empty worker or tx")
	}
	return s.inImmediateTx(ctx, func(conn *sql.Conn) error {
		var pendJSON string
		if err := conn.QueryRowContext(ctx, `SELECT COALESCE(pending_sup_json,'') FROM settle_workers WHERE worker_id=?`, workerID).Scan(&pendJSON); err != nil {
			return err
		}
		if strings.TrimSpace(pendJSON) == "" {
			return fmt.Errorf("workersettle: no SUP pending for %s", workerID)
		}
		pend := map[string]any{}
		_ = json.Unmarshal([]byte(pendJSON), &pend)
		pend["tx_hash"] = txHash
		raw, _ := json.Marshal(pend)
		if _, err := conn.ExecContext(ctx, `UPDATE settle_workers SET pending_sup_json=?, updated_at=? WHERE worker_id=?`,
			string(raw), time.Now().Unix(), workerID); err != nil {
			return err
		}
		_, err := conn.ExecContext(ctx, `
UPDATE settle_sup_outbox SET tx_hash=?
 WHERE id=(SELECT id FROM settle_sup_outbox WHERE worker_id=? AND status='pending' ORDER BY id DESC LIMIT 1)`, txHash, workerID)
		return err
	})
}

// ClearPendingSUP drops the SUP intent (definitive reject) and abandons its outbox rows.
func (s *Store) ClearPendingSUP(ctx context.Context, workerID string) error {
	workerID = strings.TrimSpace(workerID)
	now := time.Now().Unix()
	return s.inImmediateTx(ctx, func(conn *sql.Conn) error {
		if _, err := conn.ExecContext(ctx, `UPDATE settle_workers SET pending_sup_json='', updated_at=? WHERE worker_id=?`, now, workerID); err != nil {
			return err
		}
		_, err := conn.ExecContext(ctx, `UPDATE settle_sup_outbox SET status='abandoned', applied_at=? WHERE worker_id=? AND status='pending'`, now, workerID)
		return err
	})
}

// ApplySettleSUPUnits advances the SUP watermark after a confirmed transfer and clears the intent.
// HMC columns are untouched.
func (s *Store) ApplySettleSUPUnits(ctx context.Context, workerID, payoutAddr, txHash string, newSettledUnits, anchorUnits uint64, clearAnchor bool) error {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return fmt.Errorf("workersettle: empty worker_id")
	}
	if clearAnchor {
		anchorUnits = 0
	}
	now := time.Now().Unix()
	return s.inImmediateTx(ctx, func(conn *sql.Conn) error {
		var prev uint64
		_ = conn.QueryRowContext(ctx, `SELECT COALESCE(settled_sup_units,0) FROM settle_workers WHERE worker_id=?`, workerID).Scan(&prev)
		if newSettledUnits < prev {
			return fmt.Errorf("workersettle: SUP watermark would go down for %s (%d < %d)", workerID, newSettledUnits, prev)
		}
		if _, err := conn.ExecContext(ctx, `
INSERT INTO settle_workers (worker_id, settled_sup, settled_sup_units, session_anchor_sup, session_anchor_sup_units, payout_address, pending_sup_json, updated_at)
VALUES (?, ?, ?, ?, ?, ?, '', ?)
ON CONFLICT(worker_id) DO UPDATE SET
  settled_sup=excluded.settled_sup,
  settled_sup_units=excluded.settled_sup_units,
  session_anchor_sup=excluded.session_anchor_sup,
  session_anchor_sup_units=excluded.session_anchor_sup_units,
  pending_sup_json='',
  updated_at=excluded.updated_at`,
			workerID, UnitsToHMC(newSettledUnits), newSettledUnits, UnitsToHMC(anchorUnits), anchorUnits, strings.TrimSpace(payoutAddr), now); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, `
UPDATE settle_sup_outbox SET status='applied', tx_hash=?, applied_at=?
 WHERE id=(SELECT id FROM settle_sup_outbox WHERE worker_id=? AND status='pending' ORDER BY id DESC LIMIT 1)`,
			strings.TrimSpace(txHash), now, workerID); err != nil {
			return err
		}
		_, err := conn.ExecContext(ctx, `INSERT INTO settle_ledger (kind, worker_id, units, note, created_at) VALUES ('sup_payout_applied', ?, ?, ?, ?)`,
			workerID, newSettledUnits-prev, fmt.Sprintf("tx=%s sup_watermark_units=%d", txHash, newSettledUnits), now)
		return err
	})
}
