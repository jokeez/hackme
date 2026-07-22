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
	cfg     Config
	db      *sql.DB
	guard   *AbuseGuard
	stratum *StratumRegistry
	mu      sync.RWMutex
}

func NewCoordinator(db *sql.DB, cfg Config) *Coordinator {
	return &Coordinator{
		cfg:     cfg,
		db:      db,
		guard:   NewAbuseGuard(120, time.Minute),
		stratum: NewStratumRegistry(),
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
	return c.RegisterStorageWorkerEndpoint(workerID, pubHex, quotaGB, "")
}

// RegisterStorageWorkerEndpoint registers a storage worker and optional remote push URL.
// Pubkey is immutable after first registration (H48 / HMC-001).
func (c *Coordinator) RegisterStorageWorkerEndpoint(workerID, pubHex string, quotaGB int, endpointURL string) error {
	workerID = trimWorkerID(workerID)
	pubHex = strings.TrimSpace(strings.ToLower(pubHex))
	if err := ValidateWorkerID(workerID); err != nil {
		return err
	}
	if err := ValidateQuota(c.cfg, quotaGB); err != nil {
		return err
	}
	if err := validateWorkerPubkeyHex(pubHex); err != nil {
		return err
	}
	ep, err := ValidateWorkerEndpointURL(endpointURL)
	if err != nil {
		return err
	}
	var existingPub string
	var existingLast int64
	err = c.db.QueryRow(`SELECT pubkey_hex, last_seen_unix FROM hms_workers WHERE worker_id=?`, workerID).
		Scan(&existingPub, &existingLast)
	if err == nil {
		if !strings.EqualFold(strings.TrimSpace(existingPub), pubHex) {
			return errors.New("worker pubkey immutable — already registered with different key")
		}
		if existingLast < c.workerOnlineCutoff() {
			return errors.New("worker offline — heartbeat required before quota update")
		}
		now := time.Now().Unix()
		_, err = c.db.Exec(`UPDATE hms_workers SET role='storage', quota_gb=?, last_seen_unix=?, endpoint_url=? WHERE worker_id=?`,
			quotaGB, now, ep, workerID)
		return err
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	now := time.Now().Unix()
	_, err = c.db.Exec(`INSERT INTO hms_workers(worker_id, role, pubkey_hex, quota_gb, last_seen_unix, created_unix, endpoint_url)
		VALUES(?, 'storage', ?, ?, ?, ?, ?)`,
		workerID, pubHex, quotaGB, now, now, ep)
	return err
}

func (c *Coordinator) RegisterSealWorker(workerID, pubHex string) error {
	workerID = trimWorkerID(workerID)
	pubHex = strings.TrimSpace(strings.ToLower(pubHex))
	if err := ValidateWorkerID(workerID); err != nil {
		return err
	}
	if err := validateWorkerPubkeyHex(pubHex); err != nil {
		return err
	}
	var existingPub string
	err := c.db.QueryRow(`SELECT pubkey_hex FROM hms_workers WHERE worker_id=?`, workerID).Scan(&existingPub)
	if err == nil {
		if !strings.EqualFold(strings.TrimSpace(existingPub), pubHex) {
			return errors.New("worker pubkey immutable — already registered with different key")
		}
		now := time.Now().Unix()
		_, err = c.db.Exec(`UPDATE hms_workers SET role='seal', last_seen_unix=? WHERE worker_id=?`, now, workerID)
		return err
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	now := time.Now().Unix()
	_, err = c.db.Exec(`INSERT INTO hms_workers(worker_id, role, pubkey_hex, quota_gb, last_seen_unix, created_unix)
		VALUES(?, 'seal', ?, 0, ?, ?)`,
		workerID, pubHex, now, now)
	return err
}

func validateWorkerPubkeyHex(pubHex string) error {
	raw, err := hex.DecodeString(strings.TrimSpace(pubHex))
	if err != nil || len(raw) != 32 {
		return errors.New("invalid worker pubkey")
	}
	return nil
}

func (c *Coordinator) registeredPubkey(workerID string) (string, error) {
	var pub string
	err := c.db.QueryRow(`SELECT pubkey_hex FROM hms_workers WHERE worker_id=?`, trimWorkerID(workerID)).Scan(&pub)
	if err == sql.ErrNoRows {
		return "", errors.New("worker not registered")
	}
	if err != nil {
		return "", err
	}
	pub = strings.TrimSpace(pub)
	if pub == "" {
		return "", errors.New("worker pubkey missing")
	}
	return pub, nil
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
	var sectorExpected []byte
	if ctBytes, readErr := c.readMarketChunkFile(workerID, chunkID); readErr == nil && len(ctBytes) > 0 {
		sector := SectorProofFromCiphertext(ctBytes, offset)
		sectorExpected = sector[:]
	}
	chID := fmt.Sprintf("%d-%s-%x", ep.EpochID, workerID, ob)
	expires := now + int64(c.cfg.ChallengeTTL.Seconds())
	_, err = c.db.Exec(`INSERT INTO hms_challenges(challenge_id, epoch_id, worker_id, chunk_id, sector_offset, expected_hash, sector_proof_expected, expires_unix)
		VALUES(?,?,?,?,?,?,?,?)`, chID, ep.EpochID, workerID, chunkID, offset, binding[:], sectorExpected, expires)
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

// SubmitStorageProof verifies signed proof against the registered immutable pubkey (H48).
func (c *Coordinator) SubmitStorageProof(p StorageSubmitPayload, pubHex, sigHex string, proof []byte) error {
	reg, err := c.registeredPubkey(p.WorkerID)
	if err != nil {
		return err
	}
	pubHex = strings.TrimSpace(pubHex)
	if pubHex != "" && !strings.EqualFold(pubHex, reg) {
		return errors.New("pubkey does not match registered worker")
	}
	if err := VerifyStorageSubmit(p, reg, sigHex); err != nil {
		return err
	}
	var binding []byte
	var epochID int64
	var chunkID string
	var offset int64
	var answered int
	var sectorExpected []byte
	err = c.db.QueryRow(`SELECT expected_hash, epoch_id, chunk_id, sector_offset, answered, COALESCE(sector_proof_expected,'') FROM hms_challenges WHERE challenge_id=?`, p.ChallengeID).
		Scan(&binding, &epochID, &chunkID, &offset, &answered, &sectorExpected)
	if err != nil {
		return err
	}
	if answered != 0 {
		return errors.New("challenge already answered")
	}
	var exp int64
	_ = c.db.QueryRow(`SELECT expires_unix FROM hms_challenges WHERE challenge_id=?`, p.ChallengeID).Scan(&exp)
	if time.Now().Unix() > exp {
		c.markStorageChallengeFailed(p.WorkerID, chunkID, epochID, p.ChallengeID)
		return errors.New("challenge expired")
	}
	sectorBytes, err := hex.DecodeString(strings.TrimSpace(p.ProofHex))
	if err != nil || len(sectorBytes) != 32 {
		if len(proof) == 32 {
			sectorBytes = proof
		} else {
			c.markStorageChallengeFailed(p.WorkerID, chunkID, epochID, p.ChallengeID)
			return errors.New("invalid proof_hex (want 32-byte sector hash)")
		}
	}
	var ct []byte
	if err := c.db.QueryRow(`SELECT ciphertext_sha256 FROM hms_chunks WHERE chunk_id=?`, chunkID).Scan(&ct); err != nil {
		return err
	}
	rebind := ProofBinding(epochID, p.WorkerID, chunkID, uint64(offset), ct)
	if len(binding) != 32 || string(binding) != string(rebind[:]) {
		c.markStorageChallengeFailed(p.WorkerID, chunkID, epochID, p.ChallengeID)
		return errors.New("challenge binding mismatch")
	}
	var bind [32]byte
	copy(bind[:], binding)
	var sector [32]byte
	copy(sector[:], sectorBytes)
	verified := false
	if len(sectorExpected) == 32 {
		var expected [32]byte
		copy(expected[:], sectorExpected)
		if sector == expected {
			verified = true
		}
	}
	if !verified {
		var chunkWorker string
		if err := c.db.QueryRow(`SELECT worker_id FROM hms_chunks WHERE chunk_id=?`, chunkID).Scan(&chunkWorker); err == nil {
			if ctBytes, readErr := c.readMarketChunkFile(chunkWorker, chunkID); readErr == nil && len(ctBytes) > 0 {
				expected := SectorProofFromCiphertext(ctBytes, uint64(offset))
				if sector == expected {
					verified = true
				}
			}
		}
	}
	if !verified {
		c.markStorageChallengeFailed(p.WorkerID, chunkID, epochID, p.ChallengeID)
		return errors.New("sector proof mismatch")
	}
	_ = ExpectedProofHash(bind, sector)
	_, err = c.db.Exec(`UPDATE hms_challenges SET answered=1, ok=1 WHERE challenge_id=?`, p.ChallengeID)
	if err != nil {
		return err
	}
	_, _ = c.db.Exec(`UPDATE hms_workers SET last_seen_unix=?, strikes=CASE WHEN strikes>0 THEN strikes-1 ELSE 0 END WHERE worker_id=?`,
		time.Now().Unix(), p.WorkerID)
	return nil
}

func (c *Coordinator) buildManifestLocked(epochID int64) ([32]byte, error) {
	// Customer market blobs (ord-*) participate in PoSt but not in the hourly seal manifest.
	rows, err := c.db.Query(`SELECT chunk_id, ciphertext_sha256, size, COALESCE(erasure_meta,'') FROM hms_chunks
		WHERE epoch_id=? AND chunk_id NOT LIKE 'ord-%'`, epochID)
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

// SubmitSeal validates nonce and signature against the registered immutable pubkey (H48).
func (c *Coordinator) SubmitSeal(p SealSubmitPayload, pubHex, sigHex string) error {
	p.WorkerID = trimWorkerID(p.WorkerID)
	sigHex = strings.TrimSpace(sigHex)
	pubHex = strings.TrimSpace(pubHex)
	if sigHex != "" {
		reg, err := c.registeredPubkey(p.WorkerID)
		if err != nil {
			return err
		}
		if pubHex != "" && !strings.EqualFold(pubHex, reg) {
			return errors.New("pubkey does not match registered worker")
		}
		if err := VerifySealSubmit(p, reg, sigHex); err != nil {
			return err
		}
	} else if pubHex != "" {
		return errors.New("signature required")
	} else if !stratumInsecureEnabled() {
		return errors.New("signature required")
	}
	return c.submitSealCore(p)
}

// SubmitSealFromStratum accepts an unsigned seal after Stratum bind/HMAC gates (H47).
func (c *Coordinator) SubmitSealFromStratum(p SealSubmitPayload, hmacAuthorized bool) error {
	if hmacAuthorized {
		return c.submitSealCore(p)
	}
	if stratumInsecureEnabled() {
		return c.submitSealCore(p)
	}
	return errors.New("signature required")
}

func stratumInsecureEnabled() bool {
	return strings.TrimSpace(os.Getenv("HMS_STRATUM_INSECURE")) == "1"
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
	sealRes, err := c.db.Exec(`UPDATE hms_epochs SET sealed=1, seal_nonce=?, seal_worker_id=?, seal_found_unix=?, seal_target=?, payouts_enabled=1 WHERE epoch_id=? AND sealed=0`,
		p.Nonce, p.WorkerID, now, newTarget, p.EpochID)
	if err != nil {
		return err
	}
	if n, _ := sealRes.RowsAffected(); n == 0 {
		return errors.New("epoch already sealed")
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
	capSnap, _ := c.NetworkCapacity()
	_ = c.db.QueryRow(`SELECT COALESCE(SUM(quota_gb),0) FROM hms_workers WHERE role='storage'`).Scan(&committed)
	out := map[string]any{
		"status":               "ok",
		"pool":                 "HackMe Official Pool",
		"lane":                 "hms",
		"storage_workers":      storageWorkers,
		"seal_workers":         sealWorkers,
		"storage_committed_gb": committed,
		"network_capacity":     capSnap,
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

func (c *Coordinator) markStorageChallengeFailed(workerID, chunkID string, epochID int64, challengeID string) {
	_ = c.addStrike(workerID, epochID)
	_, _ = c.db.Exec(`UPDATE hms_challenges SET answered=1, ok=0 WHERE challenge_id=?`, challengeID)
	c.recordStorageProofFailure(workerID, chunkID)
}

// WorkerEpochStorageEligible is false when worker failed PoSt or is slashed for market replicas.
func (c *Coordinator) WorkerEpochStorageEligible(workerID string, epochID int64) (bool, error) {
	ok, err := c.WorkerEligibleForStoragePayout(workerID)
	if err != nil || !ok {
		return ok, err
	}
	var failed int
	err = c.db.QueryRow(`SELECT COUNT(*) FROM hms_challenges WHERE worker_id=? AND epoch_id=? AND answered=1 AND ok=0`,
		workerID, epochID).Scan(&failed)
	if err != nil {
		return false, err
	}
	return failed == 0, nil
}

func (c *Coordinator) Guard() *AbuseGuard { return c.guard }
