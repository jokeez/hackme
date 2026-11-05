package chain

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
)

// Address ↔ key binding (Economics v2).
//
// An address is derived from the spending key, but the derivation alone is all that ties the two
// together. From the v2 activation height every spend (HMC/SUP/HMS transfers, signed SUP burns)
// must also use the key bound to its address:
//
//   - the first included signed spend binds its key (history is seeded by the store migration);
//   - an owner can bind before ever spending with a signed bind proof (BindAddressPubKey), which
//     desktop wallets send on start;
//   - once bound, a spend signed by any other key is rejected (pubkey_binding_mismatch).
//
// Bindings are recorded before activation too, so the table is complete when enforcement starts.

const (
	PubKeyBindSourceHMC   = "spend_hmc"
	PubKeyBindSourceSUP   = "spend_sup"
	PubKeyBindSourceHMS   = "spend_hms"
	PubKeyBindSourceBurn  = "burn_sup"
	PubKeyBindSourceProof = "bind_proof"
)

// ErrPubKeyBindingMismatch: the address is bound to a different key.
var ErrPubKeyBindingMismatch = errors.New("chain: address is bound to a different key")

// AddressBindMessage is the canonical message an owner signs to bind its key to its address.
// It carries no chain id: the statement "this key owns this address" is the same on every network.
func AddressBindMessage(address, pubKeyHex string) []byte {
	return []byte("hackme-address-bind-v1|" + strings.TrimSpace(address) + "|" + normPubKeyHex(pubKeyHex))
}

func normPubKeyHex(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

// PubKeyBindingEnforcedForIndex reports whether spends applied in block idx must match the binding.
func PubKeyBindingEnforcedForIndex(idx uint64) bool {
	return EconomicsV2ActiveForIndex(idx)
}

// BoundPubKey returns the key bound to address ("" when unbound).
func (s *Service) BoundPubKey(ctx context.Context, address string) (string, error) {
	return boundPubKeyQ(ctx, s.db, address)
}

func boundPubKeyQ(ctx context.Context, q queryRowContext, address string) (string, error) {
	var v string
	err := q.QueryRowContext(ctx, `SELECT pubkey_ed25519 FROM address_pubkeys WHERE address=?`, strings.TrimSpace(address)).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return normPubKeyHex(v), nil
}

// checkPubKeyBindingQ enforces the binding for a spend applied in block idx (no-op before v2).
func checkPubKeyBindingQ(ctx context.Context, q queryRowContext, idx uint64, from, pubKeyHex string) (string, string) {
	if !PubKeyBindingEnforcedForIndex(idx) {
		return "", ""
	}
	bound, err := boundPubKeyQ(ctx, q, from)
	if err != nil {
		return "internal_error", err.Error()
	}
	if bound != "" && bound != normPubKeyHex(pubKeyHex) {
		return "pubkey_binding_mismatch", ErrPubKeyBindingMismatch.Error()
	}
	return "", ""
}

// bindPubKeyQ records the first key seen for address (an existing binding is never replaced).
func bindPubKeyQ(ctx context.Context, q queryRowExecContext, address, pubKeyHex string, idx uint64, source string) error {
	pub := normPubKeyHex(pubKeyHex)
	if len(pub) != 2*ed25519.PublicKeySize {
		return nil
	}
	_, err := q.ExecContext(ctx,
		`INSERT INTO address_pubkeys (address, pubkey_ed25519, bound_at_index, source) VALUES (?, ?, ?, ?)
		 ON CONFLICT(address) DO NOTHING`,
		strings.TrimSpace(address), pub, idx, source)
	return err
}

// BindAddressPubKey binds pubKeyHex to address from a signed bind proof (owner-initiated, before
// the first spend). Returns "bound", "already_bound", or an error code. Only funded/known
// addresses can be bound, so the table cannot be filled with throwaway keys.
func (s *Service) BindAddressPubKey(ctx context.Context, address, pubKeyHex, sigHex string) (string, error) {
	address = strings.TrimSpace(address)
	if IsLongAddress(address) {
		// The long address is the full key hash: nothing to bind.
		return "not_needed", errors.New("long addresses need no binding")
	}
	if !IsLegacyAddress(address) {
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
	if derived, err := addressFromPubKeyHex(pubKeyHex); err != nil || derived != address {
		return "address_pubkey_mismatch", errors.New("address does not match pubkey")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), AddressBindMessage(address, pubKeyHex), sig) {
		return "invalid_signature", errors.New("signature verify failed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var one int
	switch err := s.db.QueryRowContext(ctx, `SELECT 1 FROM accounts WHERE address=?`, address).Scan(&one); {
	case errors.Is(err, sql.ErrNoRows):
		return "unknown_address", errors.New("address has no account yet")
	case err != nil:
		return "internal_error", err
	}
	idx, err := s.nextBlockIndexQ(ctx, s.db)
	if err != nil {
		return "internal_error", err
	}
	bound, err := boundPubKeyQ(ctx, s.db, address)
	if err != nil {
		return "internal_error", err
	}
	if bound != "" {
		if bound != normPubKeyHex(pubKeyHex) {
			return "pubkey_binding_mismatch", ErrPubKeyBindingMismatch
		}
		return "already_bound", nil
	}
	if err := bindPubKeyQ(ctx, s.db, address, pubKeyHex, idx, PubKeyBindSourceProof); err != nil {
		return "internal_error", err
	}
	return "bound", nil
}
