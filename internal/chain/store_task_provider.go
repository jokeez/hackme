package chain

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"
)

// ordersViaPoolOnly is set on the chain command node when open orders must be solved
// only via the public pool (coordinator claim → worker → POST /api/poh/solve-order).
// Local PoH mining still runs for baseline blocks using the internal task fallback.
func ordersViaPoolOnly() bool {
	v := strings.TrimSpace(os.Getenv("HACKME_CHAIN_LEADER_ORDERS_VIA_POOL_ONLY"))
	if v == "" {
		v = strings.TrimSpace(os.Getenv("HACKME_ORDERS_POOL_ONLY"))
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// StoreTaskProvider prefers open paid tasks from SQLite; otherwise falls back to file/internal.
type StoreTaskProvider struct {
	svc      *Service
	fallback TaskProvider
}

// NewStoreTaskProvider returns a TaskProvider that serves POST /api/tasks orders first.
func NewStoreTaskProvider(svc *Service, fallback TaskProvider) *StoreTaskProvider {
	if fallback == nil {
		fallback = InternalTaskProvider{}
	}
	return &StoreTaskProvider{svc: svc, fallback: fallback}
}

// Snapshot returns the oldest open paid order (reward > 0), else fallback chain.
func (p *StoreTaskProvider) Snapshot(ctx context.Context) (TaskSpec, error) {
	if p.svc == nil {
		return p.fallback.Snapshot(ctx)
	}
	if err := p.svc.ExpireOpenOrderTasks(ctx); err != nil {
		return TaskSpec{}, err
	}
	if ordersViaPoolOnly() {
		return p.fallback.Snapshot(ctx)
	}
	var id, artifact, manifest, kindStr string
	var reward float64
	var created int64
	nowUnix := time.Now().Unix()
	err := p.svc.db.QueryRowContext(ctx,
		`SELECT id, artifact_hash, reward, manifest_json, kind, created_at FROM tasks
		 WHERE status = ? AND reward > 0 AND (expires_at = 0 OR expires_at > ?) ORDER BY created_at ASC LIMIT 1`,
		TaskStatusOpen, nowUnix,
	).Scan(&id, &artifact, &reward, &manifest, &kindStr, &created)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return p.fallback.Snapshot(ctx)
		}
		return TaskSpec{}, err
	}
	var m orderManifestJSON
	if err := json.Unmarshal([]byte(manifest), &m); err != nil {
		return TaskSpec{}, err
	}
	kind := TaskKind(strings.TrimSpace(kindStr))
	if kind == "" {
		kind = TaskKindSyntheticPoH
	}
	spec := TaskSpec{
		ID:           id,
		Kind:         kind,
		ArtifactHash: strings.TrimSpace(artifact),
		RewardHMC:    reward,
		Source:       TaskSourceOrder,
		ManifestPath: "db:" + id,
		TimeoutMS:    m.TimeoutMS,
	}
	if m.TimeoutMS > 0 {
		spec.Timeout = time.Duration(m.TimeoutMS) * time.Millisecond
	}
	if wb, err := p.svc.WasmCheckFromManifestJSON([]byte(manifest)); err != nil {
		return TaskSpec{}, err
	} else {
		spec.WasmCheck = wb
	}
	return spec, nil
}
