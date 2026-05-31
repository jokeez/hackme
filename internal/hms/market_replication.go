package hms

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func marketReplicaCount() int {
	n, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("HMS_MARKET_REPLICAS")))
	if n < 1 {
		n = 2
	}
	if n > 5 {
		n = 5
	}
	return n
}

// PickStorageWorkers returns up to n distinct online storage workers (most free quota first).
func (c *Coordinator) PickStorageWorkers(n int) ([]string, error) {
	if n < 1 {
		n = 1
	}
	cutoff := c.workerOnlineCutoff()
	rows, err := c.db.Query(`
		SELECT w.worker_id FROM hms_workers w
		WHERE w.role='storage' AND w.quota_gb > 0 AND w.last_seen_unix >= ?
		ORDER BY (w.quota_gb * 1073741824 - COALESCE((SELECT SUM(size) FROM hms_chunks c2 WHERE c2.worker_id=w.worker_id),0)) DESC
		LIMIT ?`, cutoff, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("no online storage workers — register workerstorage and keep heartbeat")
	}
	return out, nil
}

// AssignMarketChunk registers a customer backup chunk for PoSt (not blocked by seal epoch freeze).
func (c *Coordinator) AssignMarketChunk(chunkID, workerID string, ciphertextSHA256 []byte, size uint64, erasureMeta []byte) error {
	ep, err := c.CurrentEpoch()
	if err != nil {
		return err
	}
	banned, err := c.workerBanned(workerID, ep.EpochID)
	if err != nil {
		return err
	}
	if banned {
		return errors.New("worker banned for epoch")
	}
	now := time.Now().Unix()
	_, err = c.db.Exec(`INSERT INTO hms_chunks(chunk_id, ciphertext_sha256, size, erasure_meta, worker_id, epoch_id, created_unix)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(chunk_id) DO UPDATE SET ciphertext_sha256=excluded.ciphertext_sha256, size=excluded.size,
		erasure_meta=excluded.erasure_meta, worker_id=excluded.worker_id, created_unix=excluded.created_unix`,
		chunkID, ciphertextSHA256, size, erasureMeta, workerID, ep.EpochID, now)
	return err
}

func (c *Coordinator) recordChunkReplicas(orderID string, chunkIndex int, workers []string) error {
	_, err := c.db.Exec(`DELETE FROM hms_order_chunk_replicas WHERE order_id=? AND chunk_index=?`, orderID, chunkIndex)
	if err != nil {
		return err
	}
	for i, wid := range workers {
		if _, err := c.db.Exec(`INSERT INTO hms_order_chunk_replicas(order_id, chunk_index, replica_index, worker_id)
			VALUES(?,?,?,?)`, orderID, chunkIndex, i, wid); err != nil {
			return err
		}
	}
	_, err = c.db.Exec(`UPDATE hms_order_chunks SET replica_count=? WHERE order_id=? AND chunk_index=?`, len(workers), orderID, chunkIndex)
	return err
}

func (c *Coordinator) listChunkReplicaWorkers(orderID string, chunkIndex int) ([]string, error) {
	rows, err := c.db.Query(`SELECT worker_id FROM hms_order_chunk_replicas
		WHERE order_id=? AND chunk_index=? ORDER BY replica_index`, orderID, chunkIndex)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var wid string
		if err := rows.Scan(&wid); err != nil {
			return nil, err
		}
		out = append(out, wid)
	}
	if len(out) > 0 {
		return out, rows.Err()
	}
	// Legacy rows before replication table.
	var primary string
	err = c.db.QueryRow(`SELECT worker_id FROM hms_order_chunks WHERE order_id=? AND chunk_index=?`, orderID, chunkIndex).Scan(&primary)
	if err != nil {
		return nil, err
	}
	return []string{primary}, nil
}

func (c *Coordinator) readMarketChunkFile(workerID, chunkID string) ([]byte, error) {
	for _, p := range []string{
		filepathJoinMarket(marketStorageRoot(), workerID, chunkID+".dat"),
		filepathJoinMarket(marketDataRoot(), workerID, chunkID+".dat"),
	} {
		if p == "" {
			continue
		}
		b, err := os.ReadFile(p)
		if err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("chunk file missing for worker %s", workerID)
}

func filepathJoinMarket(root, workerID, name string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	return strings.TrimRight(root, string(os.PathSeparator)) + string(os.PathSeparator) + workerID + string(os.PathSeparator) + name
}
