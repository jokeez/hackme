package chain

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"hackme/internal/block"
)

// ErrImportOrderEscrowDenied is returned when a follower tries to import a PoH block
// that spends order escrow without HACKME_P2P_IMPORT_ORDER_ESCROW=1.
var ErrImportOrderEscrowDenied = errors.New("chain: import of order-escrow PoH blocked (set HACKME_P2P_IMPORT_ORDER_ESCROW=1)")

func importOrderEscrowAllowed() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_P2P_IMPORT_ORDER_ESCROW")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// ImportPoHBlock replays a previously mined/signed PoH block onto this node with ledger effects.
// Unlike raw P2P INSERT, this credits rewards / order escrow the same way AppendPoHBlock does.
//
// HMC-RES-01: blocks carrying order_task_id are rejected unless HACKME_P2P_IMPORT_ORDER_ESCROW=1,
// because followers typically lack the payer's escrow + WASM gate state required for safe credit.
func (s *Service) ImportPoHBlock(ctx context.Context, b *block.Block) error {
	if b == nil {
		return errors.New("chain: nil block")
	}
	if strings.TrimSpace(b.Task.Kind) != block.PoHBlockKind {
		return fmt.Errorf("chain: import supports %s only, got %q", block.PoHBlockKind, b.Task.Kind)
	}
	if err := verifyBlockIntegrityAndSignature(b); err != nil {
		return err
	}

	orderTaskID := orderTaskIDFromPayload(b.Task.Payload)
	if orderTaskID != "" && !importOrderEscrowAllowed() {
		return fmt.Errorf("%w: order_task_id=%s", ErrImportOrderEscrowDenied, orderTaskID)
	}

	nonce, eval, mod, err := pohFieldsFromPayload(b.Task.Payload)
	if err != nil {
		return err
	}
	// Prefer header nonce when present (payload mirrors it for PoH blocks).
	if b.Nonce != 0 {
		nonce = b.Nonce
	}
	if err := validatePoHSubmission(b.Index, nonce, eval, mod); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var tipHash string
	err = s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'tip_hash'`).Scan(&tipHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var maxIdx sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(block_index) FROM blocks`).Scan(&maxIdx); err != nil {
		return err
	}
	if !maxIdx.Valid {
		return errors.New("chain: empty (genesis required before import)")
	}
	expectIdx := uint64(maxIdx.Int64) + 1
	if b.Index != expectIdx {
		return fmt.Errorf("chain: import index mismatch (want %d got %d)", expectIdx, b.Index)
	}
	if strings.TrimSpace(b.PrevHash) != strings.TrimSpace(tipHash) {
		return fmt.Errorf("chain: import prev_hash mismatch")
	}

	rewardHMC := BaseRewardForBlockIndex(b.Index)
	if orderTaskID != "" {
		var orderReward float64
		now := b.Timestamp
		if now <= 0 {
			now = time.Now().Unix()
		}
		if err := s.db.QueryRowContext(ctx,
			`SELECT reward FROM tasks WHERE id = ? AND status = ? AND (expires_at = 0 OR expires_at > ?)`,
			orderTaskID, TaskStatusOpen, now,
		).Scan(&orderReward); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("chain: order task %q not open or missing (refuse import)", orderTaskID)
			}
			return err
		}
		rewardHMC = orderReward
	}

	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := b.Timestamp
	if now <= 0 {
		now = time.Now().Unix()
	}
	if _, err := s.expireOpenOrderTasksTx(ctx, tx, now); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO blocks (block_index, hash, prev_hash, json) VALUES (?,?,?,?)`,
		b.Index, b.Hash, b.PrevHash, string(raw)); err != nil {
		return err
	}
	if orderTaskID != "" {
		var orderReward float64
		if err := tx.QueryRowContext(ctx,
			`SELECT reward FROM tasks WHERE id = ? AND status = ? AND (expires_at = 0 OR expires_at > ?)`,
			orderTaskID, TaskStatusOpen, now,
		).Scan(&orderReward); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("chain: order task %q not open or missing (refuse import)", orderTaskID)
			}
			return err
		}
		if HMCToUnits(orderReward) != HMCToUnits(rewardHMC) {
			return fmt.Errorf("chain: order reward mismatch for %q", orderTaskID)
		}
	}
	if err := s.bumpOrderTaskProgress(ctx, tx, orderTaskID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE meta SET value = ? WHERE key = 'tip_hash'`, b.Hash); err != nil {
		return err
	}

	if rewardHMC > 0 {
		mintedUnits, burnedUnits, err := s.econTotalsUnits(ctx, tx)
		if err != nil {
			return err
		}
		credit := HMCToUnits(rewardHMC)
		if orderTaskID != "" {
			minerAddress := strings.TrimSpace(b.EffectiveMinerAddress())
			if minerAddress == "" || !strings.HasPrefix(minerAddress, "HMC-") {
				return errors.New("chain: valid miner_address required for order reward credit")
			}
			escrowUnits, err := s.metaUint(ctx, tx, metaOrderEscrowUnits, 0)
			if err != nil {
				return err
			}
			if credit > escrowUnits {
				return fmt.Errorf("chain: order escrow depleted (%d < %d)", escrowUnits, credit)
			}
			if err := s.creditUnits(ctx, tx, minerAddress, credit); err != nil {
				return err
			}
			if err := s.upsertMetaUint(ctx, tx, metaOrderEscrowUnits, escrowUnits-credit); err != nil {
				return err
			}
			if err := s.upsertMetaFloat(ctx, tx, metaTotalMintedHMC, UnitsToHMC(mintedUnits)); err != nil {
				return err
			}
			if err := s.upsertMetaFloat(ctx, tx, metaTotalBurnedHMC, UnitsToHMC(burnedUnits)); err != nil {
				return err
			}
		} else {
			var rewardCreditAddr string
			if err := tx.QueryRowContext(ctx, `SELECT address FROM wallet WHERE id = 1`).Scan(&rewardCreditAddr); err != nil {
				return err
			}
			rewardCreditAddr = strings.TrimSpace(rewardCreditAddr)
			if rewardCreditAddr == "" {
				return errors.New("chain: primary wallet address missing (cannot credit PoH reward)")
			}
			maxSupplyUnits := HMCToUnits(MaxSupplyHMC)
			var allowedUnits uint64
			if mintedUnits < maxSupplyUnits {
				remainingUnits := maxSupplyUnits - mintedUnits
				if credit > remainingUnits {
					allowedUnits = remainingUnits
				} else {
					allowedUnits = credit
				}
			}
			if allowedUnits > 0 {
				if err := s.creditUnits(ctx, tx, rewardCreditAddr, allowedUnits); err != nil {
					return err
				}
				if err := s.upsertMetaUint(ctx, tx, metaTotalMintedUnits, mintedUnits+allowedUnits); err != nil {
					return err
				}
				if err := s.upsertMetaFloat(ctx, tx, metaTotalMintedHMC, UnitsToHMC(mintedUnits+allowedUnits)); err != nil {
					return err
				}
				if err := s.upsertMetaUint(ctx, tx, metaTotalBurnedUnits, burnedUnits); err != nil {
					return err
				}
			}
		}
	}

	if err := s.applyPendingTransfers(ctx, tx, b.Index, b.Hash); err != nil {
		return err
	}
	if err := s.applyPendingSupTransfers(ctx, tx, b.Index, b.Hash); err != nil {
		return err
	}

	// Advance target mod using the imported block's timestamp (matches AppendPoHBlock retarget).
	nextMod, err := s.nextPoHTargetModTx(ctx, tx, b, mod)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES ('poh_target_mod', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		strconv.FormatUint(nextMod, 10)); err != nil {
		return err
	}
	if err := s.checkEconomicInvariants(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func pohFieldsFromPayload(payload []byte) (nonce, eval, mod uint64, err error) {
	if len(payload) == 0 {
		return 0, 0, 0, errors.New("chain: empty poh payload")
	}
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		return 0, 0, 0, err
	}
	nonce, err = anyToUint64(m["nonce"])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("chain: payload nonce: %w", err)
	}
	eval, err = anyToUint64(m["eval"])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("chain: payload eval: %w", err)
	}
	mod, err = anyToUint64(m["mod"])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("chain: payload mod: %w", err)
	}
	mod = ClampPoHTargetMod(mod)
	return nonce, eval, mod, nil
}

func anyToUint64(v any) (uint64, error) {
	switch x := v.(type) {
	case float64:
		if x < 0 {
			return 0, errors.New("negative")
		}
		return uint64(x), nil
	case json.Number:
		u, err := strconv.ParseUint(string(x), 10, 64)
		return u, err
	case string:
		return strconv.ParseUint(strings.TrimSpace(x), 10, 64)
	case nil:
		return 0, errors.New("missing")
	default:
		return 0, fmt.Errorf("unsupported type %T", v)
	}
}

func (s *Service) nextPoHTargetModTx(ctx context.Context, tx *sql.Tx, b *block.Block, currentMod uint64) (uint64, error) {
	w := uint64(PoHRetargetWindowBlocks)
	nextIdx := b.Index
	now := b.Timestamp
	if now <= 0 {
		now = time.Now().Unix()
	}
	if nextIdx >= w && nextIdx%w == 0 {
		anchorIdx := nextIdx - w
		var anchorRaw string
		if err := tx.QueryRowContext(ctx, `SELECT json FROM blocks WHERE block_index = ?`, anchorIdx).Scan(&anchorRaw); err != nil {
			return 0, err
		}
		var anchorB block.Block
		if err := json.Unmarshal([]byte(anchorRaw), &anchorB); err != nil {
			return 0, err
		}
		actualSec := now - anchorB.Timestamp
		if actualSec < 1 {
			actualSec = 1
		}
		idealSec := PoHRetargetWindowBlocks * PoHRetargetTargetSec
		return ClampPoHTargetMod(RetargetWindow(currentMod, actualSec, idealSec)), nil
	}
	var prevJSON string
	if err := tx.QueryRowContext(ctx, `SELECT json FROM blocks WHERE hash = ?`, b.PrevHash).Scan(&prevJSON); err != nil {
		return 0, err
	}
	var prev block.Block
	if err := json.Unmarshal([]byte(prevJSON), &prev); err != nil {
		return 0, err
	}
	lastDeltaSec := now - prev.Timestamp
	if lastDeltaSec < 1 {
		lastDeltaSec = 1
	}
	return ClampPoHTargetMod(RetargetMicroStep(currentMod, lastDeltaSec, PoHRetargetTargetSec)), nil
}
