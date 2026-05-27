package chain

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"hackme/internal/block"
	"hackme/internal/nodecrypto"
)

var ErrGenesisExists = errors.New("genesis already initialized")
var ErrEconomicInvariant = errors.New("economic invariant violation")

// Service coordinates chain state and SQLite persistence.
type Service struct {
	mu           sync.Mutex
	db           *sql.DB
	artifactRoot string // wasm_artifact_path resolves under here (see DefaultArtifactRoot)
	signer       *nodecrypto.Signer
}

const (
	metaTotalMintedHMC   = "econ_total_minted_hmc"
	metaTotalBurnedHMC   = "econ_total_burned_hmc"
	metaTotalMintedUnits = "econ_total_minted_units"
	metaTotalBurnedUnits = "econ_total_burned_units"
	metaOrderEscrowUnits = "econ_order_escrow_units"
)

func New(db *sql.DB) *Service {
	if err := validateLockedPolicy(); err != nil {
		panic(err)
	}
	return &Service{
		db:           db,
		artifactRoot: DefaultArtifactRoot(),
	}
}

func NewWithSigner(db *sql.DB, signer *nodecrypto.Signer) *Service {
	s := New(db)
	s.signer = signer
	return s
}

// RebindWalletToSigner aligns primary wallet ownership with the active signer.
// It migrates wallet units from old wallet account to signer account atomically.
func (s *Service) RebindWalletToSigner(ctx context.Context) error {
	if s.signer == nil {
		return nil
	}
	signerAddr := strings.TrimSpace(s.signer.Address())
	if signerAddr == "" {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var walletAddr string
	var walletUnits uint64
	if err := tx.QueryRowContext(ctx, `SELECT address, balance_units FROM wallet WHERE id = 1`).Scan(&walletAddr, &walletUnits); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	walletAddr = strings.TrimSpace(walletAddr)
	if walletAddr == "" || walletAddr == signerAddr {
		return nil
	}

	var oldBal uint64
	if err := tx.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address = ?`, walletAddr).Scan(&oldBal); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("wallet address account missing for rebind: %s", walletAddr)
		}
		return err
	}
	sourceUnits := oldBal
	if sourceUnits > walletUnits {
		sourceUnits = walletUnits
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE accounts SET balance_units = balance_units - ?, updated_at = strftime('%s','now') WHERE address = ?`,
		sourceUnits, walletAddr); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, ?, 0, strftime('%s','now'))
		 ON CONFLICT(address) DO UPDATE SET balance_units=accounts.balance_units + excluded.balance_units, updated_at=excluded.updated_at`,
		signerAddr, sourceUnits); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE wallet SET address = ?, balance_units = ?, balance_hmc = ? WHERE id = 1`,
		signerAddr, sourceUnits, UnitsToHMC(sourceUnits)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

// WasmCheckFromManifestJSON resolves wasm_check_hex or wasm_artifact_path + artifact_hash.
func (s *Service) WasmCheckFromManifestJSON(manifestJSON []byte) ([]byte, error) {
	return ResolveWasmCheckFromManifest(manifestJSON, s.artifactRoot)
}

// SchemaVersion returns SQLite PRAGMA user_version (set by store migrations).
func (s *Service) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v)
	return v, err
}

// Economics returns emission/burn counters from meta.
func (s *Service) Economics(ctx context.Context) (EconomicsSnapshot, error) {
	mintedUnits, burnedUnits, err := s.econTotalsUnits(ctx, s.db)
	if err != nil {
		return EconomicsSnapshot{}, err
	}
	minted := UnitsToHMC(mintedUnits)
	burned := UnitsToHMC(burnedUnits)
	circ := minted - burned
	if circ < 0 {
		circ = 0
	}
	remain := MaxSupplyHMC - minted
	if remain < 0 {
		remain = 0
	}
	return EconomicsSnapshot{
		MaxSupplyHMC:  MaxSupplyHMC,
		TotalMinted:   minted,
		TotalBurned:   burned,
		Circulating:   circ,
		MintRemaining: remain,
		BurnRateOrder: OrderBurnRate,
		OrderFeeRate:  OrderPlatformFeeRate,
		NetFeeBurn:    NetworkFeeBurnShare,
		NetFeeDev:     NetworkFeeDevShare,
		DevFeeAddress: DevFeeAddress,
		PolicyHash:    lockedPolicyHash(),
	}, nil
}

// PolicyHash returns the consensus-locked economics policy fingerprint.
func (s *Service) PolicyHash() string {
	return lockedPolicyHash()
}

func (s *Service) HasGenesis(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM blocks WHERE block_index = 0`).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// InitGenesis creates block #0 with minerAddress in the block header (node signer display).
// When GenesisRewardHMC > 0, that amount is minted to DevFeeAddress (treasury); the primary wallet row
// stays minerAddress with zero balance until PoH/order credits (unless miner == DevFeeAddress).
func (s *Service) InitGenesis(ctx context.Context, minerAddress string) (*block.Block, float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM blocks WHERE block_index = 0`).Scan(&exists); err != nil {
		return nil, 0, err
	}
	if exists > 0 {
		return nil, 0, ErrGenesisExists
	}

	miner := strings.TrimSpace(minerAddress)
	if miner == "" {
		return nil, 0, fmt.Errorf("genesis: empty miner address")
	}
	treasury := strings.TrimSpace(DevFeeAddress)
	reward := block.GenesisRewardHMC
	rewardUnits := HMCToUnits(reward)
	if rewardUnits > 0 && treasury == "" {
		return nil, 0, fmt.Errorf("genesis: empty DevFeeAddress for treasury mint")
	}

	b := block.NewGenesisBlock(miner)
	if err := s.attachBlockSignature(b); err != nil {
		return nil, 0, err
	}
	if err := verifyBlockIntegrityAndSignature(b); err != nil {
		return nil, 0, err
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return nil, 0, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO blocks (block_index, hash, prev_hash, json) VALUES (?,?,?,?)`,
		b.Index, b.Hash, b.PrevHash, string(raw)); err != nil {
		return nil, 0, err
	}
	walletBal := 0.0
	walletUnits := uint64(0)
	minerAcctUnits := uint64(0)
	treasuryAcctUnits := uint64(0)
	if rewardUnits > 0 {
		if treasury == miner {
			walletBal = reward
			walletUnits = rewardUnits
			minerAcctUnits = rewardUnits
		} else {
			treasuryAcctUnits = rewardUnits
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO wallet (id, address, balance_hmc, balance_units) VALUES (1, ?, ?, ?)`,
		miner, walletBal, walletUnits); err != nil {
		return nil, 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, ?, 0, strftime('%s','now'))
		 ON CONFLICT(address) DO UPDATE SET balance_units=excluded.balance_units, updated_at=excluded.updated_at`,
		miner, minerAcctUnits); err != nil {
		return nil, 0, err
	}
	if treasuryAcctUnits > 0 {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, ?, 0, strftime('%s','now'))
			 ON CONFLICT(address) DO UPDATE SET balance_units=excluded.balance_units, updated_at=excluded.updated_at`,
			treasury, treasuryAcctUnits); err != nil {
			return nil, 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES ('tip_hash', ?)`, b.Hash); err != nil {
		return nil, 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES ('chain_id', ?)`, block.ChainID); err != nil {
		return nil, 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES ('poh_target_mod', ?)`, strconv.FormatUint(DefaultPoHTargetMod, 10)); err != nil {
		return nil, 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES (?, ?)`, metaTotalMintedHMC, strconv.FormatFloat(block.GenesisRewardHMC, 'f', -1, 64)); err != nil {
		return nil, 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES (?, ?)`, metaTotalBurnedHMC, "0"); err != nil {
		return nil, 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES (?, ?)`, metaTotalMintedUnits, strconv.FormatUint(HMCToUnits(block.GenesisRewardHMC), 10)); err != nil {
		return nil, 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES (?, ?)`, metaTotalBurnedUnits, "0"); err != nil {
		return nil, 0, err
	}
	if err := s.checkEconomicInvariants(ctx, tx); err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	return b, block.GenesisRewardHMC, nil
}

// Wallet returns primary wallet row.
func (s *Service) Wallet(ctx context.Context) (address string, balance float64, err error) {
	var units uint64
	err = s.db.QueryRowContext(ctx, `SELECT address, balance_units FROM wallet WHERE id = 1`).Scan(&address, &units)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	var accountUnits uint64
	if qerr := s.db.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address = ?`, strings.TrimSpace(address)).Scan(&accountUnits); qerr == nil {
		units = accountUnits
	} else if !errors.Is(qerr, sql.ErrNoRows) {
		return "", 0, qerr
	}
	return address, UnitsToHMC(units), err
}

// MirrorWalletRowFromAccounts copies accounts.balance_units into wallet id=1 so direct SQL / legacy readers match Transfer ledger.
func (s *Service) MirrorWalletRowFromAccounts(ctx context.Context) error {
	var addr string
	if err := s.db.QueryRowContext(ctx, `SELECT address FROM wallet WHERE id = 1`).Scan(&addr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	addr = strings.TrimSpace(addr)
	var au uint64
	if err := s.db.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address = ?`, addr).Scan(&au); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	hmc := UnitsToHMC(au)
	_, err := s.db.ExecContext(ctx, `UPDATE wallet SET balance_units = ?, balance_hmc = ? WHERE id = 1`, au, hmc)
	return err
}

// Tip returns max block index and tip hash (0, "" if chain empty).
func (s *Service) Tip(ctx context.Context) (height uint64, hash string, err error) {
	var maxIdx sql.NullInt64
	var tip sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT MAX(block_index), (SELECT hash FROM blocks ORDER BY block_index DESC LIMIT 1) FROM blocks`).Scan(&maxIdx, &tip)
	if err != nil {
		return 0, "", err
	}
	if maxIdx.Valid {
		height = uint64(maxIdx.Int64)
	}
	if tip.Valid {
		hash = tip.String
	}
	return height, hash, nil
}

// TipFast reads tip via meta tip_hash (point lookup). Prefer for hot /api/status under mining write load.
func (s *Service) TipFast(ctx context.Context) (height uint64, hash string, err error) {
	var tipHash string
	err = s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'tip_hash'`).Scan(&tipHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", nil
		}
		return 0, "", err
	}
	tipHash = strings.TrimSpace(tipHash)
	if tipHash == "" {
		return 0, "", nil
	}
	var idx sql.NullInt64
	err = s.db.QueryRowContext(ctx, `SELECT block_index FROM blocks WHERE hash = ?`, tipHash).Scan(&idx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, tipHash, nil
		}
		return 0, "", err
	}
	if idx.Valid {
		height = uint64(idx.Int64)
	}
	return height, tipHash, nil
}

// ListBlocks returns last `limit` blocks oldest-first.
func (s *Service) ListBlocks(ctx context.Context, limit int) ([]json.RawMessage, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT json FROM blocks ORDER BY block_index DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out [][]byte
	for rows.Next() {
		var js string
		if err := rows.Scan(&js); err != nil {
			return nil, err
		}
		out = append(out, []byte(js))
	}
	// reverse to oldest-first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	raw := make([]json.RawMessage, len(out))
	for i := range out {
		raw[i] = json.RawMessage(out[i])
	}
	return raw, rows.Err()
}

// ListBlocksFromHeight returns up to `limit` blocks with block_index >= minHeight, oldest-first.
// Used by P2P sync to anchor an empty follower with genesis (block 0) before tail replay.
func (s *Service) ListBlocksFromHeight(ctx context.Context, minHeight uint64, limit int) ([]json.RawMessage, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT json FROM blocks WHERE block_index >= ? ORDER BY block_index ASC LIMIT ?`,
		minHeight, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var js string
		if err := rows.Scan(&js); err != nil {
			return nil, err
		}
		out = append(out, []byte(js))
	}
	raw := make([]json.RawMessage, len(out))
	for i := range out {
		raw[i] = json.RawMessage(out[i])
	}
	return raw, rows.Err()
}

// ChainID reads stored chain id meta.
func (s *Service) ChainID(ctx context.Context) string {
	var v string
	_ = s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'chain_id'`).Scan(&v)
	if v == "" {
		return block.ChainID
	}
	return v
}

// GenesisBlock returns block 0 JSON for mining status.
func (s *Service) GenesisBlock(ctx context.Context) (*block.Block, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT json FROM blocks WHERE block_index = 0`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var b block.Block
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// AppendMiningReward adds HMC to the primary wallet (MVP PoH payout).
func (s *Service) AppendMiningReward(ctx context.Context, delta float64) error {
	du := HMCToUnits(delta)
	_, err := s.db.ExecContext(ctx, `UPDATE wallet SET balance_units = balance_units + ?, balance_hmc = (balance_units + ?) / ? WHERE id = 1`, du, du, float64(UnitsPerHMC))
	return err
}

// PoHTargetMod returns the active PoH modulus from meta (default if row missing).
func (s *Service) PoHTargetMod(ctx context.Context) (uint64, error) {
	// Emergency anti-stall path: if no PoH block is appended for a while, gradually
	// ease target_mod so the network can recover without waiting for a lucky hit.
	// This keeps the 30s target practical after abrupt hashrate drops.
	s.mu.Lock()
	defer s.mu.Unlock()
	mod, err := s.poHTargetModFromDB(ctx, s.db)
	if err != nil {
		return 0, err
	}
	const minStallSec = PoHRetargetTargetSec * 2
	now := time.Now().Unix()
	var tipRaw string
	if err := s.db.QueryRowContext(ctx, `SELECT json FROM blocks ORDER BY block_index DESC LIMIT 1`).Scan(&tipRaw); err != nil {
		return mod, nil
	}
	var tip block.Block
	if err := json.Unmarshal([]byte(tipRaw), &tip); err != nil {
		return mod, nil
	}
	stallSec := now - tip.Timestamp
	if stallSec < minStallSec {
		return mod, nil
	}
	var lastAdjRaw string
	lastAdj := int64(0)
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'poh_target_emergency_last_unix'`).Scan(&lastAdjRaw); err == nil {
		if v, e := strconv.ParseInt(strings.TrimSpace(lastAdjRaw), 10, 64); e == nil && v > 0 {
			lastAdj = v
		}
	}
	// Avoid adjusting on every hot poll path call.
	if now-lastAdj < 10 {
		return mod, nil
	}
	next := ClampPoHTargetMod(RetargetMicroStep(mod, stallSec, PoHRetargetTargetSec))
	if next >= mod {
		// Only ease during stall; do not harden here.
		_, _ = s.db.ExecContext(ctx,
			`INSERT INTO meta (key, value) VALUES ('poh_target_emergency_last_unix', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			strconv.FormatInt(now, 10))
		return mod, nil
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES ('poh_target_mod', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		strconv.FormatUint(next, 10)); err != nil {
		return mod, nil
	}
	_, _ = s.db.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES ('poh_target_emergency_last_unix', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		strconv.FormatInt(now, 10))
	return next, nil
}

// AppendPoHBlock inserts the next block after a valid PoH solution, updates tip meta,
// credits reward, and sets meta poh_target_mod for the next round via retargeting.
// orderTaskID, when non-empty, must match an open row in tasks; progress is bumped in the same DB transaction.
func (s *Service) AppendPoHBlock(ctx context.Context, minerAddress string, nonce, eval uint64, rewardHMC float64, targetMod uint64, orderTaskID string) (*block.Block, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if rewardHMC < 0 {
		return nil, errors.New("chain: negative reward is not allowed")
	}
	var tipHash string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'tip_hash'`).Scan(&tipHash)
	if err != nil {
		return nil, err
	}

	var prevJSON string
	if err := s.db.QueryRowContext(ctx, `SELECT json FROM blocks WHERE hash = ?`, tipHash).Scan(&prevJSON); err != nil {
		return nil, err
	}
	var prev block.Block
	if err := json.Unmarshal([]byte(prevJSON), &prev); err != nil {
		return nil, err
	}

	metaMod, err := s.poHTargetModFromDB(ctx, s.db)
	if err != nil {
		return nil, err
	}
	orderTaskID = strings.TrimSpace(orderTaskID)
	if orderTaskID == "" {
		if targetMod != metaMod {
			return nil, fmt.Errorf("chain: poh target mod mismatch (chain %d, submitted %d)", metaMod, targetMod)
		}
	} else {
		// Pool order solve: lease M from coordinator (validated in SubmitOrderPoHSolve).
		targetMod = ClampPoHTargetMod(targetMod)
	}

	var maxIdx sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(block_index) FROM blocks`).Scan(&maxIdx); err != nil {
		return nil, err
	}
	if !maxIdx.Valid {
		return nil, errors.New("chain: empty (genesis required)")
	}
	nextIdx := uint64(maxIdx.Int64) + 1
	if err := validatePoHSubmission(nextIdx, nonce, eval, targetMod); err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	var nextMod uint64
	w := uint64(PoHRetargetWindowBlocks)
	if nextIdx >= w && nextIdx%w == 0 {
		anchorIdx := nextIdx - w
		var anchorRaw string
		if err := s.db.QueryRowContext(ctx, `SELECT json FROM blocks WHERE block_index = ?`, anchorIdx).Scan(&anchorRaw); err != nil {
			return nil, err
		}
		var anchorB block.Block
		if err := json.Unmarshal([]byte(anchorRaw), &anchorB); err != nil {
			return nil, err
		}
		actualSec := now - anchorB.Timestamp
		if actualSec < 1 {
			actualSec = 1
		}
		idealSec := PoHRetargetWindowBlocks * PoHRetargetTargetSec
		nextMod = RetargetWindow(targetMod, actualSec, idealSec)
		nextMod = ClampPoHTargetMod(nextMod)
	} else {
		// Faster bounded adaptation between full retarget windows.
		lastDeltaSec := now - prev.Timestamp
		if lastDeltaSec < 1 {
			lastDeltaSec = 1
		}
		nextMod = RetargetMicroStep(targetMod, lastDeltaSec, PoHRetargetTargetSec)
		nextMod = ClampPoHTargetMod(nextMod)
	}

	b := block.NewPoHBlock(
		nextIdx,
		tipHash,
		minerAddress,
		nonce,
		eval,
		targetMod,
		orderTaskID,
		PoHFormulaLabelForIndex(nextIdx),
	)
	if err := s.attachBlockSignature(b); err != nil {
		return nil, err
	}
	if err := verifyBlockIntegrityAndSignature(b); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := s.expireOpenOrderTasksTx(ctx, tx, now); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO blocks (block_index, hash, prev_hash, json) VALUES (?,?,?,?)`,
		b.Index, b.Hash, b.PrevHash, string(raw)); err != nil {
		return nil, err
	}
	if orderTaskID != "" {
		var orderReward float64
		if err := tx.QueryRowContext(ctx, `SELECT reward FROM tasks WHERE id = ? AND status = ? AND (expires_at = 0 OR expires_at > ?)`, orderTaskID, TaskStatusOpen, now).Scan(&orderReward); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("chain: order task %q not open or missing (refuse PoH block)", orderTaskID)
			}
			return nil, err
		}
		if HMCToUnits(orderReward) != HMCToUnits(rewardHMC) {
			return nil, fmt.Errorf("chain: order reward mismatch for %q (task %.8f HMC, block %.8f HMC)", orderTaskID, orderReward, rewardHMC)
		}
	}
	if err := s.bumpOrderTaskProgress(ctx, tx, orderTaskID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE meta SET value = ? WHERE key = 'tip_hash'`, b.Hash); err != nil {
		return nil, err
	}
	if rewardHMC > 0 {
		mintedUnits, burnedUnits, err := s.econTotalsUnits(ctx, tx)
		if err != nil {
			return nil, err
		}
		creditUnits := HMCToUnits(rewardHMC)
		if orderTaskID != "" {
			// Paid order: escrow pays the solver recorded in the block header (pool worker or chain leader).
			minerAddress = strings.TrimSpace(minerAddress)
			if minerAddress == "" || !strings.HasPrefix(minerAddress, "HMC-") {
				return nil, errors.New("chain: valid miner_address required for order reward credit")
			}
			escrowUnits, err := s.metaUint(ctx, tx, metaOrderEscrowUnits, 0)
			if err != nil {
				return nil, err
			}
			if creditUnits > escrowUnits {
				return nil, fmt.Errorf("chain: order escrow depleted (%d < %d)", escrowUnits, creditUnits)
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, ?, 0, strftime('%s','now'))
				 ON CONFLICT(address) DO UPDATE SET balance_units=accounts.balance_units + excluded.balance_units, updated_at=excluded.updated_at`,
				minerAddress, creditUnits); err != nil {
				return nil, err
			}
			if err := s.upsertMetaUint(ctx, tx, metaOrderEscrowUnits, escrowUnits-creditUnits); err != nil {
				return nil, err
			}
			// Keep float/meta compatibility in sync with unit-backed economics.
			if err := s.upsertMetaFloat(ctx, tx, metaTotalMintedHMC, UnitsToHMC(mintedUnits)); err != nil {
				return nil, err
			}
			if err := s.upsertMetaFloat(ctx, tx, metaTotalBurnedHMC, UnitsToHMC(burnedUnits)); err != nil {
				return nil, err
			}
		} else {
			// Empty mining: credit primary wallet row (see TestAppendPoHCreditsPrimaryWalletNotMinerArg).
			var rewardCreditAddr string
			if err := tx.QueryRowContext(ctx, `SELECT address FROM wallet WHERE id = 1`).Scan(&rewardCreditAddr); err != nil {
				return nil, err
			}
			rewardCreditAddr = strings.TrimSpace(rewardCreditAddr)
			if rewardCreditAddr == "" {
				return nil, errors.New("chain: primary wallet address missing (cannot credit PoH reward)")
			}
			maxSupplyUnits := HMCToUnits(MaxSupplyHMC)
			var allowedUnits uint64
			if mintedUnits >= maxSupplyUnits {
				allowedUnits = 0
			} else {
				remainingUnits := maxSupplyUnits - mintedUnits
				if creditUnits > remainingUnits {
					allowedUnits = remainingUnits
				} else {
					allowedUnits = creditUnits
				}
			}
			if allowedUnits > 0 {
				if _, err := tx.ExecContext(ctx, `UPDATE wallet SET balance_units = balance_units + ?, balance_hmc = (balance_units + ?) / ? WHERE id = 1`, allowedUnits, allowedUnits, float64(UnitsPerHMC)); err != nil {
					return nil, err
				}
				if _, err := tx.ExecContext(ctx,
					`INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, ?, 0, strftime('%s','now'))
					 ON CONFLICT(address) DO UPDATE SET balance_units=accounts.balance_units + excluded.balance_units, updated_at=excluded.updated_at`,
					rewardCreditAddr, allowedUnits); err != nil {
					return nil, err
				}
				if err := s.upsertMetaUint(ctx, tx, metaTotalMintedUnits, mintedUnits+allowedUnits); err != nil {
					return nil, err
				}
				if err := s.upsertMetaFloat(ctx, tx, metaTotalMintedHMC, UnitsToHMC(mintedUnits+allowedUnits)); err != nil {
					return nil, err
				}
				if err := s.upsertMetaUint(ctx, tx, metaTotalBurnedUnits, burnedUnits); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := s.applyPendingTransfers(ctx, tx, nextIdx, b.Hash); err != nil {
		return nil, err
	}
	if err := s.applyPendingSupTransfers(ctx, tx, nextIdx, b.Hash); err != nil {
		return nil, err
	}
	nextModStr := strconv.FormatUint(nextMod, 10)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES ('poh_target_mod', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		nextModStr); err != nil {
		return nil, err
	}
	if err := s.checkEconomicInvariants(ctx, tx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return b, nil
}

type queryRowExecContext interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (s *Service) metaFloat(ctx context.Context, q queryRowContext, key string, fallback float64) (float64, error) {
	var val sql.NullString
	err := q.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&val)
	if errors.Is(err, sql.ErrNoRows) || !val.Valid || val.String == "" {
		return fallback, nil
	}
	if err != nil {
		return 0, err
	}
	x, err := strconv.ParseFloat(val.String, 64)
	if err != nil {
		return fallback, nil
	}
	return x, nil
}

func (s *Service) upsertMetaFloat(ctx context.Context, q queryRowExecContext, key string, v float64) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, strconv.FormatFloat(v, 'f', -1, 64),
	)
	return err
}

func (s *Service) metaUint(ctx context.Context, q queryRowContext, key string, fallback uint64) (uint64, error) {
	var val sql.NullString
	err := q.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&val)
	if errors.Is(err, sql.ErrNoRows) || !val.Valid || val.String == "" {
		return fallback, nil
	}
	if err != nil {
		return 0, err
	}
	x, err := strconv.ParseUint(val.String, 10, 64)
	if err != nil {
		return fallback, nil
	}
	return x, nil
}

func (s *Service) upsertMetaUint(ctx context.Context, q queryRowExecContext, key string, v uint64) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, strconv.FormatUint(v, 10),
	)
	return err
}

func (s *Service) econTotalsUnits(ctx context.Context, q queryRowContext) (mintedUnits uint64, burnedUnits uint64, err error) {
	mintedUnits, err = s.metaUint(ctx, q, metaTotalMintedUnits, HMCToUnits(block.GenesisRewardHMC))
	if err != nil {
		return 0, 0, err
	}
	burnedUnits, err = s.metaUint(ctx, q, metaTotalBurnedUnits, 0)
	if err != nil {
		return 0, 0, err
	}
	// Backward compatibility: legacy float-only meta keys.
	if mintedUnits == 0 {
		if legacy, e := s.metaFloat(ctx, q, metaTotalMintedHMC, block.GenesisRewardHMC); e == nil {
			mintedUnits = HMCToUnits(legacy)
		}
	}
	if burnedUnits == 0 {
		if legacy, e := s.metaFloat(ctx, q, metaTotalBurnedHMC, 0); e == nil {
			burnedUnits = HMCToUnits(legacy)
		}
	}
	return mintedUnits, burnedUnits, nil
}

func (s *Service) checkEconomicInvariants(ctx context.Context, q queryRowContext) error {
	if err := validateLockedPolicy(); err != nil {
		return fmt.Errorf("%w: %v", ErrEconomicInvariant, err)
	}
	mintedUnits, burnedUnits, err := s.econTotalsUnits(ctx, q)
	if err != nil {
		return err
	}
	minted := UnitsToHMC(mintedUnits)
	burned := UnitsToHMC(burnedUnits)
	if minted < -1e-12 {
		return fmt.Errorf("%w: minted < 0 (%f)", ErrEconomicInvariant, minted)
	}
	if burned < -1e-12 {
		return fmt.Errorf("%w: burned < 0 (%f)", ErrEconomicInvariant, burned)
	}
	if minted > MaxSupplyHMC+1e-9 {
		return fmt.Errorf("%w: minted > max supply (%f > %f)", ErrEconomicInvariant, minted, MaxSupplyHMC)
	}
	if burnedUnits > mintedUnits {
		return fmt.Errorf("%w: burned > minted (%f > %f)", ErrEconomicInvariant, burned, minted)
	}
	escrowUnits, err := s.metaUint(ctx, q, metaOrderEscrowUnits, 0)
	if err != nil {
		return err
	}
	neededUnits, err := s.openOrderLiabilityUnits(ctx, q)
	if err != nil {
		return err
	}
	if escrowUnits < neededUnits {
		return fmt.Errorf("%w: order escrow underflow (%d < %d)", ErrEconomicInvariant, escrowUnits, neededUnits)
	}
	return nil
}

func (s *Service) openOrderLiabilityUnits(ctx context.Context, q queryRowContext) (uint64, error) {
	rows, err := q.QueryContext(ctx, `SELECT reward, target_solves, progress_count FROM tasks WHERE status = ?`, TaskStatusOpen)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var total uint64
	for rows.Next() {
		var reward float64
		var target, progress int
		if err := rows.Scan(&reward, &target, &progress); err != nil {
			return 0, err
		}
		if target < 0 {
			target = 0
		}
		if progress < 0 {
			progress = 0
		}
		remaining := target - progress
		if remaining < 0 {
			remaining = 0
		}
		if remaining == 0 || reward <= 0 {
			continue
		}
		need := HMCToUnits(reward * float64(remaining))
		if ^uint64(0)-total < need {
			return 0, fmt.Errorf("chain: open order liability overflow")
		}
		total += need
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return total, nil
}

func (s *Service) poHTargetModFromDB(ctx context.Context, db queryRowContext) (uint64, error) {
	var val sql.NullString
	err := db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'poh_target_mod'`).Scan(&val)
	if errors.Is(err, sql.ErrNoRows) || !val.Valid || val.String == "" {
		return DefaultPoHTargetMod, nil
	}
	if err != nil {
		return 0, err
	}
	p, err := strconv.ParseUint(val.String, 10, 64)
	if err != nil {
		return DefaultPoHTargetMod, nil
	}
	return ClampPoHTargetMod(p), nil
}

type queryRowContext interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// PoHBlockCountSince counts PoH blocks with timestamp_unix >= sinceUnix (excludes genesis).
func (s *Service) PoHBlockCountSince(ctx context.Context, sinceUnix int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM blocks WHERE block_index > 0
		 AND CAST(json_extract(json, '$.timestamp_unix') AS INTEGER) >= ?
		 AND json_extract(json, '$.task.kind') = ?`,
		sinceUnix, block.PoHBlockKind,
	).Scan(&n)
	return n, err
}

// RecentPoHAvgBlockSec returns average seconds per recent PoH block transition.
// Returns -1 when there are not enough PoH blocks in the sample.
func (s *Service) RecentPoHAvgBlockSec(ctx context.Context, window int) (float64, error) {
	if window < 2 {
		window = 2
	}
	if window > 200 {
		window = 200
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT CAST(json_extract(json, '$.timestamp_unix') AS INTEGER) AS ts
		   FROM blocks
		  WHERE block_index > 0
		    AND json_extract(json, '$.task.kind') = ?
		  ORDER BY block_index DESC
		  LIMIT ?`,
		block.PoHBlockKind, window+1)
	if err != nil {
		return -1, err
	}
	defer rows.Close()

	ts := make([]int64, 0, window+1)
	for rows.Next() {
		var x int64
		if err := rows.Scan(&x); err != nil {
			return -1, err
		}
		ts = append(ts, x)
	}
	if err := rows.Err(); err != nil {
		return -1, err
	}
	if len(ts) < 2 {
		return -1, nil
	}
	newest := ts[0]
	oldest := ts[len(ts)-1]
	steps := float64(len(ts) - 1)
	delta := float64(newest - oldest)
	if delta <= 0 || steps <= 0 {
		return -1, nil
	}
	return delta / steps, nil
}

// BlockSummary is a compact row for reports / dashboards (newest-first lists).
type BlockSummary struct {
	Index         uint64 `json:"index"`
	TimestampUnix int64  `json:"timestamp_unix"`
	TaskKind      string `json:"task_kind"`
	TaskID        string `json:"task_id"`
	HashPrefix    string `json:"hash_prefix"`
	MinerAddress  string `json:"miner_address,omitempty"`
}

// ListRecentBlockSummaries returns up to `limit` blocks, newest first.
func (s *Service) ListRecentBlockSummaries(ctx context.Context, limit int) ([]BlockSummary, error) {
	if limit <= 0 {
		limit = 30
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT block_index, json FROM blocks ORDER BY block_index DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BlockSummary
	for rows.Next() {
		var idx uint64
		var raw string
		if err := rows.Scan(&idx, &raw); err != nil {
			return nil, err
		}
		var b block.Block
		if err := json.Unmarshal([]byte(raw), &b); err != nil {
			continue
		}
		hp := b.Hash
		if len(hp) > 16 {
			hp = hp[:16] + "…"
		}
		out = append(out, BlockSummary{
			Index:         idx,
			TimestampUnix: b.Timestamp,
			TaskKind:      b.Task.Kind,
			TaskID:        b.Task.ID,
			HashPrefix:    hp,
			MinerAddress:  strings.TrimSpace(b.EffectiveMinerAddress()),
		})
	}
	return out, rows.Err()
}

// GetBlockSummaryByIndex returns one block summary row (explorer / deep links).
func (s *Service) GetBlockSummaryByIndex(ctx context.Context, index uint64) (BlockSummary, bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT json FROM blocks WHERE block_index = ?`, index).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return BlockSummary{}, false, nil
	}
	if err != nil {
		return BlockSummary{}, false, err
	}
	var b block.Block
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		return BlockSummary{}, false, err
	}
	hp := b.Hash
	if len(hp) > 16 {
		hp = hp[:16] + "…"
	}
	return BlockSummary{
		Index:         index,
		TimestampUnix: b.Timestamp,
		TaskKind:      b.Task.Kind,
		TaskID:        b.Task.ID,
		HashPrefix:    hp,
		MinerAddress:  strings.TrimSpace(b.EffectiveMinerAddress()),
	}, true, nil
}

func validatePoHSubmission(index, nonce, eval, targetMod uint64) error {
	if targetMod < pohRetargetMinMod || targetMod > pohRetargetMaxMod {
		return fmt.Errorf("chain: target mod out of bounds: %d", targetMod)
	}
	exp := PohEvalForIndex(index, nonce)
	if exp != eval {
		return errors.New("chain: eval does not match active PohEval(index, nonce)")
	}
	if eval%targetMod != 0 {
		return errors.New("chain: eval not divisible by target mod")
	}
	return nil
}

func (s *Service) attachBlockSignature(b *block.Block) error {
	if s == nil || s.signer == nil || b == nil {
		return nil
	}
	b.MinerSigAlg = TransferSigAlgEd25519
	b.MinerPubKey = s.signer.PublicKeyHex()
	b.MinerSig = s.signer.SignHex([]byte(b.Hash))
	return nil
}

func verifyBlockIntegrityAndSignature(b *block.Block) error {
	if b == nil {
		return errors.New("chain: nil block")
	}
	if b.HeaderHashHex() != b.Hash {
		return errors.New("chain: block hash mismatch")
	}
	if b.MinerPubKey == "" && b.MinerSig == "" {
		return nil
	}
	alg := strings.TrimSpace(strings.ToLower(b.MinerSigAlg))
	if alg == "" {
		alg = TransferSigAlgEd25519
	}
	if alg != TransferSigAlgEd25519 {
		return errors.New("chain: unsupported miner_sig_alg")
	}
	if b.MinerPubKey == "" || b.MinerSig == "" {
		return errors.New("chain: incomplete block signature fields")
	}
	pub, err := hex.DecodeString(b.MinerPubKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("chain: invalid miner_pubkey_ed25519")
	}
	sig, err := hex.DecodeString(b.MinerSig)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("chain: invalid miner_sig_ed25519")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(b.Hash), sig) {
		return errors.New("chain: block signature verify failed")
	}
	return nil
}
