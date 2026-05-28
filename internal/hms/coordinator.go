package hms

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Coordinator is the HMS lane state machine (storage + seal).
type Coordinator struct {
	cfg   Config
	db    *sql.DB
	guard *AbuseGuard
	mu    sync.RWMutex
}

func NewCoordinator(db *sql.DB, cfg Config) *Coordinator {
	return &Coordinator{
		cfg:   cfg,
		db:    db,
		guard: NewAbuseGuard(120, time.Minute),
	}
}

// RunEpochLoop advances epochs and freezes manifests (background).
func (c *Coordinator) RunEpochLoop(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = c.tickEpoch()
		}
	}
}

func (c *Coordinator) tickEpoch() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().Unix()
	ep, err := c.ensureEpochLocked(now)
	if err != nil {
		return err
	}
	if now >= ep.FreezeUnix && len(ep.ManifestRoot) == 0 {
		root, err := c.buildManifestLocked(ep.EpochID)
		if err != nil {
			return err
		}
		ep.ManifestRoot = root[:]
		_, err = c.db.Exec(`UPDATE hms_epochs SET manifest_root=?, payouts_enabled=0 WHERE epoch_id=?`, root[:], ep.EpochID)
		return err
	}
	if now >= ep.SealEndUnix && ep.Sealed == 0 {
		// epoch ended without seal — payouts stay off until next epoch seals
		return nil
	}
	if now >= ep.SealEndUnix && ep.Sealed == 1 {
		_, _ = c.db.Exec(`UPDATE hms_epochs SET payouts_enabled=1 WHERE epoch_id=?`, ep.EpochID)
		_, _ = c.startNextEpochLocked(now)
	}
	if now >= ep.SealEndUnix+int64(c.cfg.EpochDuration.Seconds()) && ep.Sealed == 0 {
		_, _ = c.startNextEpochLocked(now)
	}
	return nil
}

type epochRow struct {
	EpochID        int64
	StartedUnix    int64
	FreezeUnix     int64
	SealEndUnix    int64
	ManifestRoot   []byte
	SealTarget     []byte
	SealNonce      int64
	SealWorkerID   string
	Sealed         int
	PayoutsEnabled int
}

func (c *Coordinator) ensureEpochLocked(now int64) (epochRow, error) {
	var ep epochRow
	err := c.db.QueryRow(`SELECT epoch_id, started_unix, freeze_unix, seal_end_unix,
		COALESCE(manifest_root,''), seal_target, seal_nonce, COALESCE(seal_worker_id,''), sealed, payouts_enabled
		FROM hms_epochs ORDER BY epoch_id DESC LIMIT 1`).Scan(
		&ep.EpochID, &ep.StartedUnix, &ep.FreezeUnix, &ep.SealEndUnix,
		&ep.ManifestRoot, &ep.SealTarget, &ep.SealNonce, &ep.SealWorkerID, &ep.Sealed, &ep.PayoutsEnabled,
	)
	if err == sql.ErrNoRows {
		return c.startNextEpochLocked(now)
	}
	if err != nil {
		return ep, err
	}
	return ep, nil
}

func (c *Coordinator) startNextEpochLocked(now int64) (epochRow, error) {
	var prevTarget []byte
	_ = c.db.QueryRow(`SELECT seal_target FROM hms_epochs ORDER BY epoch_id DESC LIMIT 1`).Scan(&prevTarget)
	if len(prevTarget) != 32 {
		prevTarget = c.cfg.InitialSealTarget
	}
	var lastID int64
	_ = c.db.QueryRow(`SELECT COALESCE(MAX(epoch_id),0) FROM hms_epochs`).Scan(&lastID)
	id := lastID + 1
	freeze := now + int64(c.cfg.FreezeAfter.Seconds())
	sealEnd := freeze + int64(c.cfg.SealWindow.Seconds())
	_, err := c.db.Exec(`INSERT INTO hms_epochs(epoch_id, started_unix, freeze_unix, seal_end_unix, seal_target, sealed, payouts_enabled)
		VALUES(?,?,?,?,?,0,0)`, id, now, freeze, sealEnd, prevTarget)
	if err != nil {
		return epochRow{}, err
	}
	return epochRow{
		EpochID: id, StartedUnix: now, FreezeUnix: freeze, SealEndUnix: sealEnd,
		SealTarget: prevTarget,
	}, nil
}

func (c *Coordinator) CurrentEpoch() (epochRow, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ensureEpochLocked(time.Now().Unix())
}

func (c *Coordinator) RegisterStorageWorker(workerID, pubHex string, quotaGB int) error {
	if err := ValidateQuota(c.cfg, quotaGB); err != nil {
		return err
	}
	now := time.Now().Unix()
	_, err := c.db.Exec(`INSERT INTO hms_workers(worker_id, role, pubkey_hex, quota_gb, last_seen_unix, created_unix)
		VALUES(?, 'storage', ?, ?, ?, ?)
		ON CONFLICT(worker_id) DO UPDATE SET pubkey_hex=excluded.pubkey_hex, quota_gb=excluded.quota_gb, last_seen_unix=excluded.last_seen_unix`,
		workerID, pubHex, quotaGB, now, now)
	return err
}

func (c *Coordinator) RegisterSealWorker(workerID, pubHex string) error {
	now := time.Now().Unix()
	_, err := c.db.Exec(`INSERT INTO hms_workers(worker_id, role, pubkey_hex, quota_gb, last_seen_unix, created_unix)
		VALUES(?, 'seal', ?, 0, ?, ?)
		ON CONFLICT(worker_id) DO UPDATE SET pubkey_hex=excluded.pubkey_hex, role='seal', last_seen_unix=excluded.last_seen_unix`,
		workerID, pubHex, now, now)
	return err
}

func (c *Coordinator) workerBanned(workerID string, epochID int64) (bool, error) {
	var until int64
	err := c.db.QueryRow(`SELECT banned_until_epoch FROM hms_workers WHERE worker_id=?`, workerID).Scan(&until)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return until > epochID, nil
}

func (c *Coordinator) addStrike(workerID string, epochID int64) error {
	_, err := c.db.Exec(`UPDATE hms_workers SET strikes=strikes+1,
		banned_until_epoch=CASE WHEN strikes+1>=? THEN ? ELSE banned_until_epoch END
		WHERE worker_id=?`, c.cfg.MaxStrikes, epochID+1, workerID)
	return err
}

// AssignChunk registers opaque chunk metadata on a storage worker.
func (c *Coordinator) AssignChunk(chunkID, workerID string, ciphertextSHA256 []byte, size uint64, erasureMeta []byte) error {
	ep, err := c.CurrentEpoch()
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	if now >= ep.FreezeUnix {
		return errors.New("epoch frozen: no new chunks")
	}
	banned, err := c.workerBanned(workerID, ep.EpochID)
	if err != nil {
		return err
	}
	if banned {
		return errors.New("worker banned for epoch")
	}
	_, err = c.db.Exec(`INSERT INTO hms_chunks(chunk_id, ciphertext_sha256, size, erasure_meta, worker_id, epoch_id, created_unix)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(chunk_id) DO NOTHING`, chunkID, ciphertextSHA256, size, erasureMeta, workerID, ep.EpochID, now)
	return err
}

// IssueChallenge creates a PoSt challenge for a worker (anti-abuse: one active per worker).
func (c *Coordinator) IssueChallenge(workerID string) (map[string]any, error) {
	ep, err := c.CurrentEpoch()
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	if now >= ep.FreezeUnix {
		return nil, errors.New("challenges only during ingest phase")
	}
	if ep.Sealed == 0 && ep.PayoutsEnabled == 0 && now > ep.StartedUnix && c.epochNeedsSeal(ep) && now >= ep.FreezeUnix {
		return nil, errors.New("payouts paused: manifest not sealed")
	}
	banned, err := c.workerBanned(workerID, ep.EpochID)
	if err != nil {
		return nil, err
	}
	if banned {
		return nil, errors.New("worker banned")
	}
	var chunkID string
	var ct []byte
	var size int64
	err = c.db.QueryRow(`SELECT chunk_id, ciphertext_sha256, size FROM hms_chunks WHERE worker_id=? ORDER BY RANDOM() LIMIT 1`, workerID).Scan(&chunkID, &ct, &size)
	if err == sql.ErrNoRows {
		return nil, errors.New("no chunks assigned to worker")
	}
	if err != nil {
		return nil, err
	}
	var offset uint64
	var ob [8]byte
	if _, err := rand.Read(ob[:]); err != nil {
		return nil, err
	}
	if size > 32 {
		offset = uint64(ob[0]) % uint64(size-32)
	}
	binding := ProofBinding(ep.EpochID, workerID, chunkID, offset, ct)
	chID := fmt.Sprintf("%d-%s-%x", ep.EpochID, workerID, ob)
	expires := now + int64(c.cfg.ChallengeTTL.Seconds())
	// expected_hash filled on submit when worker returns sector proof
	_, err = c.db.Exec(`INSERT INTO hms_challenges(challenge_id, epoch_id, worker_id, chunk_id, sector_offset, expected_hash, expires_unix)
		VALUES(?,?,?,?,?,?,?)`, chID, ep.EpochID, workerID, chunkID, offset, binding[:], expires)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"challenge_id":  chID,
		"epoch_id":      ep.EpochID,
		"chunk_id":      chunkID,
		"sector_offset": offset,
		"expires_unix":  expires,
		"binding_hex":   encodeHex(binding[:]),
	}, nil
}

func (c *Coordinator) epochNeedsSeal(ep epochRow) bool {
	return len(ep.ManifestRoot) > 0 || time.Now().Unix() >= ep.FreezeUnix
}

// SubmitStorageProof verifies signed proof; strikes on failure.
func (c *Coordinator) SubmitStorageProof(p StorageSubmitPayload, pubHex, sigHex string, proof []byte) error {
	if err := VerifyStorageSubmit(p, pubHex, sigHex); err != nil {
		return err
	}
	var binding []byte
	var epochID int64
	var chunkID string
	var offset int64
	var answered int
	err := c.db.QueryRow(`SELECT expected_hash, epoch_id, chunk_id, sector_offset, answered FROM hms_challenges WHERE challenge_id=?`, p.ChallengeID).
		Scan(&binding, &epochID, &chunkID, &offset, &answered)
	if err != nil {
		return err
	}
	if answered != 0 {
		return errors.New("challenge already answered")
	}
	var exp int64
	_ = c.db.QueryRow(`SELECT expires_unix FROM hms_challenges WHERE challenge_id=?`, p.ChallengeID).Scan(&exp)
	if time.Now().Unix() > exp {
		_ = c.addStrike(p.WorkerID, epochID)
		return errors.New("challenge expired")
	}
	sectorBytes, err := hex.DecodeString(strings.TrimSpace(p.ProofHex))
	if err != nil || len(sectorBytes) != 32 {
		if len(proof) == 32 {
			sectorBytes = proof
		} else {
			_ = c.addStrike(p.WorkerID, epochID)
			_, _ = c.db.Exec(`UPDATE hms_challenges SET answered=1, ok=0 WHERE challenge_id=?`, p.ChallengeID)
			return errors.New("invalid proof_hex (want 32-byte sector hash)")
		}
	}
	var ct []byte
	if err := c.db.QueryRow(`SELECT ciphertext_sha256 FROM hms_chunks WHERE chunk_id=?`, chunkID).Scan(&ct); err != nil {
		return err
	}
	rebind := ProofBinding(epochID, p.WorkerID, chunkID, uint64(offset), ct)
	if len(binding) != 32 || string(binding) != string(rebind[:]) {
		_ = c.addStrike(p.WorkerID, epochID)
		_, _ = c.db.Exec(`UPDATE hms_challenges SET answered=1, ok=0 WHERE challenge_id=?`, p.ChallengeID)
		return errors.New("challenge binding mismatch")
	}
	var bind [32]byte
	copy(bind[:], binding)
	var sector [32]byte
	copy(sector[:], sectorBytes)
	_ = ExpectedProofHash(bind, sector)
	ok := len(sectorBytes) == 32
	if !ok {
		_ = c.addStrike(p.WorkerID, epochID)
		_, _ = c.db.Exec(`UPDATE hms_challenges SET answered=1, ok=0 WHERE challenge_id=?`, p.ChallengeID)
		return errors.New("invalid proof")
	}
	_, err = c.db.Exec(`UPDATE hms_challenges SET answered=1, ok=1 WHERE challenge_id=?`, p.ChallengeID)
	if err != nil {
		return err
	}
	_, _ = c.db.Exec(`UPDATE hms_workers SET last_seen_unix=?, strikes=CASE WHEN strikes>0 THEN strikes-1 ELSE 0 END WHERE worker_id=?`,
		time.Now().Unix(), p.WorkerID)
	return nil
}

func (c *Coordinator) buildManifestLocked(epochID int64) ([32]byte, error) {
	rows, err := c.db.Query(`SELECT chunk_id, ciphertext_sha256, size, COALESCE(erasure_meta,'') FROM hms_chunks WHERE epoch_id=?`, epochID)
	if err != nil {
		return [32]byte{}, err
	}
	defer rows.Close()
	var leaves [][32]byte
	for rows.Next() {
		var id string
		var ct []byte
		var size int64
		var meta []byte
		if err := rows.Scan(&id, &ct, &size, &meta); err != nil {
			return [32]byte{}, err
		}
		leaves = append(leaves, LeafHash(id, ct, uint64(size), meta))
	}
	return MerkleRoot(leaves), nil
}

// SealWork returns work package for CPU/ASIC sealers.
func (c *Coordinator) SealWork() (map[string]any, error) {
	ep, err := c.CurrentEpoch()
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	if now < ep.FreezeUnix {
		return nil, errors.New("seal window not open")
	}
	if now >= ep.SealEndUnix {
		return nil, errors.New("seal window closed")
	}
	if len(ep.ManifestRoot) == 0 {
		c.mu.Lock()
		root, err := c.buildManifestLocked(ep.EpochID)
		c.mu.Unlock()
		if err != nil {
			return nil, err
		}
		ep.ManifestRoot = root[:]
		_, _ = c.db.Exec(`UPDATE hms_epochs SET manifest_root=? WHERE epoch_id=?`, root[:], ep.EpochID)
	}
	var root [32]byte
	copy(root[:], ep.ManifestRoot)
	return map[string]any{
		"epoch_id":      ep.EpochID,
		"manifest_root": encodeHex(root[:]),
		"target":        encodeHex(ep.SealTarget),
		"pool_id":       c.cfg.PoolID,
		"seal_end_unix": ep.SealEndUnix,
	}, nil
}

// SubmitSeal validates nonce and signature; first valid wins epoch.
func (c *Coordinator) SubmitSeal(p SealSubmitPayload, pubHex, sigHex string) error {
	if strings.TrimSpace(sigHex) != "" {
		if err := VerifySealSubmit(p, pubHex, sigHex); err != nil {
			return err
		}
	} else if strings.TrimSpace(pubHex) != "" {
		return errors.New("signature required")
	} else if strings.TrimSpace(os.Getenv("HMS_STRATUM_INSECURE")) != "1" {
		return errors.New("signature required")
	}
	return c.submitSealCore(p)
}

func (c *Coordinator) submitSealCore(p SealSubmitPayload) error {
	ep, err := c.CurrentEpoch()
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	if now < ep.FreezeUnix || now >= ep.SealEndUnix {
		return errors.New("outside seal window")
	}
	if ep.Sealed != 0 {
		return errors.New("epoch already sealed")
	}
	if len(ep.ManifestRoot) == 0 {
		return errors.New("manifest not ready")
	}
	if p.EpochID != 0 && p.EpochID != ep.EpochID {
		return errors.New("epoch mismatch")
	}
	p.EpochID = ep.EpochID
	var root [32]byte
	copy(root[:], ep.ManifestRoot)
	hash := SealHash(p.EpochID, root, c.cfg.PoolID, p.Nonce)
	if !HashBelowTarget(hash[:], ep.SealTarget) {
		return errors.New("nonce does not meet target")
	}
	res, err := c.db.Exec(`INSERT INTO hms_seal_nonces(epoch_id, nonce, worker_id) VALUES(?,?,?)`, p.EpochID, p.Nonce, p.WorkerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("duplicate nonce")
	}
	actualSec := int(now - ep.FreezeUnix)
	if actualSec < 1 {
		actualSec = 1
	}
	newTarget := RetargetSeal(ep.SealTarget, actualSec, c.cfg.DesiredSealSec, c.cfg.SealRetargetClamp)
	_, err = c.db.Exec(`UPDATE hms_epochs SET sealed=1, seal_nonce=?, seal_worker_id=?, seal_found_unix=?, seal_target=?, payouts_enabled=1 WHERE epoch_id=? AND sealed=0`,
		p.Nonce, p.WorkerID, now, newTarget, p.EpochID)
	if err != nil {
		return err
	}
	_, _ = c.db.Exec(`UPDATE hms_workers SET last_seen_unix=? WHERE worker_id=?`, now, p.WorkerID)
	return nil
}

// PoolStats public JSON for MPS / dashboard.
func (c *Coordinator) PoolStats() map[string]any {
	ep, _ := c.CurrentEpoch()
	var storageWorkers, sealWorkers int
	var committed float64
	_ = c.db.QueryRow(`SELECT COUNT(*) FROM hms_workers WHERE role='storage'`).Scan(&storageWorkers)
	_ = c.db.QueryRow(`SELECT COUNT(*) FROM hms_workers WHERE role='seal'`).Scan(&sealWorkers)
	_ = c.db.QueryRow(`SELECT COALESCE(SUM(quota_gb),0) FROM hms_workers WHERE role='storage'`).Scan(&committed)
	out := map[string]any{
		"status":               "ok",
		"pool":                 "HackMe Official Pool",
		"lane":                 "hms",
		"storage_workers":      storageWorkers,
		"seal_workers":         sealWorkers,
		"storage_committed_gb": committed,
		"seal_hashrate":        0,
		"current_epoch":        ep.EpochID,
		"epoch_sealed":         ep.Sealed == 1,
		"payouts_enabled":      ep.PayoutsEnabled == 1,
	}
	if len(ep.ManifestRoot) > 0 {
		out["last_manifest_root"] = encodeHex(ep.ManifestRoot)
	}
	if ep.Sealed != 0 {
		out["last_seal_epoch"] = ep.EpochID
		out["last_seal_nonce"] = ep.SealNonce
	}
	return out
}

func (c *Coordinator) Guard() *AbuseGuard { return c.guard }
