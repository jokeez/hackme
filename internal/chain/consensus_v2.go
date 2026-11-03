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

// V2ChainID is the default public-network id from the Economics v2 activation height.
// Lab/test nodes must set HACKME_V2_CHAIN_ID to a distinct value so signed blocks/txs
// cannot be replayed across environments (audit H4).
const V2ChainID = "hackme-mainnet-1"

// ResolvedV2ChainID returns the active v2 network id (env override or V2ChainID).
func ResolvedV2ChainID() string {
	if v := strings.TrimSpace(os.Getenv("HACKME_V2_CHAIN_ID")); v != "" {
		return v
	}
	return V2ChainID
}

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
	if err := s.lockV2ChainIDTx(ctx, tx); err != nil {
		return err
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
	wantID := ResolvedV2ChainID()
	if b.ChainID != wantID {
		return fmt.Errorf("%w: chain_id %q, want %q", ErrV2Header, b.ChainID, wantID)
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
		return ResolvedV2ChainID()
	}
	return s.ChainID(ctx)
}

// checkTxChainID: a transfer (HMC, SUP, HMS) applied in block idx must be signed for that
// block's network from Economics v2 on; before v2 the field stays empty (legacy signatures).
func checkTxChainID(idx uint64, chainID string) (string, string) {
	chainID = strings.TrimSpace(chainID)
	if EconomicsV2ActiveForIndex(idx) {
		want := ResolvedV2ChainID()
		if chainID != want {
			return "chain_id_mismatch", fmt.Sprintf("tx chain_id %q, network is %s", chainID, want)
		}
		return "", ""
	}
	if chainID != "" {
		return "chain_id_mismatch", "chain_id is accepted from the Economics v2 activation height"
	}
	return "", ""
}

func (s *Service) nextBlockIndexQ(ctx context.Context, q queryRowContext) (uint64, error) {
	var maxIdx sql.NullInt64
	if err := q.QueryRowContext(ctx, `SELECT MAX(block_index) FROM blocks`).Scan(&maxIdx); err != nil {
		return 0, err
	}
	if !maxIdx.Valid || maxIdx.Int64 < 0 {
		return 1, nil
	}
	return uint64(maxIdx.Int64) + 1, nil
}

// checkTxChainIDNextQ validates a submitted transfer against the next block's network.
func (s *Service) checkTxChainIDNextQ(ctx context.Context, q queryRowContext, chainID string) (string, string) {
	idx, err := s.nextBlockIndexQ(ctx, q)
	if err != nil {
		return "internal_error", err.Error()
	}
	return checkTxChainID(idx, chainID)
}

// TxChainIDForNextBlock is the chain_id a signer must put in a new transfer ("" before v2).
func (s *Service) TxChainIDForNextBlock(ctx context.Context) string {
	idx, err := s.nextBlockIndexQ(ctx, s.db)
	if err != nil || !EconomicsV2ActiveForIndex(idx) {
		return ""
	}
	return ResolvedV2ChainID()
}

// NetworkChainID is the network id of the next block (status / explorer display).
func (s *Service) NetworkChainID(ctx context.Context) string {
	idx, err := s.nextBlockIndexQ(ctx, s.db)
	if err != nil {
		return s.ChainID(ctx)
	}
	return s.ChainIDForIndex(ctx, idx)
}

const metaV2ChainID = "v2_chain_id"

// lockV2ChainIDTx pins the resolved v2 network id (HACKME_V2_CHAIN_ID or V2ChainID) next to the
// authority key: a later env change would silently move the node to another network (its blocks
// and transfers rejected everywhere else), so it stops with an explicit error instead.
func (s *Service) lockV2ChainIDTx(ctx context.Context, tx queryRowExecContext) error {
	want := ResolvedV2ChainID()
	var pinned string
	if err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, metaV2ChainID).Scan(&pinned); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if pinned = strings.TrimSpace(pinned); pinned != "" {
		if pinned != want {
			return fmt.Errorf("chain: v2 chain_id %q (HACKME_V2_CHAIN_ID) disagrees with pinned %q", want, pinned)
		}
		return nil
	}
	return s.upsertMeta(ctx, tx, metaV2ChainID, want)
}
