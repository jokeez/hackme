package chain

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	metaHMSMaxSupplyUnits      = "hms_max_supply_units"
	metaHMSTotalMintedUnits    = "hms_total_minted_units"
	metaHMSTotalBurnedUnits    = "hms_total_burned_units"
	metaHMSGenesisUnix         = "hms_genesis_unix"
	metaHMSMintEnabled         = "hms_mint_enabled"
	DefaultHMSTransferMinFee   = uint64(1000)
	DefaultHMSTransferMaxBatch = 128
)

// HmsTransferTx moves HMS on the parallel ledger (HMC- address format).
type HmsTransferTx struct {
	TxType        string `json:"tx_type"`
	SigAlg        string `json:"sig_alg,omitempty"`
	From          string `json:"from"`
	To            string `json:"to"`
	AmountUnits   uint64 `json:"amount_units"`
	FeeUnits      uint64 `json:"fee_units"`
	Nonce         uint64 `json:"nonce"`
	TimestampUnix int64  `json:"timestamp_unix"`
	Memo          string `json:"memo,omitempty"`
	PubKeyEd25519 string `json:"pubkey_ed25519"`
	SigEd25519    string `json:"sig_ed25519"`
}

type HmsAddressState struct {
	Address         string  `json:"address"`
	BalanceHMSUnits uint64  `json:"balance_hms_units"`
	BalanceHMS      float64 `json:"balance_hms"`
	HMSNextNonce    uint64  `json:"hms_next_nonce"`
}

type HMSEconomics struct {
	MaxSupplyHMS       float64 `json:"max_supply_hms"`
	TotalMintedHMS     float64 `json:"total_minted_hms"`
	TotalBurnedHMS     float64 `json:"total_burned_hms"`
	RemainingHMS       float64 `json:"remaining_hms"`
	CirculatingHMS     float64 `json:"circulating_hms"`
	GenesisUnix        int64   `json:"genesis_unix,omitempty"`
	MintEnabled        bool    `json:"mint_enabled"`
	OnChainSettleLive  bool    `json:"on_chain_settle_live"`
	TreasuryAddress    string  `json:"treasury_address,omitempty"`
	FeeBurnShare       float64 `json:"fee_burn_share"`
	FeeTreasuryShare   float64 `json:"fee_treasury_share"`
	TreasuryGenesisPct float64 `json:"treasury_genesis_float_pct"`
}

func HMSToUnits(v float64) uint64 { return HMCToUnits(v) }
func UnitsToHMS(v uint64) float64 { return UnitsToHMC(v) }

func hmsMintEnabledFromEnv() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_HMS_MINT_ENABLED")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func (tx HmsTransferTx) canonicalBytes() ([]byte, error) {
	wire := struct {
		TxType        string `json:"tx_type"`
		SigAlg        string `json:"sig_alg,omitempty"`
		From          string `json:"from"`
		To            string `json:"to"`
		AmountUnits   uint64 `json:"amount_units"`
		FeeUnits      uint64 `json:"fee_units"`
		Nonce         uint64 `json:"nonce"`
		TimestampUnix int64  `json:"timestamp_unix"`
		Memo          string `json:"memo,omitempty"`
		PubKeyEd25519 string `json:"pubkey_ed25519"`
	}{
		TxType:        tx.TxType,
		SigAlg:        strings.TrimSpace(strings.ToLower(tx.SigAlg)),
		From:          strings.TrimSpace(tx.From),
		To:            strings.TrimSpace(tx.To),
		AmountUnits:   tx.AmountUnits,
		FeeUnits:      tx.FeeUnits,
		Nonce:         tx.Nonce,
		TimestampUnix: tx.TimestampUnix,
		Memo:          strings.TrimSpace(tx.Memo),
		PubKeyEd25519: strings.TrimSpace(tx.PubKeyEd25519),
	}
	return json.Marshal(wire)
}

func (tx HmsTransferTx) HashHex() (string, error) {
	b, err := tx.canonicalBytes()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func ValidateHmsTransferShape(tx HmsTransferTx) (code, msg string) {
	if tx.TxType != "transfer_hms_v1" {
		return "invalid_tx_type", "tx_type must be transfer_hms_v1"
	}
	if !strings.HasPrefix(strings.TrimSpace(tx.From), "HMC-") || !strings.HasPrefix(strings.TrimSpace(tx.To), "HMC-") {
		return "invalid_address", "from/to must be HMC- addresses"
	}
	if tx.AmountUnits == 0 {
		return "invalid_amount", "amount_units must be > 0"
	}
	if tx.FeeUnits < DefaultHMSTransferMinFee {
		return "fee_too_low", fmt.Sprintf("fee_units must be >= %d", DefaultHMSTransferMinFee)
	}
	return "", ""
}

func (s *Service) hmsTreasuryAddress(ctx context.Context, q queryRowContext) (string, error) {
	var v string
	err := q.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, metaHMSTreasuryAddress).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) || strings.TrimSpace(v) == "" {
		return "", fmt.Errorf("HMS treasury not configured — run hms genesis with treasury_address")
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(v), nil
}

func (s *Service) hmsMaxSupplyUnits(ctx context.Context, q queryRowExecContext) (uint64, error) {
	return s.metaUint(ctx, q, metaHMSMaxSupplyUnits, HMSToUnits(MaxSupplyHMS))
}

func (s *Service) hmsTotalMintedUnits(ctx context.Context, q queryRowExecContext) (uint64, error) {
	return s.metaUint(ctx, q, metaHMSTotalMintedUnits, 0)
}

func (s *Service) hmsTotalBurnedUnits(ctx context.Context, q queryRowExecContext) (uint64, error) {
	return s.metaUint(ctx, q, metaHMSTotalBurnedUnits, 0)
}

func (s *Service) HMSEconomics(ctx context.Context) (HMSEconomics, error) {
	maxU, err := s.hmsMaxSupplyUnits(ctx, s.db)
	if err != nil {
		return HMSEconomics{}, err
	}
	mintedU, err := s.hmsTotalMintedUnits(ctx, s.db)
	if err != nil {
		return HMSEconomics{}, err
	}
	burnedU, err := s.hmsTotalBurnedUnits(ctx, s.db)
	if err != nil {
		return HMSEconomics{}, err
	}
	var genesis int64
	var genesisStr string
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, metaHMSGenesisUnix).Scan(&genesisStr); err == nil {
		genesis, _ = strconv.ParseInt(strings.TrimSpace(genesisStr), 10, 64)
	}
	mintMeta := ""
	_ = s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, metaHMSMintEnabled).Scan(&mintMeta)
	mintOn := strings.TrimSpace(mintMeta) == "1" || hmsMintEnabledFromEnv()
	remain := uint64(0)
	if mintedU < maxU {
		remain = maxU - mintedU
	}
	treasury := ""
	if t, err := s.hmsTreasuryAddress(ctx, s.db); err == nil {
		treasury = t
	}
	circ := mintedU
	if burnedU < circ {
		circ -= burnedU
	} else {
		circ = 0
	}
	return HMSEconomics{
		MaxSupplyHMS:       UnitsToHMS(maxU),
		TotalMintedHMS:     UnitsToHMS(mintedU),
		TotalBurnedHMS:     UnitsToHMS(burnedU),
		RemainingHMS:       UnitsToHMS(remain),
		CirculatingHMS:     UnitsToHMS(circ),
		GenesisUnix:        genesis,
		MintEnabled:        mintOn,
		OnChainSettleLive:  mintOn && treasury != "",
		TreasuryAddress:    treasury,
		FeeBurnShare:       HMSNetworkFeeBurnShare,
		FeeTreasuryShare:   HMSNetworkFeeTreasuryShare,
		TreasuryGenesisPct: HMSTreasuryGenesisFloatPct,
	}, nil
}

// InitHMSGenesis pins supply cap, treasury address, enables mint, and credits genesis float to treasury.
func (s *Service) InitHMSGenesis(ctx context.Context, treasuryAddress string) error {
	treasuryAddress = strings.TrimSpace(treasuryAddress)
	if treasuryAddress == "" {
		treasuryAddress = strings.TrimSpace(os.Getenv("HMS_TREASURY_ADDRESS"))
	}
	if err := validateHMSTreasuryAddress(treasuryAddress); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	maxU := HMSToUnits(MaxSupplyHMS)
	genesisFloatU := uint64(float64(maxU) * HMSTreasuryGenesisFloatPct)
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.upsertMetaUint(ctx, tx, metaHMSMaxSupplyUnits, maxU); err != nil {
		return err
	}
	if err := s.upsertMeta(ctx, tx, metaHMSTreasuryAddress, treasuryAddress); err != nil {
		return err
	}
	var existingGenesis string
	_ = tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, metaHMSGenesisUnix).Scan(&existingGenesis)
	firstInit := strings.TrimSpace(existingGenesis) == ""
	if firstInit {
		if err := s.upsertMetaUint(ctx, tx, metaHMSTotalMintedUnits, genesisFloatU); err != nil {
			return err
		}
		if err := s.upsertMetaUint(ctx, tx, metaHMSTotalBurnedUnits, 0); err != nil {
			return err
		}
		if genesisFloatU > 0 {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO accounts (address, balance_units, balance_hms_units, next_nonce, hms_next_nonce, updated_at)
				 VALUES (?, 0, ?, 0, 0, strftime('%s','now'))
				 ON CONFLICT(address) DO UPDATE SET balance_hms_units=excluded.balance_hms_units, updated_at=excluded.updated_at`,
				treasuryAddress, genesisFloatU); err != nil {
				return err
			}
			mintRow := map[string]any{"op": "hms_genesis_float", "to": treasuryAddress, "amount_units": genesisFloatU, "ts": now}
			raw, _ := json.Marshal(mintRow)
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO hms_tx_history (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, block_index, block_hash, applied_at, reject_code)
				 VALUES (?, ?, '', ?, 0, 0, ?, 'included', 0, 'genesis', ?, '')`,
				fmt.Sprintf("hms-genesis-float-%d", now), string(raw), treasuryAddress, genesisFloatU, now); err != nil {
				return err
			}
		}
		if err := s.upsertMeta(ctx, tx, metaHMSGenesisUnix, strconv.FormatInt(now, 10)); err != nil {
			return err
		}
	}
	if err := s.upsertMeta(ctx, tx, metaHMSMintEnabled, "1"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) HmsAddressState(ctx context.Context, address string) (HmsAddressState, error) {
	addr := strings.TrimSpace(address)
	var bal, nonce uint64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(balance_hms_units,0), COALESCE(hms_next_nonce,0) FROM accounts WHERE address=?`, addr).
		Scan(&bal, &nonce)
	if errors.Is(err, sql.ErrNoRows) {
		return HmsAddressState{Address: addr}, nil
	}
	if err != nil {
		return HmsAddressState{}, err
	}
	return HmsAddressState{
		Address:         addr,
		BalanceHMSUnits: bal,
		BalanceHMS:      UnitsToHMS(bal),
		HMSNextNonce:    nonce,
	}, nil
}

const metaHMSMintIdemPrefix = "mint_idem:hms:"

// MintHMS credits HMS from emission cap (coordinator settlement / admin).
// Memo is required: retries with the same (to, amount, memo) are no-ops.
func (s *Service) MintHMS(ctx context.Context, to string, amountUnits uint64, memo string) (string, error) {
	if amountUnits == 0 {
		return "invalid_amount", errors.New("amount_units must be > 0")
	}
	to = strings.TrimSpace(to)
	memo = strings.TrimSpace(memo)
	if memo == "" {
		return "memo_required", errors.New("memo required for mint idempotency")
	}
	if !strings.HasPrefix(to, "HMC-") {
		return "invalid_address", errors.New("invalid address")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ec, _ := s.HMSEconomics(ctx)
	if !ec.MintEnabled && !hmsMintEnabledFromEnv() {
		return "hms_mint_disabled", errors.New("HMS mint not enabled — run hms_genesis_init.sh")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "internal_error", err
	}
	defer func() { _ = tx.Rollback() }()
	idemKey := mintIdempotencyMetaKey(metaHMSMintIdemPrefix, to, amountUnits, memo)
	txHash := fmt.Sprintf("mint-hms-%d-%s", time.Now().UnixNano(), to)
	if idemKey != "" {
		txHash = "hms-" + idemKey // UNIQUE(tx_hash) closes concurrent double-mint races
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT tx_hash FROM hms_tx_history WHERE tx_hash=?`, txHash).Scan(&existing)
		if err == nil {
			return "", nil // idempotent replay
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "internal_error", err
		}
		var prev string
		err = tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, idemKey).Scan(&prev)
		if err == nil && strings.TrimSpace(prev) != "" {
			return "", nil // idempotent replay
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "internal_error", err
		}
	}
	maxU, err := s.hmsMaxSupplyUnits(ctx, tx)
	if err != nil {
		return "internal_error", err
	}
	mintedU, err := s.hmsTotalMintedUnits(ctx, tx)
	if err != nil {
		return "internal_error", err
	}
	if mintedU+amountUnits > maxU {
		return "hms_supply_cap", errors.New("HMS max supply exceeded")
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO accounts (address, balance_units, balance_hms_units, next_nonce, hms_next_nonce, updated_at)
		 VALUES (?, 0, ?, 0, 0, strftime('%s','now'))
		 ON CONFLICT(address) DO UPDATE SET balance_hms_units=accounts.balance_hms_units+excluded.balance_hms_units, updated_at=excluded.updated_at`,
		to, amountUnits); err != nil {
		return "internal_error", err
	}
	if err := s.upsertMetaUint(ctx, tx, metaHMSTotalMintedUnits, mintedU+amountUnits); err != nil {
		return "internal_error", err
	}
	mintRow := map[string]any{"op": "mint_hms", "to": to, "amount_units": amountUnits, "memo": memo, "ts": time.Now().Unix()}
	raw, _ := json.Marshal(mintRow)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO hms_tx_history (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, block_index, block_hash, applied_at, reject_code)
		 VALUES (?, ?, '', ?, 0, 0, ?, 'included', 0, 'mint', ?, '')`,
		txHash, string(raw), to, amountUnits, time.Now().Unix()); err != nil {
		if idemKey != "" && strings.Contains(strings.ToLower(err.Error()), "unique") {
			return "", nil // concurrent idempotent winner
		}
		return "internal_error", err
	}
	if idemKey != "" {
		if err := s.upsertMeta(ctx, tx, idemKey, string(raw)); err != nil {
			return "internal_error", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "internal_error", err
	}
	return "", nil
}

// BurnHMSForService debits HMS and increments burned supply counter.
func (s *Service) BurnHMSForService(ctx context.Context, from string, amountUnits uint64, memo string) (string, error) {
	if amountUnits == 0 {
		return "invalid_amount", errors.New("amount_units must be > 0")
	}
	from = strings.TrimSpace(from)
	if !strings.HasPrefix(from, "HMC-") {
		return "invalid_address", errors.New("invalid address")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "internal_error", err
	}
	defer func() { _ = tx.Rollback() }()
	var bal uint64
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(balance_hms_units,0) FROM accounts WHERE address=?`, from).Scan(&bal)
	if errors.Is(err, sql.ErrNoRows) || bal < amountUnits {
		return "insufficient_hms_balance", errors.New("insufficient HMS balance")
	}
	if err != nil {
		return "internal_error", err
	}
	newBal := bal - amountUnits
	if _, err := tx.ExecContext(ctx,
		`UPDATE accounts SET balance_hms_units=?, updated_at=strftime('%s','now') WHERE address=?`,
		newBal, from); err != nil {
		return "internal_error", err
	}
	burnedU, err := s.hmsTotalBurnedUnits(ctx, tx)
	if err != nil {
		return "internal_error", err
	}
	if err := s.upsertMetaUint(ctx, tx, metaHMSTotalBurnedUnits, burnedU+amountUnits); err != nil {
		return "internal_error", err
	}
	burnRow := map[string]any{"op": "burn_hms", "from": from, "amount_units": amountUnits, "memo": strings.TrimSpace(memo), "ts": time.Now().Unix()}
	raw, _ := json.Marshal(burnRow)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO hms_tx_history (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, block_index, block_hash, applied_at, reject_code)
		 VALUES (?, ?, ?, '', 0, 0, ?, 'included', 0, 'burn', ?, '')`,
		fmt.Sprintf("burn-hms-%d-%s", time.Now().UnixNano(), from), string(raw), from, amountUnits, time.Now().Unix()); err != nil {
		return "internal_error", err
	}
	if err := tx.Commit(); err != nil {
		return "internal_error", err
	}
	return "", nil
}

func (s *Service) validateHmsTransferTx(ctx context.Context, tx HmsTransferTx, q queryRowExecContext) (string, string) {
	if code, msg := ValidateHmsTransferShape(tx); code != "" {
		return code, msg
	}
	alg := strings.TrimSpace(strings.ToLower(tx.SigAlg))
	if alg == "" {
		alg = TransferSigAlgEd25519
	}
	if alg != TransferSigAlgEd25519 {
		return "unsupported_sig_alg", "unsupported signature algorithm"
	}
	pub, err := hex.DecodeString(strings.TrimSpace(tx.PubKeyEd25519))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return "invalid_signature", "invalid pubkey"
	}
	sig, err := hex.DecodeString(strings.TrimSpace(tx.SigEd25519))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return "invalid_signature", "invalid signature"
	}
	b, err := tx.canonicalBytes()
	if err != nil {
		return "invalid_tx_encoding", err.Error()
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), b, sig) {
		return "invalid_signature", "signature verify failed"
	}
	derived, err := addressFromPubKeyHex(tx.PubKeyEd25519)
	if err != nil {
		return "invalid_signature", "invalid pubkey format"
	}
	if derived != tx.From {
		return "address_pubkey_mismatch", "from does not match pubkey"
	}
	if tx.TimestampUnix <= 0 {
		return "invalid_timestamp", "timestamp required"
	}
	now := time.Now().Unix()
	if tx.TimestampUnix < now-86400 {
		return "tx_too_old", "tx too old"
	}
	if tx.TimestampUnix > now+3600 {
		return "tx_too_far_in_future", "tx too far in future"
	}
	var bal, nonce uint64
	err = q.QueryRowContext(ctx, `SELECT COALESCE(balance_hms_units,0), COALESCE(hms_next_nonce,0) FROM accounts WHERE address=?`, tx.From).
		Scan(&bal, &nonce)
	if errors.Is(err, sql.ErrNoRows) {
		return "unknown_from", "from account not found"
	}
	if err != nil {
		return "internal_error", err.Error()
	}
	if tx.Nonce != nonce {
		return "invalid_nonce", "nonce mismatch"
	}
	total := tx.AmountUnits + tx.FeeUnits
	if total < tx.AmountUnits || bal < total {
		return "insufficient_hms_balance", "insufficient HMS balance"
	}
	return "", ""
}

func (s *Service) SubmitHmsTransferTx(ctx context.Context, tx HmsTransferTx) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	code, msg := s.validateHmsTransferTx(ctx, tx, s.db)
	if code != "" {
		return "", code, errors.New(msg)
	}
	txHash, err := tx.HashHex()
	if err != nil {
		return "", "invalid_tx_encoding", err
	}
	var existing string
	err = s.db.QueryRowContext(ctx,
		`SELECT tx_hash FROM hms_tx_pool WHERE from_address=? AND nonce=? AND status='pending' LIMIT 1`,
		tx.From, tx.Nonce).Scan(&existing)
	if err == nil {
		if strings.TrimSpace(existing) == txHash {
			return "", "duplicate_or_replay", errors.New("duplicate tx")
		}
		return "", "pending_nonce_conflict", errors.New("pending hms tx with same nonce")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "internal_error", err
	}
	raw, _ := json.Marshal(tx)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO hms_tx_pool (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, received_at, status, reject_code)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', '')`,
		txHash, string(raw), tx.From, tx.To, tx.Nonce, tx.FeeUnits, tx.AmountUnits, time.Now().Unix())
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return "", "duplicate_or_replay", errors.New("duplicate tx")
		}
		return "", "internal_error", err
	}
	return txHash, "pending", nil
}

func (s *Service) applyPendingHmsTransfers(ctx context.Context, txq queryRowExecContext, blockIndex uint64, blockHash string) error {
	treasury, err := s.hmsTreasuryAddress(ctx, txq)
	if err != nil {
		return nil
	}
	limit := DefaultHMSTransferMaxBatch
	rows, err := txq.QueryContext(ctx,
		`SELECT tx_hash, tx_json, received_at FROM hms_tx_pool WHERE status='pending' ORDER BY fee_units DESC, received_at ASC LIMIT ?`, limit)
	if err != nil {
		return err
	}
	defer rows.Close()
	type item struct {
		hash string
		tx   HmsTransferTx
	}
	var pool []item
	for rows.Next() {
		var h, raw string
		var recv int64
		if err := rows.Scan(&h, &raw, &recv); err != nil {
			return err
		}
		var tx HmsTransferTx
		if json.Unmarshal([]byte(raw), &tx) != nil {
			continue
		}
		pool = append(pool, item{hash: h, tx: tx})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range pool {
		code, _ := s.validateHmsTransferTx(ctx, item.tx, txq)
		if code != "" {
			_, _ = txq.ExecContext(ctx, `UPDATE hms_tx_pool SET status='rejected', reject_code=? WHERE tx_hash=?`, code, item.hash)
			_, _ = txq.ExecContext(ctx,
				`INSERT INTO hms_tx_history (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, block_index, block_hash, applied_at, reject_code)
				 VALUES (?, ?, ?, ?, ?, ?, ?, 'rejected', -1, '', ?, ?) ON CONFLICT(tx_hash) DO NOTHING`,
				item.hash, mustJSON(item.tx), item.tx.From, item.tx.To, item.tx.Nonce, item.tx.FeeUnits, item.tx.AmountUnits, time.Now().Unix(), code)
			_, _ = txq.ExecContext(ctx, `DELETE FROM hms_tx_pool WHERE tx_hash=?`, item.hash)
			continue
		}
		var fromBal, fromNonce, toBal uint64
		if err := txq.QueryRowContext(ctx, `SELECT COALESCE(balance_hms_units,0), COALESCE(hms_next_nonce,0) FROM accounts WHERE address=?`, item.tx.From).
			Scan(&fromBal, &fromNonce); err != nil {
			return err
		}
		if err := txq.QueryRowContext(ctx, `SELECT COALESCE(balance_hms_units,0) FROM accounts WHERE address=?`, item.tx.To).Scan(&toBal); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		total := item.tx.AmountUnits + item.tx.FeeUnits
		fromBal -= total
		fromNonce++
		toBal += item.tx.AmountUnits
		burnFee := uint64(float64(item.tx.FeeUnits) * HMSNetworkFeeBurnShare)
		treasuryFee := item.tx.FeeUnits - burnFee
		if _, err := txq.ExecContext(ctx,
			`UPDATE accounts SET balance_hms_units=?, hms_next_nonce=?, updated_at=strftime('%s','now') WHERE address=?`,
			fromBal, fromNonce, item.tx.From); err != nil {
			return err
		}
		if _, err := txq.ExecContext(ctx,
			`INSERT INTO accounts (address, balance_units, balance_hms_units, next_nonce, hms_next_nonce, updated_at)
			 VALUES (?, 0, ?, 0, 0, strftime('%s','now'))
			 ON CONFLICT(address) DO UPDATE SET balance_hms_units=excluded.balance_hms_units, updated_at=excluded.updated_at`,
			item.tx.To, toBal); err != nil {
			return err
		}
		if treasuryFee > 0 {
			if _, err := txq.ExecContext(ctx,
				`INSERT INTO accounts (address, balance_units, balance_hms_units, next_nonce, hms_next_nonce, updated_at)
				 VALUES (?, 0, ?, 0, 0, strftime('%s','now'))
				 ON CONFLICT(address) DO UPDATE SET balance_hms_units=accounts.balance_hms_units + excluded.balance_hms_units, updated_at=excluded.updated_at`,
				treasury, treasuryFee); err != nil {
				return err
			}
		}
		if burnFee > 0 {
			burnedU, _ := s.hmsTotalBurnedUnits(ctx, txq)
			_ = s.upsertMetaUint(ctx, txq, metaHMSTotalBurnedUnits, burnedU+burnFee)
		}
		if _, err := txq.ExecContext(ctx,
			`INSERT INTO hms_tx_history (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, block_index, block_hash, applied_at, reject_code)
			 VALUES (?, ?, ?, ?, ?, ?, ?, 'included', ?, ?, ?, '')`,
			item.hash, mustJSON(item.tx), item.tx.From, item.tx.To, item.tx.Nonce, item.tx.FeeUnits, item.tx.AmountUnits, blockIndex, blockHash, time.Now().Unix()); err != nil {
			return err
		}
		if _, err := txq.ExecContext(ctx, `DELETE FROM hms_tx_pool WHERE tx_hash=?`, item.hash); err != nil {
			return err
		}
	}
	_ = treasury
	return nil
}
