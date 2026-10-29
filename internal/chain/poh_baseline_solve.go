package chain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"

	"hackme/internal/block"
)

// baselineSolveMu serializes empty-block mint from pool relays so two concurrent
// accepts cannot both credit primary wallet for the same tip race window.
var baselineSolveMu sync.Mutex

// SubmitBaselinePoHSolve validates a pool-reported baseline (non-order) PoH hit and
// appends the next empty block. Subsidy credits the primary wallet (settlement payer),
// not minerAddress — same rule as local empty AppendPoHBlock.
func (s *Service) SubmitBaselinePoHSolve(ctx context.Context, minerAddress string, nonce, targetMod uint64) (*block.Block, error) {
	minerAddress = strings.TrimSpace(minerAddress)
	if minerAddress == "" {
		return nil, errors.New("chain: miner_address required")
	}
	if !strings.HasPrefix(minerAddress, "HMC-") {
		return nil, fmt.Errorf("chain: invalid miner_address %q", minerAddress)
	}

	baselineSolveMu.Lock()
	defer baselineSolveMu.Unlock()

	// Always use canonical chain target_mod — ignore caller-supplied difficulty.
	chainMod, err := s.PoHTargetMod(ctx)
	if err != nil {
		return nil, err
	}
	targetMod = ClampPoHTargetMod(chainMod)
	var tipHash string
	var maxIdx sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'tip_hash'`).Scan(&tipHash); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(block_index) FROM blocks`).Scan(&maxIdx); err != nil {
		return nil, err
	}
	if !maxIdx.Valid {
		return nil, errors.New("chain: empty (genesis required)")
	}
	nextIdx := uint64(maxIdx.Int64) + 1
	eval := PohWorkForIndex(nextIdx, nonce, tipHash, minerAddress)
	if targetMod == 0 || eval%targetMod != 0 {
		return nil, errors.New("chain: invalid poh solution for submitted target_mod")
	}

	reward, err := s.BaseRewardForNextBlock(ctx)
	if err != nil {
		return nil, err
	}
	return s.AppendPoHBlock(ctx, minerAddress, nonce, eval, reward, targetMod, "")
}
