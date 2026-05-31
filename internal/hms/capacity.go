package hms

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ErrInsufficientCapacity is returned when the lane cannot accept a new order size.
var ErrInsufficientCapacity = errors.New("insufficient network storage capacity")

// CapacitySnapshot aggregates online storage headroom for market preflight.
type CapacitySnapshot struct {
	TotalQuotaBytes  int64   `json:"total_quota_bytes"`
	UsedBytes        int64   `json:"used_bytes"`
	FreeBytes        int64   `json:"free_bytes"`
	TotalQuotaGB     float64 `json:"total_quota_gb"`
	FreeGB           float64 `json:"free_gb"`
	UsedGB           float64 `json:"used_gb"`
	OnlineWorkers    int     `json:"online_workers"`
	TotalWorkers     int     `json:"total_storage_workers"`
	ReplicaTarget    int     `json:"replica_target"`
	WorkerOnlineSec  int64   `json:"worker_online_sec"`
	OnlineCutoffUnix int64   `json:"online_cutoff_unix"`
}

func (c *Coordinator) workerOnlineCutoff() int64 {
	sec := c.cfg.WorkerOnlineSec
	if sec <= 0 {
		sec = 300
	}
	return time.Now().Unix() - sec
}

// NetworkCapacity returns aggregate free space from online storage workers only.
func (c *Coordinator) NetworkCapacity() (CapacitySnapshot, error) {
	cutoff := c.workerOnlineCutoff()
	var snap CapacitySnapshot
	snap.ReplicaTarget = marketReplicaCount()
	snap.WorkerOnlineSec = c.cfg.WorkerOnlineSec
	if snap.WorkerOnlineSec <= 0 {
		snap.WorkerOnlineSec = 300
	}
	snap.OnlineCutoffUnix = cutoff

	_ = c.db.QueryRow(`SELECT COUNT(*) FROM hms_workers WHERE role='storage' AND quota_gb > 0`).Scan(&snap.TotalWorkers)

	rows, err := c.db.Query(`
		SELECT w.worker_id, w.quota_gb,
			COALESCE((SELECT SUM(c.size) FROM hms_chunks c WHERE c.worker_id=w.worker_id), 0)
		FROM hms_workers w
		WHERE w.role='storage' AND w.quota_gb > 0 AND w.last_seen_unix >= ?
		ORDER BY w.worker_id`, cutoff)
	if err != nil {
		return snap, err
	}
	defer rows.Close()

	const gb = int64(1024 * 1024 * 1024)
	for rows.Next() {
		var wid string
		var quotaGB int
		var used int64
		if err := rows.Scan(&wid, &quotaGB, &used); err != nil {
			return snap, err
		}
		quotaBytes := int64(quotaGB) * gb
		snap.OnlineWorkers++
		snap.TotalQuotaBytes += quotaBytes
		snap.UsedBytes += used
		free := quotaBytes - used
		if free > 0 {
			snap.FreeBytes += free
		}
	}
	if err := rows.Err(); err != nil {
		return snap, err
	}
	snap.TotalQuotaGB = float64(snap.TotalQuotaBytes) / float64(gb)
	snap.FreeGB = float64(snap.FreeBytes) / float64(gb)
	snap.UsedGB = float64(snap.UsedBytes) / float64(gb)
	return snap, nil
}

// RequiredCapacityBytes is conservative headroom for a new order (plan × replica target).
func RequiredCapacityBytes(sizePlanBytes int64) int64 {
	if sizePlanBytes < 0 {
		sizePlanBytes = 0
	}
	n := int64(marketReplicaCount())
	if n < 1 {
		n = 1
	}
	return sizePlanBytes * n
}

// EnsureCapacity verifies online workers can accept additional bytes.
func (c *Coordinator) EnsureCapacity(additionalBytes int64) (CapacitySnapshot, error) {
	snap, err := c.NetworkCapacity()
	if err != nil {
		return snap, err
	}
	if snap.OnlineWorkers == 0 {
		return snap, fmt.Errorf("%w: no online storage workers (register workerstorage and keep heartbeat)", ErrInsufficientCapacity)
	}
	if additionalBytes <= 0 {
		return snap, nil
	}
	if snap.FreeBytes < additionalBytes {
		return snap, fmt.Errorf("%w: need %d bytes free, have %d bytes across %d online worker(s)",
			ErrInsufficientCapacity, additionalBytes, snap.FreeBytes, snap.OnlineWorkers)
	}
	return snap, nil
}

func isInsufficientCapacity(err error) bool {
	return errors.Is(err, ErrInsufficientCapacity)
}

func marketHTTPStatus(err error) int {
	if isInsufficientCapacity(err) {
		return http.StatusServiceUnavailable
	}
	return http.StatusBadRequest
}
