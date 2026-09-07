package main

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"

	"hackme/internal/lanpool"
)

// peerPersistFlusher coalesces lan_peer_rigs SQLite upserts so PoH submit/push_work
// do not block on a disk write every acknowledgement.
type peerPersistFlusher struct {
	db       *sql.DB
	reg      *lanpool.Registry
	mu       sync.Mutex
	dirty    map[string]struct{}
	interval time.Duration
}

func newPeerPersistFlusher(db *sql.DB, reg *lanpool.Registry, interval time.Duration) *peerPersistFlusher {
	if interval < time.Second {
		interval = time.Second
	}
	if interval > 10*time.Second {
		interval = 10 * time.Second
	}
	return &peerPersistFlusher{
		db:       db,
		reg:      reg,
		dirty:    make(map[string]struct{}, 64),
		interval: interval,
	}
}

func (f *peerPersistFlusher) mark(workerID string) {
	if f == nil || f.db == nil || f.reg == nil {
		return
	}
	id := strings.TrimSpace(workerID)
	if id == "" {
		return
	}
	f.mu.Lock()
	f.dirty[id] = struct{}{}
	f.mu.Unlock()
}

func (f *peerPersistFlusher) start(ctx context.Context) {
	if f == nil || f.db == nil {
		return
	}
	go func() {
		t := time.NewTicker(f.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				f.flush(context.Background())
				return
			case <-t.C:
				f.flush(ctx)
			}
		}
	}()
}

func (f *peerPersistFlusher) flush(ctx context.Context) {
	if f == nil || f.db == nil || f.reg == nil {
		return
	}
	f.mu.Lock()
	if len(f.dirty) == 0 {
		f.mu.Unlock()
		return
	}
	ids := make([]string, 0, len(f.dirty))
	for id := range f.dirty {
		ids = append(ids, id)
	}
	f.dirty = make(map[string]struct{}, 64)
	f.mu.Unlock()

	for _, id := range ids {
		persistPeer(ctx, f.db, id, f.reg)
	}
}

func (f *peerPersistFlusher) dirtyCount() int {
	if f == nil {
		return 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.dirty)
}
