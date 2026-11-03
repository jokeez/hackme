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
	"strings"
	"time"
)

// SUP fee spend (Economics v2, RFC D5): SUP is a non-refundable fee token for any product in
// the ecosystem. The chain only verifies the payer, debits and burns; what a product sells for
// SUP is the product's business. New products (HMS storage, later coins) register here and need
// no new consensus rule.
//
// One burn per (product, ref): the burn's history row is keyed by that pair, so a replayed or
// retried signed burn can never charge the payer twice.

// SUPFeeProductHMCOrder is the fee leg of an HMC order (PoH order, Dig/Hunt campaign).
const SUPFeeProductHMCOrder = "hmc_order"

// SUPFeeRateToHMCFee is SUP units charged per HMC unit of fee (RFC: 1:1).
const SUPFeeRateToHMCFee = 1

var supFeeProducts = map[string]bool{
	SUPFeeProductHMCOrder: true,
}

const metaSUPTotalBurnedUnits = "sup_total_burned_units"

var (
	ErrSUPFeeProduct     = errors.New("chain: unknown SUP fee product")
	ErrSUPFeeRefPaid     = errors.New("chain: SUP fee already paid for this product/ref")
	ErrSUPFeeInsufficent = errors.New("chain: insufficient SUP balance for fee")
)

// SUPFeeBurn is a holder-signed request to pay a product fee in SUP.
type SUPFeeBurn struct {
	From          string `json:"from"`
	AmountUnits   uint64 `json:"amount_units"`
	Product       string `json:"product"`
	Ref           string `json:"ref"`
	PubKeyEd25519 string `json:"pubkey_ed25519"`
	SigEd25519    string `json:"sig_ed25519"`
}

// BurnSUPForFeeMessage is the ed25519 payload: chain_id stops a signature from one network
// (lab, testnet, a future chain_id) being spent on another.
func BurnSUPForFeeMessage(chainID, from string, amountUnits uint64, product, ref string) []byte {
	return []byte(fmt.Sprintf("burn_sup_for_fee_v1|%s|%s|%d|%s|%s",
		strings.TrimSpace(chainID), strings.TrimSpace(from), amountUnits,
		strings.TrimSpace(product), strings.TrimSpace(ref)))
}

func supFeeRefKey(product, ref string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(product) + "|" + strings.TrimSpace(ref)))
	return "supfee-" + hex.EncodeToString(sum[:16])
}

func validateSUPFeeShape(from string, amountUnits uint64, product, ref string) (string, error) {
	if amountUnits == 0 {
		return "invalid_amount", errors.New("amount_units must be > 0")
	}
	if !strings.HasPrefix(strings.TrimSpace(from), "HMC-") {
		return "invalid_address", errors.New("invalid address")
	}
	if !supFeeProducts[strings.TrimSpace(product)] {
		return "unknown_product", ErrSUPFeeProduct
	}
	ref = strings.TrimSpace(ref)
	if ref == "" || len(ref) > 128 || strings.ContainsAny(ref, "|\n\r") {
		return "invalid_ref", errors.New("ref required (≤128 chars, no '|' or newlines)")
	}
	return "", nil
}

// BurnSUPForFee burns a holder-signed SUP fee for (product, ref).
func (s *Service) BurnSUPForFee(ctx context.Context, b SUPFeeBurn) (string, error) {
	from := strings.TrimSpace(b.From)
	if code, err := validateSUPFeeShape(from, b.AmountUnits, b.Product, b.Ref); err != nil {
		return code, err
	}
	pub, err := hex.DecodeString(strings.TrimSpace(b.PubKeyEd25519))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return "invalid_signature", errors.New("invalid pubkey")
	}
	sig, err := hex.DecodeString(strings.TrimSpace(b.SigEd25519))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return "invalid_signature", errors.New("invalid signature")
	}
	if derived, err := addressFromPubKeyHex(b.PubKeyEd25519); err != nil || derived != from {
		return "address_pubkey_mismatch", errors.New("from does not match pubkey")
	}
	msg := BurnSUPForFeeMessage(s.ChainID(ctx), from, b.AmountUnits, b.Product, b.Ref)
	if !ed25519.Verify(ed25519.PublicKey(pub), msg, sig) {
		return "invalid_signature", errors.New("signature verify failed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "internal_error", err
	}
	defer func() { _ = tx.Rollback() }()
	if code, err := s.burnSUPForFeeTx(ctx, tx, from, b.AmountUnits, b.Product, b.Ref); err != nil {
		return code, err
	}
	if err := tx.Commit(); err != nil {
		return "internal_error", err
	}
	return "", nil
}

// burnSUPForFeeTx debits and burns SUP inside the caller's tx (order open stays atomic).
// Caller holds s.mu and has authorized `from` (signature, or the primary wallet itself).
func (s *Service) burnSUPForFeeTx(ctx context.Context, tx *sql.Tx, from string, amountUnits uint64, product, ref string) (string, error) {
	from = strings.TrimSpace(from)
	if code, err := validateSUPFeeShape(from, amountUnits, product, ref); err != nil {
		return code, err
	}
	key := supFeeRefKey(product, ref)
	var seen string
	switch err := tx.QueryRowContext(ctx, `SELECT tx_hash FROM sup_tx_history WHERE tx_hash=?`, key).Scan(&seen); {
	case err == nil:
		return "fee_ref_already_paid", ErrSUPFeeRefPaid
	case !errors.Is(err, sql.ErrNoRows):
		return "internal_error", err
	}
	var bal uint64
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(balance_sup_units,0) FROM accounts WHERE address=?`, from).Scan(&bal)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && bal < amountUnits) {
		return "insufficient_sup_balance", ErrSUPFeeInsufficent
	}
	if err != nil {
		return "internal_error", err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE accounts SET balance_sup_units=balance_sup_units-?, updated_at=strftime('%s','now') WHERE address=?`,
		amountUnits, from); err != nil {
		return "internal_error", err
	}
	burned, err := s.metaUint(ctx, tx, metaSUPTotalBurnedUnits, 0)
	if err != nil {
		return "internal_error", err
	}
	if err := s.upsertMetaUint(ctx, tx, metaSUPTotalBurnedUnits, burned+amountUnits); err != nil {
		return "internal_error", err
	}
	now := time.Now().Unix()
	row, _ := json.Marshal(map[string]any{
		"op": "burn_sup_for_fee", "from": from, "amount_units": amountUnits,
		"product": strings.TrimSpace(product), "ref": strings.TrimSpace(ref), "ts": now,
	})
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sup_tx_history (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, block_index, block_hash, applied_at, reject_code)
		 VALUES (?, ?, ?, '', 0, 0, ?, 'included', 0, 'burn', ?, '')`,
		key, string(row), from, amountUnits, now); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return "fee_ref_already_paid", ErrSUPFeeRefPaid
		}
		return "internal_error", err
	}
	return "", nil
}
