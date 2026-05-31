package hms

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	OrderStatusDegraded = "degraded"
	HealthOK            = "ok"
	HealthDegraded      = "degraded"
	HealthFailed        = "failed"
)

// RunHealthLoop repairs missing replicas and refreshes order health SLA.
func (c *Coordinator) RunHealthLoop(ctx context.Context) {
	interval := time.Duration(c.cfg.RepairIntervalSec) * time.Second
	if interval < 5*time.Second {
		interval = 30 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = c.RunHealthTick()
		}
	}
}

// RunHealthTick scans replicas, attempts repair, updates order health fields.
func (c *Coordinator) RunHealthTick() error {
	if err := c.repairMissingReplicas(); err != nil {
		return err
	}
	return c.refreshOrderHealth()
}

func (c *Coordinator) workerOnline(workerID string) bool {
	cutoff := c.workerOnlineCutoff()
	var last int64
	err := c.db.QueryRow(`SELECT last_seen_unix FROM hms_workers WHERE worker_id=?`, workerID).Scan(&last)
	if err != nil {
		return false
	}
	return last >= cutoff
}

func (c *Coordinator) replicaFileOK(workerID, chunkID string) bool {
	_, err := c.readMarketChunkFile(workerID, chunkID)
	return err == nil
}

func (c *Coordinator) recordReplicaHealth(orderID string, chunkIndex int, workerID string, ok bool) {
	now := time.Now().Unix()
	if ok {
		_, _ = c.db.Exec(`INSERT INTO hms_replica_health(order_id, chunk_index, worker_id, healthy, fail_count, slashed, last_ok_unix)
			VALUES(?,?,?,1,0,0,?)
			ON CONFLICT(order_id, chunk_index, worker_id) DO UPDATE SET healthy=1, fail_count=0, last_ok_unix=excluded.last_ok_unix`,
			orderID, chunkIndex, workerID, now)
		return
	}
	streak := c.cfg.HealthSlashStreak
	if streak <= 0 {
		streak = c.cfg.MaxStrikes
	}
	if streak <= 0 {
		streak = 3
	}
	_, _ = c.db.Exec(`INSERT INTO hms_replica_health(order_id, chunk_index, worker_id, healthy, fail_count, slashed, last_ok_unix)
		VALUES(?,?,?,0,1,0,0)
		ON CONFLICT(order_id, chunk_index, worker_id) DO UPDATE SET
			healthy=0,
			fail_count=fail_count+1,
			slashed=CASE WHEN fail_count+1>=? THEN 1 ELSE slashed END`,
		orderID, chunkIndex, workerID, streak)
}

// RecordStorageProofFailure marks replica unhealthy (slash after streak).
func (c *Coordinator) recordStorageProofFailure(workerID, chunkID string) {
	var orderID string
	var chunkIndex int
	err := c.db.QueryRow(`SELECT order_id, chunk_index FROM hms_order_chunks WHERE chunk_id=?`, chunkID).Scan(&orderID, &chunkIndex)
	if err != nil {
		return
	}
	c.recordReplicaHealth(orderID, chunkIndex, workerID, false)
}

// WorkerEligibleForStoragePayout returns false when worker has slashed market replicas.
func (c *Coordinator) WorkerEligibleForStoragePayout(workerID string) (bool, error) {
	var n int
	err := c.db.QueryRow(`SELECT COUNT(*) FROM hms_replica_health WHERE worker_id=? AND slashed=1`, workerID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

func (c *Coordinator) repairMissingReplicas() error {
	target := marketReplicaCount()
	rows, err := c.db.Query(`SELECT order_id, chunk_index, chunk_id FROM hms_order_chunks
		WHERE order_id IN (SELECT order_id FROM hms_orders WHERE status IN ('uploading','stored','degraded'))`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var orderID, chunkID string
		var chunkIndex int
		if err := rows.Scan(&orderID, &chunkIndex, &chunkID); err != nil {
			return err
		}
		replicas, err := c.listChunkReplicaWorkers(orderID, chunkIndex)
		if err != nil {
			continue
		}
		healthy := 0
		for _, wid := range replicas {
			online := c.workerOnline(wid)
			fileOK := c.replicaFileOK(wid, chunkID)
			if online && fileOK {
				c.recordReplicaHealth(orderID, chunkIndex, wid, true)
				healthy++
			} else {
				c.recordReplicaHealth(orderID, chunkIndex, wid, false)
			}
		}
		if healthy >= target {
			continue
		}
		_ = c.tryRepairChunk(orderID, chunkIndex, chunkID, replicas, target-healthy)
	}
	return rows.Err()
}

func (c *Coordinator) tryRepairChunk(orderID string, chunkIndex int, chunkID string, existing []string, need int) error {
	if need <= 0 {
		return nil
	}
	var src []byte
	var srcWorker string
	for _, wid := range existing {
		if !c.replicaFileOK(wid, chunkID) {
			continue
		}
		b, err := c.readMarketChunkFile(wid, chunkID)
		if err == nil && len(b) > 0 {
			src = b
			srcWorker = wid
			break
		}
	}
	if len(src) == 0 {
		return errors.New("no healthy source replica for repair")
	}
	existingSet := map[string]struct{}{}
	for _, w := range existing {
		existingSet[w] = struct{}{}
	}
	for repaired := 0; repaired < need; repaired++ {
		candidates, err := c.PickStorageWorkers(marketReplicaCount())
		if err != nil {
			return err
		}
		var picked string
		for _, wid := range candidates {
			if _, ok := existingSet[wid]; ok {
				continue
			}
			picked = wid
			break
		}
		if picked == "" {
			return fmt.Errorf("no spare online worker for repair")
		}
		if err := c.writeMarketChunkFile(picked, chunkID, src); err != nil {
			continue
		}
		existing = append(existing, picked)
		existingSet[picked] = struct{}{}
		_ = c.recordChunkReplicas(orderID, chunkIndex, existing)
		c.recordReplicaHealth(orderID, chunkIndex, picked, true)
		_, _ = c.db.Exec(`INSERT INTO hms_repair_log(order_id, chunk_index, chunk_id, from_worker, to_worker, bytes, created_unix)
			VALUES(?,?,?,?,?,?,?)`,
			orderID, chunkIndex, chunkID, srcWorker, picked, len(src), time.Now().Unix())
	}
	return nil
}

func (c *Coordinator) refreshOrderHealth() error {
	target := marketReplicaCount()
	rows, err := c.db.Query(`SELECT order_id FROM hms_orders WHERE status IN ('uploading','stored','degraded')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	now := time.Now().Unix()
	for rows.Next() {
		var orderID string
		if err := rows.Scan(&orderID); err != nil {
			return err
		}
		chunks, err := c.ListOrderChunks(orderID)
		if err != nil {
			continue
		}
		if len(chunks) == 0 {
			continue
		}
		totalHealthy := 0
		totalNeeded := 0
		for _, ch := range chunks {
			idx, _ := ch["chunk_index"].(int)
			chunkID, _ := ch["chunk_id"].(string)
			replicas, _ := c.listChunkReplicaWorkers(orderID, idx)
			need := target
			if need < 1 {
				need = 1
			}
			totalNeeded += need
			for _, wid := range replicas {
				if c.workerOnline(wid) && c.replicaFileOK(wid, chunkID) {
					totalHealthy++
				}
			}
		}
		health := HealthOK
		detail := ""
		status := OrderStatusStored
		if totalHealthy == 0 {
			health = HealthFailed
			status = OrderStatusFailed
			detail = "no healthy replicas — restore unavailable until repair succeeds"
		} else if totalHealthy < totalNeeded {
			health = HealthDegraded
			status = OrderStatusDegraded
			detail = fmt.Sprintf("healthy replicas %d/%d — restore may use surviving copy; repair queued", totalHealthy, totalNeeded)
		}
		var curStatus string
		_ = c.db.QueryRow(`SELECT status FROM hms_orders WHERE order_id=?`, orderID).Scan(&curStatus)
		if curStatus == OrderStatusDraft {
			status = curStatus
		}
		if curStatus == OrderStatusUploading && totalHealthy > 0 {
			status = OrderStatusUploading
		}
		_, _ = c.db.Exec(`UPDATE hms_orders SET health_status=?, health_detail=?, alert_unix=?, status=CASE WHEN status IN ('failed') THEN status ELSE ? END, updated_unix=? WHERE order_id=?`,
			health, detail, now, status, now, orderID)
	}
	return rows.Err()
}

// OrderHealth returns SLA fields for dashboard/API.
func (c *Coordinator) OrderHealth(orderID string) (map[string]any, error) {
	o, err := c.GetStorageOrder(orderID)
	if err != nil {
		return nil, err
	}
	chunks, _ := c.ListOrderChunks(orderID)
	return map[string]any{
		"order_id":       o.OrderID,
		"status":         o.Status,
		"health_status":  o.HealthStatus,
		"health_detail":  o.HealthDetail,
		"replica_target": marketReplicaCount(),
		"chunks":         chunks,
		"restore_ok":     o.HealthStatus != HealthFailed && o.Status != OrderStatusFailed,
	}, nil
}
