package poolfuzz

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/hunt"
)

// poolCorpusRow mirrors fuzz_pool_corpus scheduling state.
type poolCorpusRow struct {
	Input      uint64
	InputBytes []byte
	Energy     int
	Edge       int
	Path       int
	Crash      bool
}

func (s *Service) loadPoolCorpusSeeds(ctx context.Context, campaignID string, max int) ([]fuzzengine.PoolCorpusSeed, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("poolfuzz: no database")
	}
	if max <= 0 {
		max = 256
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT input_u64, input_bytes, energy, edge_bucket, path_bucket, is_crash
		   FROM fuzz_pool_corpus
		  WHERE campaign_id=? AND is_crash=0
		  ORDER BY energy DESC, last_seen_at DESC
		  LIMIT ?`, campaignID, max)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []fuzzengine.PoolCorpusSeed
	for rows.Next() {
		var r poolCorpusRow
		var crash int
		var inputSigned int64
		if err := rows.Scan(&inputSigned, &r.InputBytes, &r.Energy, &r.Edge, &r.Path, &crash); err != nil {
			return nil, err
		}
		r.Input = uint64(inputSigned)
		r.Crash = crash != 0
		out = append(out, fuzzengine.PoolCorpusSeed{
			Input: r.Input, InputBytes: append([]byte(nil), r.InputBytes...), Energy: r.Energy, Edge: r.Edge, Path: r.Path, Crash: r.Crash,
		})
	}
	return out, rows.Err()
}

func (s *Service) seedPoolCorpusFromConfig(ctx context.Context, campaignID string, cfg map[string]any, now int64) error {
	if s == nil || s.DB == nil || !fuzzengine.GuidedSchedulingEnabled(cfg) {
		return nil
	}
	if fuzzengine.ParseInputMode(cfg) == fuzzengine.InputModeBytes {
		for _, b := range fuzzengine.ParseByteCorpus(cfg) {
			u := fuzzengine.PackInputBytesToU64(b)
			edge, path := fuzzengine.CoverageBucketsFromBytes(b)
			if err := s.upsertPoolCorpusSeed(ctx, campaignID, u, b, 2, edge, path, false, now); err != nil {
				return err
			}
		}
		return nil
	}
	for _, in := range fuzzengine.ParseSeedCorpus(cfg) {
		edge, path := fuzzengine.CoverageBuckets(in)
		if err := s.upsertPoolCorpusSeed(ctx, campaignID, in, nil, 2, edge, path, false, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) upsertPoolCorpusSeed(ctx context.Context, campaignID string, input uint64, inputBytes []byte, energy, edge, path int, crash bool, now int64) error {
	if energy < 1 {
		energy = 1
	}
	crashInt := 0
	if crash {
		crashInt = 1
	}
	if inputBytes == nil {
		inputBytes = []byte{}
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO fuzz_pool_corpus
		 (campaign_id, input_u64, input_bytes, energy, edge_bucket, path_bucket, is_crash, first_seen_at, last_seen_at, exec_count)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
		 ON CONFLICT(campaign_id, input_u64) DO UPDATE SET
		   energy=MAX(fuzz_pool_corpus.energy, excluded.energy),
		   edge_bucket=excluded.edge_bucket,
		   path_bucket=excluded.path_bucket,
		   is_crash=MAX(fuzz_pool_corpus.is_crash, excluded.is_crash),
		   input_bytes=CASE
		     WHEN length(excluded.input_bytes) > length(fuzz_pool_corpus.input_bytes) THEN excluded.input_bytes
		     ELSE fuzz_pool_corpus.input_bytes END,
		   last_seen_at=excluded.last_seen_at,
		   exec_count=fuzz_pool_corpus.exec_count+1`,
		campaignID, poolCorpusU64Arg(input), inputBytes, energy, edge, path, crashInt, now, now)
	return err
}

func (s *Service) cullPoolCorpus(ctx context.Context, campaignID string, max int) error {
	if max <= 0 {
		return nil
	}
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM fuzz_pool_corpus
		  WHERE campaign_id=? AND rowid NOT IN (
		    SELECT rowid FROM fuzz_pool_corpus
		     WHERE campaign_id=?
		     ORDER BY is_crash DESC, energy DESC, last_seen_at DESC
		     LIMIT ?
		  )`, campaignID, campaignID, max)
	return err
}

func (s *Service) observePoolCorpus(ctx context.Context, campaignID string, input uint64, inputBytes []byte, recordFinding bool, now int64) error {
	if s == nil || s.DB == nil {
		return nil
	}
	var cfgJSON string
	if err := s.DB.QueryRowContext(ctx, `SELECT config_json FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&cfgJSON); err != nil {
		return err
	}
	cfg := parseConfigJSON(cfgJSON)
	if !fuzzengine.GuidedSchedulingEnabled(cfg) {
		return nil
	}
	var edge, path int
	if len(inputBytes) > 0 {
		edge, path = fuzzengine.CoverageBucketsFromBytes(inputBytes)
	} else {
		edge, path = fuzzengine.CoverageBuckets(input)
	}
	newEdge, err := s.coverageBucketNew(ctx, campaignID, "edge", edge, now)
	if err != nil {
		return err
	}
	newPath, err := s.coverageBucketNew(ctx, campaignID, "path", path, now)
	if err != nil {
		return err
	}
	boost := fuzzengine.CorpusObserveBoost(recordFinding, newEdge, newPath)
	crash := recordFinding
	if err := s.upsertPoolCorpusSeed(ctx, campaignID, input, inputBytes, boost, edge, path, crash, now); err != nil {
		return err
	}
	if err := s.exportNamespaceCorpus(ctx, cfg, input, inputBytes, boost, edge, path, crash, now); err != nil {
		return err
	}
	return s.cullPoolCorpus(ctx, campaignID, fuzzengine.PoolCorpusMax(cfg))
}

func (s *Service) coverageBucketNew(ctx context.Context, campaignID, kind string, bucket int, now int64) (bool, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT OR IGNORE INTO fuzz_coverage_seen (campaign_id, kind, bucket, first_seen_at) VALUES (?, ?, ?, ?)`,
		campaignID, kind, bucket, now)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Service) poolCorpusSize(ctx context.Context, campaignID string) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM fuzz_pool_corpus WHERE campaign_id=?`, campaignID).Scan(&n)
	return n, err
}

func (s *Service) storeExpectedInputs(ctx context.Context, campaignID string, itemID int64, inputU uint64, inputB []byte) error {
	if inputB == nil {
		inputB = []byte{}
	}
	_, err := s.DB.ExecContext(ctx,
		`UPDATE fuzz_work_items
		    SET expected_input_u64=?, expected_input_bytes=?, expected_input_locked=1, updated_at=?
		  WHERE id=? AND campaign_id=?`,
		poolCorpusU64Arg(inputU), inputB, time.Now().Unix(), itemID, campaignID)
	return err
}

func (s *Service) loadExpectedInputs(ctx context.Context, campaignID string, itemID int64) (inputU uint64, inputB []byte, locked bool, err error) {
	var u int64
	var lockedInt int
	err = s.DB.QueryRowContext(ctx,
		`SELECT expected_input_u64, expected_input_bytes, expected_input_locked
		   FROM fuzz_work_items WHERE id=? AND campaign_id=?`, itemID, campaignID).
		Scan(&u, &inputB, &lockedInt)
	if err == sql.ErrNoRows {
		return 0, nil, false, fmt.Errorf("poolfuzz: work item not found")
	}
	if err != nil {
		return 0, nil, false, err
	}
	return uint64(u), inputB, lockedInt != 0, nil
}

func (s *Service) storeCorpusSnapshot(ctx context.Context, campaignID string, itemID int64, seeds []fuzzengine.PoolCorpusSeed) error {
	if len(seeds) == 0 {
		return nil
	}
	jsonBytes, sha, err := fuzzengine.EncodeCorpusSnapshot(seeds)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx,
		`UPDATE fuzz_work_items
		    SET corpus_snapshot_json=?, corpus_snapshot_sha256=?, updated_at=?
		  WHERE id=? AND campaign_id=?`,
		jsonBytes, sha, time.Now().Unix(), itemID, campaignID)
	return err
}

func (s *Service) loadCorpusSnapshot(ctx context.Context, campaignID string, itemID int64) ([]fuzzengine.PoolCorpusSeed, error) {
	var jsonBytes []byte
	err := s.DB.QueryRowContext(ctx,
		`SELECT corpus_snapshot_json FROM fuzz_work_items WHERE id=? AND campaign_id=?`, itemID, campaignID).
		Scan(&jsonBytes)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("poolfuzz: work item not found")
	}
	if err != nil {
		return nil, err
	}
	return fuzzengine.DecodeCorpusSnapshot(jsonBytes)
}

// EnsureGuidedCorpusSeeded idempotently seeds fuzz_pool_corpus from campaign config.
func (s *Service) EnsureGuidedCorpusSeeded(ctx context.Context, campaignID string, cfg map[string]any, now int64) error {
	if s == nil || s.DB == nil || !fuzzengine.GuidedSchedulingEnabled(cfg) {
		return nil
	}
	if IsHuntCampaign(cfg) {
		targetID := strings.TrimSpace(jsonString(cfg["upstream_target_id"]))
		if targetID != "" {
			if _, err := hunt.MergeLibFuzzerSeedCorpus(cfg, hunt.RepoRoot(), targetID); err != nil {
				return err
			}
		}
	}
	n, err := s.poolCorpusSize(ctx, campaignID)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if err := s.seedPoolCorpusFromConfig(ctx, campaignID, cfg, now); err != nil {
		return err
	}
	return s.importNamespaceCorpus(ctx, campaignID, cfg, now)
}

func (s *Service) loadNamespaceCorpusSeeds(ctx context.Context, namespace string, max int) ([]fuzzengine.PoolCorpusSeed, error) {
	if s == nil || s.DB == nil || namespace == "" {
		return nil, nil
	}
	if max <= 0 {
		max = 64
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT input_u64, input_bytes, energy, edge_bucket, path_bucket, is_crash
		   FROM fuzz_corpus_namespace
		  WHERE namespace=? AND is_crash=0
		  ORDER BY energy DESC, last_seen_at DESC
		  LIMIT ?`, namespace, max)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []fuzzengine.PoolCorpusSeed
	for rows.Next() {
		var r poolCorpusRow
		var crash int
		var inputSigned int64
		if err := rows.Scan(&inputSigned, &r.InputBytes, &r.Energy, &r.Edge, &r.Path, &crash); err != nil {
			return nil, err
		}
		r.Input = uint64(inputSigned)
		r.Crash = crash != 0
		out = append(out, fuzzengine.PoolCorpusSeed{
			Input: r.Input, InputBytes: append([]byte(nil), r.InputBytes...), Energy: r.Energy, Edge: r.Edge, Path: r.Path, Crash: r.Crash,
		})
	}
	return out, rows.Err()
}

func (s *Service) importNamespaceCorpus(ctx context.Context, campaignID string, cfg map[string]any, now int64) error {
	if !fuzzengine.CorpusPersistEnabled(cfg) {
		return nil
	}
	ns := fuzzengine.CorpusPersistNamespace(cfg)
	if ns == "" {
		return nil
	}
	seeds, err := s.loadNamespaceCorpusSeeds(ctx, ns, fuzzengine.CorpusPersistMax(cfg))
	if err != nil {
		return err
	}
	for _, seed := range seeds {
		if err := s.upsertPoolCorpusSeed(ctx, campaignID, seed.Input, seed.InputBytes, seed.Energy, seed.Edge, seed.Path, seed.Crash, now); err != nil {
			return err
		}
	}
	if len(seeds) > 0 {
		return s.cullPoolCorpus(ctx, campaignID, fuzzengine.PoolCorpusMax(cfg))
	}
	return nil
}

func (s *Service) exportNamespaceCorpus(ctx context.Context, cfg map[string]any, input uint64, inputBytes []byte, energy, edge, path int, crash bool, now int64) error {
	if s == nil || s.DB == nil || crash || !fuzzengine.CorpusPersistEnabled(cfg) {
		return nil
	}
	ns := fuzzengine.CorpusPersistNamespace(cfg)
	if ns == "" {
		return nil
	}
	if energy < 1 {
		energy = 1
	}
	if inputBytes == nil {
		inputBytes = []byte{}
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO fuzz_corpus_namespace
		 (namespace, input_u64, input_bytes, energy, edge_bucket, path_bucket, is_crash, first_seen_at, last_seen_at, exec_count)
		 VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, 1)
		 ON CONFLICT(namespace, input_u64) DO UPDATE SET
		   energy=MAX(fuzz_corpus_namespace.energy, excluded.energy),
		   edge_bucket=excluded.edge_bucket,
		   path_bucket=excluded.path_bucket,
		   input_bytes=CASE
		     WHEN length(excluded.input_bytes) > length(fuzz_corpus_namespace.input_bytes) THEN excluded.input_bytes
		     ELSE fuzz_corpus_namespace.input_bytes END,
		   last_seen_at=excluded.last_seen_at,
		   exec_count=fuzz_corpus_namespace.exec_count+1`,
		ns, poolCorpusU64Arg(input), inputBytes, energy, edge, path, now, now)
	return err
}

// UpsertNamespaceCorpusSeeds merges coordinator/node namespace corpus rows.
func (s *Service) UpsertNamespaceCorpusSeeds(ctx context.Context, namespace string, seeds []fuzzengine.PoolCorpusSeed, now int64) error {
	if s == nil || s.DB == nil || namespace == "" {
		return nil
	}
	for _, seed := range seeds {
		if seed.Crash {
			continue
		}
		energy := seed.Energy
		if energy < 1 {
			energy = 1
		}
		inputBytes := seed.InputBytes
		if inputBytes == nil {
			inputBytes = []byte{}
		}
		_, err := s.DB.ExecContext(ctx,
			`INSERT INTO fuzz_corpus_namespace
			 (namespace, input_u64, input_bytes, energy, edge_bucket, path_bucket, is_crash, first_seen_at, last_seen_at, exec_count)
			 VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, 1)
			 ON CONFLICT(namespace, input_u64) DO UPDATE SET
			   energy=MAX(fuzz_corpus_namespace.energy, excluded.energy),
			   edge_bucket=excluded.edge_bucket,
			   path_bucket=excluded.path_bucket,
			   input_bytes=CASE
			     WHEN length(excluded.input_bytes) > length(fuzz_corpus_namespace.input_bytes) THEN excluded.input_bytes
			     ELSE fuzz_corpus_namespace.input_bytes END,
			   last_seen_at=excluded.last_seen_at,
			   exec_count=fuzz_corpus_namespace.exec_count+1`,
			namespace, poolCorpusU64Arg(seed.Input), inputBytes, energy, seed.Edge, seed.Path, now, now)
		if err != nil {
			return err
		}
	}
	return nil
}

// ListNamespaceCorpus returns persisted cross-campaign seeds for a namespace.
func (s *Service) ListNamespaceCorpus(ctx context.Context, namespace string, max int) ([]fuzzengine.PoolCorpusSeed, error) {
	return s.loadNamespaceCorpusSeeds(ctx, namespace, max)
}

// SeedsForWorkItem returns frozen corpus seeds for submit verify (snapshot at claim; no live fallback).
func (s *Service) SeedsForWorkItem(ctx context.Context, campaignID string, itemID int64, cfg map[string]any) ([]fuzzengine.PoolCorpusSeed, error) {
	guided := fuzzengine.GuidedSchedulingEnabled(cfg)
	if IsHuntCampaign(cfg) {
		if !hunt.HuntCorpusGuided(cfg) {
			return nil, nil
		}
		guided = true
	}
	if !guided && PoolExecPerUnit(cfg) <= 1 && !IsHuntCampaign(cfg) {
		return nil, nil
	}
	seeds, err := s.loadCorpusSnapshot(ctx, campaignID, itemID)
	if err != nil {
		return nil, err
	}
	if guided && len(seeds) == 0 {
		return nil, fmt.Errorf("poolfuzz: missing guided corpus snapshot for work item %d", itemID)
	}
	return seeds, nil
}

// poolCorpusU64Arg binds uint64 pool inputs for SQLite INTEGER (signed int64 wire format).
func poolCorpusU64Arg(input uint64) int64 {
	return int64(input)
}
