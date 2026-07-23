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
	MaxSupplySUP               = 21_000_000
	metaSUPMaxSupplyUnits      = "sup_max_supply_units"
	metaSUPTotalMintedUnits    = "sup_total_minted_units"
	metaSUPGenesisUnix         = "sup_genesis_unix"
	metaSUPMintEnabled         = "sup_mint_enabled"
	DefaultSUPTransferMinFee   = uint64(1000)
	DefaultSUPTransferMaxBatch = 128
)

// SupTransferTx moves SUP on the parallel ledger (same address format as HMC).
type SupTransferTx struct {
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

type SupAddressState struct {
	Address         string  `json:"address"`
	BalanceSUPUnits uint64  `json:"balance_sup_units"`
	BalanceSUP      float64 `json:"balance_sup"`
	SUPNextNonce    uint64  `json:"sup_next_nonce"`
}

type SUPEconomics struct {
	MaxSupplySUP      float64 `json:"max_supply_sup"`
	TotalMintedSUP    float64 `json:"total_minted_sup"`
	RemainingSUP      float64 `json:"remaining_sup"`
	GenesisUnix       int64   `json:"genesis_unix,omitempty"`
	MintEnabled       bool    `json:"mint_enabled"`
	OnChainSettleLive bool    `json:"on_chain_settle_live"`
}

func SUPToUnits(v float64) uint64 { return HMCToUnits(v) }
func UnitsToSUP(v uint64) float64 { return UnitsToHMC(v) }

func supMintEnabledFromEnv() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_SUP_MINT_ENABLED")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func (tx SupTransferTx) canonicalBytes() ([]byte, error) {
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

func (tx SupTransferTx) HashHex() (string, error) {
	b, err := tx.canonicalBytes()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func ValidateSupTransferShape(tx SupTransferTx) (code, msg string) {
	if tx.TxType != "transfer_sup_v1" {
		return "invalid_tx_type", "tx_type must be transfer_sup_v1"
	}
	if !strings.HasPrefix(strings.TrimSpace(tx.From), "HMC-") || !strings.HasPrefix(strings.TrimSpace(tx.To), "HMC-") {
		return "invalid_address", "from/to must be HMC- addresses"
	}
	if tx.AmountUnits == 0 {
		return "invalid_amount", "amount_units must be > 0"
	}
	if tx.FeeUnits < DefaultSUPTransferMinFee {
		return "fee_too_low", fmt.Sprintf("fee_units must be >= %d", DefaultSUPTransferMinFee)
	}
	return "", ""
}

func (s *Service) supMaxSupplyUnits(ctx context.Context, q queryRowExecContext) (uint64, error) {
	return s.metaUint(ctx, q, metaSUPMaxSupplyUnits, SUPToUnits(MaxSupplySUP))
}

func (s *Service) supTotalMintedUnits(ctx context.Context, q queryRowExecContext) (uint64, error) {
	return s.metaUint(ctx, q, metaSUPTotalMintedUnits, 0)
}

func (s *Service) SUPEconomics(ctx context.Context) (SUPEconomics, error) {
	maxU, err := s.supMaxSupplyUnits(ctx, s.db)
	if err != nil {
		return SUPEconomics{}, err
	}
	mintedU, err := s.supTotalMintedUnits(ctx, s.db)
	if err != nil {
		return SUPEconomics{}, err
	}
	var genesis int64
	var genesisStr string
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, metaSUPGenesisUnix).Scan(&genesisStr); err == nil {
		genesis, _ = strconv.ParseInt(strings.TrimSpace(genesisStr), 10, 64)
	}
	mintMeta := ""
	_ = s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, metaSUPMintEnabled).Scan(&mintMeta)
	mintOn := strings.TrimSpace(mintMeta) == "1" || supMintEnabledFromEnv()
	remain := uint64(0)
	if mintedU < maxU {
		remain = maxU - mintedU
	}
	return SUPEconomics{
		MaxSupplySUP:      UnitsToSUP(maxU),
		TotalMintedSUP:    UnitsToSUP(mintedU),
		RemainingSUP:      UnitsToSUP(remain),
		GenesisUnix:       genesis,
		MintEnabled:       mintOn,
		OnChainSettleLive: mintOn,
	}, nil
}

// InitSUPGenesis pins max supply and enables settlement mint.
// Idempotent: does not reset total_minted or genesis_unix once set.
// Self-heal: if sum(account SUP balances) > total_minted (legacy wipe),
// raises the minted floor so supply-cap accounting stays honest.
func (s *Service) InitSUPGenesis(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	maxU := SUPToUnits(MaxSupplySUP)
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.upsertMetaUint(ctx, tx, metaSUPMaxSupplyUnits, maxU); err != nil {
		return err
	}

	var genesisStr string
	genesisErr := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, metaSUPGenesisUnix).Scan(&genesisStr)
	if errors.Is(genesisErr, sql.ErrNoRows) || strings.TrimSpace(genesisStr) == "" {
		if err := s.upsertMeta(ctx, tx, metaSUPGenesisUnix, strconv.FormatInt(now, 10)); err != nil {
			return err
		}
	} else if genesisErr != nil {
		return genesisErr
	}

	mintedU, err := s.supTotalMintedUnits(ctx, tx)
	if err != nil {
		return err
	}
	var balSum uint64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(balance_sup_units), 0) FROM accounts`).Scan(&balSum); err != nil {
		return err
	}
	// First-ever genesis with empty ledger: keep minted at 0.
	// If balances already exist above minted (repeated genesis wipe), heal the floor.
	if balSum > mintedU {
		if err := s.upsertMetaUint(ctx, tx, metaSUPTotalMintedUnits, balSum); err != nil {
			return err
		}
	} else if mintedU == 0 && balSum == 0 {
		// Ensure key exists for fresh chains.
		var exists string
		err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, metaSUPTotalMintedUnits).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			if err := s.upsertMetaUint(ctx, tx, metaSUPTotalMintedUnits, 0); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}

	if err := s.upsertMeta(ctx, tx, metaSUPMintEnabled, "1"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) upsertMeta(ctx context.Context, q queryRowExecContext, key, val string) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, val)
	return err
}

func (s *Service) SupAddressState(ctx context.Context, address string) (SupAddressState, error) {
	addr := strings.TrimSpace(address)
	var bal, nonce uint64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(balance_sup_units,0), COALESCE(sup_next_nonce,0) FROM accounts WHERE address=?`, addr).
		Scan(&bal, &nonce)
	if errors.Is(err, sql.ErrNoRows) {
		return SupAddressState{Address: addr}, nil
	}
	if err != nil {
		return SupAddressState{}, err
	}
	return SupAddressState{
		Address:         addr,
		BalanceSUPUnits: bal,
		BalanceSUP:      UnitsToSUP(bal),
		SUPNextNonce:    nonce,
	}, nil
}

const metaSUPMintIdemPrefix = "mint_idem:sup:"

// MintSUP credits SUP from emission cap (admin settlement / genesis migration).
// Memo is required: retries with the same (to, amount, memo) are no-ops.
func (s *Service) MintSUP(ctx context.Context, to string, amountUnits uint64, memo string) (string, error) {
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
	ec, _ := s.SUPEconomics(ctx)
	if !ec.MintEnabled && !supMintEnabledFromEnv() {
		return "sup_mint_disabled", errors.New("SUP mint not enabled — run sup_genesis_init.sh")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "internal_error", err
	}
	defer func() { _ = tx.Rollback() }()
	idemKey := mintIdempotencyMetaKey(metaSUPMintIdemPrefix, to, amountUnits, memo)
	txHash := fmt.Sprintf("mint-%d-%s", time.Now().UnixNano(), to)
	if idemKey != "" {
		txHash = "sup-" + idemKey // UNIQUE(tx_hash) closes concurrent double-mint races
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT tx_hash FROM sup_tx_history WHERE tx_hash=?`, txHash).Scan(&existing)
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
	maxU, err := s.supMaxSupplyUnits(ctx, tx)
	if err != nil {
		return "internal_error", err
	}
	mintedU, err := s.supTotalMintedUnits(ctx, tx)
	if err != nil {
		return "internal_error", err
	}
	if mintedU+amountUnits > maxU {
		return "sup_supply_cap", errors.New("SUP max supply exceeded")
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO accounts (address, balance_units, balance_sup_units, next_nonce, sup_next_nonce, updated_at)
		 VALUES (?, 0, ?, 0, 0, strftime('%s','now'))
		 ON CONFLICT(address) DO UPDATE SET balance_sup_units=accounts.balance_sup_units+excluded.balance_sup_units, updated_at=excluded.updated_at`,
		to, amountUnits); err != nil {
		return "internal_error", err
	}
	if err := s.upsertMetaUint(ctx, tx, metaSUPTotalMintedUnits, mintedU+amountUnits); err != nil {
		return "internal_error", err
	}
	mintRow := map[string]any{"op": "mint_sup", "to": to, "amount_units": amountUnits, "memo": memo, "ts": time.Now().Unix()}
	raw, _ := json.Marshal(mintRow)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sup_tx_history (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, block_index, block_hash, applied_at, reject_code)
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

// BurnSUPForService debits SUP for in-ecosystem utility (audit discount, etc.).
// Unsigned burns are limited to the primary node wallet — client payer_wallet cannot burn others' SUP (C11).
func (s *Service) BurnSUPForService(ctx context.Context, from string, amountUnits uint64, memo string) (string, error) {
	if amountUnits == 0 {
		return "invalid_amount", errors.New("amount_units must be > 0")
	}
	from = strings.TrimSpace(from)
	s.mu.Lock()
	defer s.mu.Unlock()
	var walletAddr string
	if err := s.db.QueryRowContext(ctx, `SELECT address FROM wallet WHERE id=1`).Scan(&walletAddr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "wallet_missing", errors.New("chain: primary wallet missing")
		}
		return "internal_error", err
	}
	walletAddr = strings.TrimSpace(walletAddr)
	if from == "" {
		from = walletAddr
	}
	if !strings.HasPrefix(from, "HMC-") {
		return "invalid_address", errors.New("invalid address")
	}
	if from != walletAddr {
		return "owner_required", errors.New("chain: unsigned SUP burn only allowed for primary wallet; use BurnSUPWithOwnerProof")
	}
	return s.burnSUPTx(ctx, from, amountUnits, memo)
}

// BurnSUPCanonicalMessage is the ed25519 payload for BurnSUPWithOwnerProof.
func BurnSUPCanonicalMessage(from string, amountUnits uint64, memo string) []byte {
	return []byte(fmt.Sprintf("burn_sup_v1|%s|%d|%s", strings.TrimSpace(from), amountUnits, strings.TrimSpace(memo)))
}

// BurnSUPWithOwnerProof burns SUP after verifying ed25519 ownership of `from`.
func (s *Service) BurnSUPWithOwnerProof(ctx context.Context, from string, amountUnits uint64, memo, pubKeyHex, sigHex string) (string, error) {
	if amountUnits == 0 {
		return "invalid_amount", errors.New("amount_units must be > 0")
	}
	from = strings.TrimSpace(from)
	if !strings.HasPrefix(from, "HMC-") {
		return "invalid_address", errors.New("invalid address")
	}
	pub, err := hex.DecodeString(strings.TrimSpace(pubKeyHex))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return "invalid_signature", errors.New("invalid pubkey")
	}
	sig, err := hex.DecodeString(strings.TrimSpace(sigHex))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return "invalid_signature", errors.New("invalid signature")
	}
	derived, err := addressFromPubKeyHex(pubKeyHex)
	if err != nil || derived != from {
		return "address_pubkey_mismatch", errors.New("from does not match pubkey")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), BurnSUPCanonicalMessage(from, amountUnits, memo), sig) {
		return "invalid_signature", errors.New("signature verify failed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.burnSUPTx(ctx, from, amountUnits, memo)
}

func (s *Service) burnSUPTx(ctx context.Context, from string, amountUnits uint64, memo string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "internal_error", err
	}
	defer func() { _ = tx.Rollback() }()
	var bal uint64
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(balance_sup_units,0) FROM accounts WHERE address=?`, from).Scan(&bal)
	if errors.Is(err, sql.ErrNoRows) || bal < amountUnits {
		return "insufficient_sup_balance", errors.New("insufficient SUP balance")
	}
	if err != nil {
		return "internal_error", err
	}
	newBal := bal - amountUnits
	if _, err := tx.ExecContext(ctx,
		`UPDATE accounts SET balance_sup_units=?, updated_at=strftime('%s','now') WHERE address=?`,
		newBal, from); err != nil {
		return "internal_error", err
	}
	burnRow := map[string]any{"op": "burn_sup", "from": from, "amount_units": amountUnits, "memo": strings.TrimSpace(memo), "ts": time.Now().Unix()}
	raw, _ := json.Marshal(burnRow)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sup_tx_history (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, block_index, block_hash, applied_at, reject_code)
		 VALUES (?, ?, ?, '', 0, 0, ?, 'included', 0, 'burn', ?, '')`,
		fmt.Sprintf("burn-%d-%s", time.Now().UnixNano(), from), string(raw), from, amountUnits, time.Now().Unix()); err != nil {
		return "internal_error", err
	}
	if err := tx.Commit(); err != nil {
		return "internal_error", err
	}
	return "", nil
}

const AuditSUPDiscountMaxFraction = 0.15

// ApplyAuditSUPDiscount reduces HMC escrow budget by up to 15% when payer holds enough on-chain SUP.
func ApplyAuditSUPDiscount(budgetHMC, payerSUPBalance float64) (cashHMC, supUsed float64) {
	if budgetHMC <= 0 || payerSUPBalance <= 0 {
		return budgetHMC, 0
	}
	cap := budgetHMC * AuditSUPDiscountMaxFraction
	supUsed = payerSUPBalance
	if supUsed > cap {
		supUsed = cap
	}
	cashHMC = budgetHMC - supUsed
	if cashHMC < 0 {
		cashHMC = 0
	}
	return cashHMC, supUsed
}

func (s *Service) validateSupTransferTx(ctx context.Context, tx SupTransferTx, q queryRowExecContext) (string, string) {
	if code, msg := ValidateSupTransferShape(tx); code != "" {
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
	err = q.QueryRowContext(ctx, `SELECT COALESCE(balance_sup_units,0), COALESCE(sup_next_nonce,0) FROM accounts WHERE address=?`, tx.From).
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
		return "insufficient_sup_balance", "insufficient SUP balance"
	}
	return "", ""
}

func (s *Service) SubmitSupTransferTx(ctx context.Context, tx SupTransferTx) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	code, msg := s.validateSupTransferTx(ctx, tx, s.db)
	if code != "" {
		return "", code, errors.New(msg)
	}
	txHash, err := tx.HashHex()
	if err != nil {
		return "", "invalid_tx_encoding", err
	}
	var existing string
	err = s.db.QueryRowContext(ctx,
		`SELECT tx_hash FROM sup_tx_pool WHERE from_address=? AND nonce=? AND status='pending' LIMIT 1`,
		tx.From, tx.Nonce).Scan(&existing)
	if err == nil {
		if strings.TrimSpace(existing) == txHash {
			return "", "duplicate_or_replay", errors.New("duplicate tx")
		}
		return "", "pending_nonce_conflict", errors.New("pending sup tx with same nonce")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "internal_error", err
	}
	raw, _ := json.Marshal(tx)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sup_tx_pool (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, received_at, status, reject_code)
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

func (s *Service) applyPendingSupTransfers(ctx context.Context, txq queryRowExecContext, blockIndex uint64, blockHash string) error {
	limit := DefaultSUPTransferMaxBatch
	rows, err := txq.QueryContext(ctx,
		`SELECT tx_hash, tx_json, received_at FROM sup_tx_pool WHERE status='pending' ORDER BY fee_units DESC, received_at ASC LIMIT ?`, limit)
	if err != nil {
		return err
	}
	defer rows.Close()
	type item struct {
		hash string
		tx   SupTransferTx
	}
	var pool []item
	for rows.Next() {
		var h, raw string
		var recv int64
		if err := rows.Scan(&h, &raw, &recv); err != nil {
			return err
		}
		var tx SupTransferTx
		if json.Unmarshal([]byte(raw), &tx) != nil {
			continue
		}
		pool = append(pool, item{hash: h, tx: tx})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	devAddr := DevFeeAddress
	for _, item := range pool {
		code, _ := s.validateSupTransferTx(ctx, item.tx, txq)
		if code != "" {
			_, _ = txq.ExecContext(ctx, `UPDATE sup_tx_pool SET status='rejected', reject_code=? WHERE tx_hash=?`, code, item.hash)
			_, _ = txq.ExecContext(ctx,
				`INSERT INTO sup_tx_history (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, block_index, block_hash, applied_at, reject_code)
				 VALUES (?, ?, ?, ?, ?, ?, ?, 'rejected', -1, '', ?, ?) ON CONFLICT(tx_hash) DO NOTHING`,
				item.hash, mustJSON(item.tx), item.tx.From, item.tx.To, item.tx.Nonce, item.tx.FeeUnits, item.tx.AmountUnits, time.Now().Unix(), code)
			_, _ = txq.ExecContext(ctx, `DELETE FROM sup_tx_pool WHERE tx_hash=?`, item.hash)
			continue
		}
		var fromBal, fromNonce, toBal uint64
		if err := txq.QueryRowContext(ctx, `SELECT COALESCE(balance_sup_units,0), COALESCE(sup_next_nonce,0) FROM accounts WHERE address=?`, item.tx.From).
			Scan(&fromBal, &fromNonce); err != nil {
			return err
		}
		if err := txq.QueryRowContext(ctx, `SELECT COALESCE(balance_sup_units,0) FROM accounts WHERE address=?`, item.tx.To).Scan(&toBal); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		total := item.tx.AmountUnits + item.tx.FeeUnits
		fromBal -= total
		fromNonce++
		toBal += item.tx.AmountUnits
		burnFee := uint64(float64(item.tx.FeeUnits) * NetworkFeeBurnShare)
		devFee := item.tx.FeeUnits - burnFee
		if _, err := txq.ExecContext(ctx,
			`UPDATE accounts SET balance_sup_units=?, sup_next_nonce=?, updated_at=strftime('%s','now') WHERE address=?`,
			fromBal, fromNonce, item.tx.From); err != nil {
			return err
		}
		if _, err := txq.ExecContext(ctx,
			`INSERT INTO accounts (address, balance_units, balance_sup_units, next_nonce, sup_next_nonce, updated_at)
			 VALUES (?, 0, ?, 0, 0, strftime('%s','now'))
			 ON CONFLICT(address) DO UPDATE SET balance_sup_units=excluded.balance_sup_units, updated_at=excluded.updated_at`,
			item.tx.To, toBal); err != nil {
			return err
		}
		if devFee > 0 {
			if _, err := txq.ExecContext(ctx,
				`INSERT INTO accounts (address, balance_units, balance_sup_units, next_nonce, sup_next_nonce, updated_at)
				 VALUES (?, 0, ?, 0, 0, strftime('%s','now'))
				 ON CONFLICT(address) DO UPDATE SET balance_sup_units=accounts.balance_sup_units + excluded.balance_sup_units, updated_at=excluded.updated_at`,
				devAddr, devFee); err != nil {
				return err
			}
		}
		if _, err := txq.ExecContext(ctx,
			`INSERT INTO sup_tx_history (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, block_index, block_hash, applied_at, reject_code)
			 VALUES (?, ?, ?, ?, ?, ?, ?, 'included', ?, ?, ?, '')`,
			item.hash, mustJSON(item.tx), item.tx.From, item.tx.To, item.tx.Nonce, item.tx.FeeUnits, item.tx.AmountUnits, blockIndex, blockHash, time.Now().Unix()); err != nil {
			return err
		}
		if _, err := txq.ExecContext(ctx, `DELETE FROM sup_tx_pool WHERE tx_hash=?`, item.hash); err != nil {
			return err
		}
	}
	return nil
}
