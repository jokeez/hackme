package chain

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"hackme/internal/block"
)

// Economics v2 consensus header rules (from the activation height):
//
//   - chain_id must be V2ChainID: a block (or signature) from a lab/test network cannot be replayed here.
//   - reward_to names the empty-block subsidy recipient inside the signed header, so every node
//     credits the same account. Before this, followers credited their own local wallet and the
//     state could not be reproduced from blocks. Order-linked blocks pay the miner from escrow
//     and carry no reward_to.
//   - The block must be signed by the authority key pinned in chain meta (unsigned blocks and
//     blocks signed by any other key are rejected).
//
// Blocks below the height must not carry the new fields (legacy hashes stay unchanged).

// V2ChainID is the network id from the Economics v2 activation height.
const V2ChainID = "hackme-mainnet-1"

const metaAuthorityPubKey = "authority_pubkey_ed25519"

var (
	ErrV2Header           = errors.New("chain: economics v2 block header invalid")
	ErrAuthorityPubKeyCfg = errors.New("chain: authority pubkey not configured (set HACKME_CHAIN_AUTHORITY_PUBKEY)")
)

func authorityPubKeyFromEnv() string {
	return strings.ToLower(strings.TrimSpace(os.Getenv("HACKME_CHAIN_AUTHORITY_PUBKEY")))
}

func validAuthorityPubKeyHex(v string) bool {
	raw, err := hex.DecodeString(v)
	return err == nil && len(raw) == 32
}

// lockAuthorityPubKeyTx pins the block-signing key once Economics v2 is configured: env, else the
// node's own signer (the hub). A follower without either stays unpinned and refuses v2 blocks.
// Later env must agree with the pin. Caller holds the append/import tx.
func (s *Service) lockAuthorityPubKeyTx(ctx context.Context, tx queryRowExecContext) error {
	if EconomicsV2ActivationHeight() < 1 {
		return nil
	}
	var pinned string
	if err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, metaAuthorityPubKey).Scan(&pinned); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	pinned = strings.ToLower(strings.TrimSpace(pinned))
	env := authorityPubKeyFromEnv()
	if pinned != "" {
		if env != "" && env != pinned {
			return fmt.Errorf("chain: HACKME_CHAIN_AUTHORITY_PUBKEY=%s disagrees with pinned authority %s", env, pinned)
		}
		return nil
	}
	key := env
	if key == "" && s.signer != nil {
		key = strings.ToLower(strings.TrimSpace(s.signer.PublicKeyHex()))
	}
	if key == "" {
		return nil
	}
	if !validAuthorityPubKeyHex(key) {
		return fmt.Errorf("chain: invalid authority pubkey %q", key)
	}
	return s.upsertMeta(ctx, tx, metaAuthorityPubKey, key)
}

func (s *Service) authorityPubKeyQ(ctx context.Context, q queryRowContext) (string, error) {
	var v string
	if err := q.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, metaAuthorityPubKey).Scan(&v); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return strings.ToLower(strings.TrimSpace(v)), nil
}

// validateV2HeaderQ enforces the v2 header rules for b (call after lockAuthorityPubKeyTx in the
// same tx). Signature bytes are checked by verifyBlockIntegrityAndSignature.
func (s *Service) validateV2HeaderQ(ctx context.Context, q queryRowContext, b *block.Block, orderTaskID string) error {
	if !EconomicsV2ActiveForIndex(b.Index) {
		if b.ChainID != "" || b.RewardTo != "" {
			return fmt.Errorf("%w: chain_id/reward_to before activation (index %d)", ErrV2Header, b.Index)
		}
		return nil
	}
	if b.ChainID != V2ChainID {
		return fmt.Errorf("%w: chain_id %q, want %q", ErrV2Header, b.ChainID, V2ChainID)
	}
	if strings.TrimSpace(orderTaskID) == "" {
		if !strings.HasPrefix(b.RewardTo, "HMC-") || strings.TrimSpace(b.RewardTo) != b.RewardTo {
			return fmt.Errorf("%w: reward_to required on empty blocks", ErrV2Header)
		}
	} else if b.RewardTo != "" {
		return fmt.Errorf("%w: order blocks pay from escrow, reward_to must be empty", ErrV2Header)
	}
	if b.MinerPubKey == "" || b.MinerSig == "" {
		return fmt.Errorf("%w: authority signature required", ErrV2Header)
	}
	auth, err := s.authorityPubKeyQ(ctx, q)
	if err != nil {
		return err
	}
	if auth == "" {
		return ErrAuthorityPubKeyCfg
	}
	if strings.ToLower(strings.TrimSpace(b.MinerPubKey)) != auth {
		return fmt.Errorf("%w: block signed by %s, authority is %s", ErrV2Header, b.MinerPubKey, auth)
	}
	return nil
}

// ChainIDForIndex is the network id that block (or a signature applied in it) belongs to.
func (s *Service) ChainIDForIndex(ctx context.Context, index uint64) string {
	if EconomicsV2ActiveForIndex(index) {
		return V2ChainID
	}
	return s.ChainID(ctx)
}
