package lanpool

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

const rigOnlineSec = 45

// Registry tracks workers that POST /api/push_work (LAN pool).
type Registry struct {
	mu   sync.Mutex
	rigs map[string]*entry
}

type entry struct {
	WorkerID       string
	Name           string
	HashrateGHS    float64
	LastSeen       time.Time
	IP             string
	SharesAccepted uint64
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{rigs: make(map[string]*entry)}
}

// Upsert merges a heartbeat from a worker.
func (reg *Registry) Upsert(remoteAddr string, b PushWorkBody) error {
	id := strings.TrimSpace(b.WorkerID)
	if id == "" {
		return fmt.Errorf("worker_id required")
	}
	name := strings.TrimSpace(b.Name)
	if name == "" {
		name = id
	}
	ip := strings.TrimSpace(b.IP)
	if ip == "" {
		ip = hostFromRemoteAddr(remoteAddr)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	e, ok := reg.rigs[id]
	if !ok {
		e = &entry{WorkerID: id}
		reg.rigs[id] = e
	}
	e.Name = name
	if b.HashrateGHS > 0 {
		e.HashrateGHS = b.HashrateGHS
	}
	e.IP = ip
	e.LastSeen = time.Now()
	if b.SharesAccepted > 0 {
		e.SharesAccepted += b.SharesAccepted
	}
	if b.ShareAccepted != nil && *b.ShareAccepted {
		e.SharesAccepted++
	}
	return nil
}

// SetRigLastSeen updates LastSeen (for tests).
func (reg *Registry) SetRigLastSeen(workerID string, t time.Time) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if e, ok := reg.rigs[workerID]; ok {
		e.LastSeen = t
	}
}

// List returns a snapshot for metrics / network stats.
func (reg *Registry) List() []MetricsRow {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	now := time.Now()
	out := make([]MetricsRow, 0, len(reg.rigs))
	for _, e := range reg.rigs {
		online := now.Sub(e.LastSeen) < rigOnlineSec*time.Second
		out = append(out, MetricsRow{
			WorkerID:       e.WorkerID,
			Name:           e.Name,
			HashrateGHS:    e.HashrateGHS,
			LastSeenUnix:   e.LastSeen.Unix(),
			IP:             e.IP,
			Online:         online,
			Source:         "remote",
			SharesAccepted: e.SharesAccepted,
		})
	}
	return out
}

// ListOnline returns only online workers for live dashboards.
func (reg *Registry) ListOnline() []MetricsRow {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	now := time.Now()
	out := make([]MetricsRow, 0, len(reg.rigs))
	for _, e := range reg.rigs {
		online := now.Sub(e.LastSeen) < rigOnlineSec*time.Second
		if !online {
			continue
		}
		out = append(out, MetricsRow{
			WorkerID:       e.WorkerID,
			Name:           e.Name,
			HashrateGHS:    e.HashrateGHS,
			LastSeenUnix:   e.LastSeen.Unix(),
			IP:             e.IP,
			Online:         true,
			Source:         "remote",
			SharesAccepted: e.SharesAccepted,
		})
	}
	return out
}

// PruneOlderThan removes workers not seen for maxAge and returns removed worker IDs.
func (reg *Registry) PruneOlderThan(maxAge time.Duration) []string {
	if maxAge <= 0 {
		return nil
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	var removed []string
	for id, e := range reg.rigs {
		if e.LastSeen.Before(cutoff) {
			delete(reg.rigs, id)
			removed = append(removed, id)
		}
	}
	return removed
}

// SeedFromDBRow restores one row without bumping LastSeen clock (startup load).
func (reg *Registry) SeedFromDBRow(workerID, name string, gh float64, lastSeenUnix int64, ip string, shares uint64) {
	id := strings.TrimSpace(workerID)
	if id == "" {
		return
	}
	if name == "" {
		name = id
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	e, ok := reg.rigs[id]
	if !ok {
		e = &entry{WorkerID: id}
		reg.rigs[id] = e
	}
	e.Name = name
	if gh > 0 {
		e.HashrateGHS = gh
	}
	e.IP = ip
	e.SharesAccepted = shares
	if lastSeenUnix > 0 {
		e.LastSeen = time.Unix(lastSeenUnix, 0)
	} else {
		e.LastSeen = time.Unix(0, 0)
	}
}

// RowForPersist returns DB fields after Upsert (for SQLite).
func (reg *Registry) RowForPersist(workerID string) (name string, gh float64, lastUnix int64, ip string, shares uint64, ok bool) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	e, ok := reg.rigs[strings.TrimSpace(workerID)]
	if !ok {
		return "", 0, 0, "", 0, false
	}
	return e.Name, e.HashrateGHS, e.LastSeen.Unix(), e.IP, e.SharesAccepted, true
}

func hostFromRemoteAddr(remote string) string {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return strings.Trim(remote, "[]")
	}
	return host
}
