package chain

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"hackme/internal/block"
	"hackme/internal/store"
)

// TestReplayProdHistory replays a production chain snapshot block by block through the
// follower import path of the current code (opt-in: HACKME_REPLAY_PROD_SNAPSHOT=<sqlite copy>,
// optional HACKME_REPLAY_MAX_BLOCKS). It proves every historical block still validates
// (hash, signature, PoH formula, target mod / ASERT, timestamps, v2 header rules before the
// height) and that the replayed tip and block emission match the hub.
//
// Balances outside block emission (transfers, settlement, escrows) are not carried by blocks,
// so they are out of scope here — that is what state_root (v2.1) closes.
func TestReplayProdHistory(t *testing.T) {
	snap := os.Getenv("HACKME_REPLAY_PROD_SNAPSHOT")
	if snap == "" {
		t.Skip("set HACKME_REPLAY_PROD_SNAPSHOT to a prod chain DB copy")
	}
	ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(ResetEconomicsV2ActivationHeightForTest)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", "")
	ResetEconomicsV2ActivationHeightForTest()
	maxBlocks := uint64(0)
	if v := os.Getenv("HACKME_REPLAY_MAX_BLOCKS"); v != "" {
		maxBlocks, _ = strconv.ParseUint(v, 10, 64)
	}
	ctx := context.Background()
	src, err := sql.Open("sqlite", "file:"+snap+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	var genJSON, hubWallet string
	if err := src.QueryRow(`SELECT json FROM blocks WHERE block_index=0`).Scan(&genJSON); err != nil {
		t.Fatal(err)
	}
	_ = src.QueryRow(`SELECT address FROM wallet WHERE id=1`).Scan(&hubWallet)
	var gen block.Block
	if err := json.Unmarshal([]byte(genJSON), &gen); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(t.TempDir(), "replay.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	if _, _, err := svc.InitGenesis(ctx, gen.MinerAddress); err != nil {
		t.Fatal(err)
	}
	// Adopt the hub's exact genesis block (InitGenesis stamps a fresh time/id).
	if _, err := db.Exec(`UPDATE blocks SET hash=?, prev_hash=?, json=? WHERE block_index=0`, gen.Hash, gen.PrevHash, genJSON); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE meta SET value=? WHERE key='tip_hash'`, gen.Hash); err != nil {
		t.Fatal(err)
	}
	if hubWallet != "" {
		if _, err := db.Exec(`UPDATE wallet SET address=? WHERE id=1`, hubWallet); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := src.Query(`SELECT block_index, json FROM blocks WHERE block_index > 0 ORDER BY block_index`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	start := time.Now()
	var n, last, preASERT, orderBlocks uint64
	t.Setenv("HACKME_P2P_IMPORT_ORDER_ESCROW", "1")
	var hubEmission uint64
	for rows.Next() {
		var idx uint64
		var raw string
		if err := rows.Scan(&idx, &raw); err != nil {
			t.Fatal(err)
		}
		var b block.Block
		if err := json.Unmarshal([]byte(raw), &b); err != nil {
			t.Fatalf("block %d json: %v", idx, err)
		}
		if !ASERTActiveForIndex(idx) {
			// Pre-ASERT M came from the hub's wall-clock stall easing (node-local meta), which no
			// follower can recompute — followers bootstrap from a snapshot. Trust the header M
			// here; PoH work, hash, signature and formula are still verified against it.
			_, _, payloadMod, perr := pohFieldsFromPayload(b.Task.Payload)
			if perr != nil {
				t.Fatalf("block %d payload: %v", idx, perr)
			}
			if _, err := db.Exec(`UPDATE meta SET value=? WHERE key='poh_target_mod'`, strconv.FormatUint(payloadMod, 10)); err != nil {
				t.Fatal(err)
			}
			preASERT++
		}
		if _, tipHash, _ := svc.Tip(ctx); b.PrevHash != tipHash {
			// Known history anomaly: in May 2026 the hub rolled back from block 93 to 88 but kept
			// numbering, so block 94's parent is block 88 and 89–93 are an orphan side branch left
			// in the table. Any other such link is a failure.
			var parentIdx int64 = -1
			_ = db.QueryRow(`SELECT block_index FROM blocks WHERE hash=?`, b.PrevHash).Scan(&parentIdx)
			if idx != 94 || parentIdx != 88 {
				t.Fatalf("block %d: prev %s is not the replayed tip (parent index %d) — unknown fork", idx, b.PrevHash, parentIdx)
			}
			t.Logf("known anomaly: block %d builds on block %d (orphans %d..%d kept by the hub)", idx, parentIdx, parentIdx+1, idx-1)
			if _, err := db.Exec(`UPDATE meta SET value=? WHERE key='tip_hash'`, b.PrevHash); err != nil {
				t.Fatal(err)
			}
		}
		if oid := orderTaskIDFromPayload(b.Task.Payload); oid != "" {
			// Order-linked block: give the replay node the hub's order (open) and enough escrow for
			// this solve, so the escrow → miner credit path is exercised like on the hub.
			var reward float64
			var manifest, kind string
			// One solve at a time: liability = this block's reward, matched by the escrow top-up.
			if err := src.QueryRow(`SELECT reward, manifest_json, kind FROM tasks WHERE id=?`, oid).Scan(&reward, &manifest, &kind); err != nil {
				t.Fatalf("block %d: order %s missing in snapshot: %v", idx, oid, err)
			}
			if _, err := db.Exec(`INSERT INTO tasks (id, artifact_hash, reward, status, created_at, manifest_json, kind, target_solves, progress_count, payer_ref, prepaid_hmc, expires_at)
				VALUES (?, '', ?, ?, 0, ?, ?, 1, 0, 'replay', 0, 0)
				ON CONFLICT(id) DO UPDATE SET status=excluded.status, expires_at=0, target_solves=1, progress_count=0`, oid, reward, TaskStatusOpen, manifest, kind); err != nil {
				t.Fatal(err)
			}
			esc, _ := svc.metaUint(ctx, db, metaOrderEscrowUnits, 0)
			if err := svc.upsertMetaUint(ctx, db, metaOrderEscrowUnits, esc+HMCToUnits(reward)); err != nil {
				t.Fatal(err)
			}
			orderBlocks++
		}
		if err := svc.ImportPoHBlock(ctx, &b); err != nil {
			t.Fatalf("block %d (%s) rejected by current code: %v", idx, b.Hash, err)
		}
		if orderTaskIDFromPayload(b.Task.Payload) == "" {
			hubEmission += HMCToUnits(BaseRewardForBlockIndex(idx))
		}
		n++
		last = idx
		if n%50000 == 0 {
			t.Logf("replayed %d blocks (tip %d) in %s", n, idx, time.Since(start).Round(time.Second))
		}
		if maxBlocks > 0 && n >= maxBlocks {
			break
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	h, tip, err := svc.Tip(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var srcHash string
	if err := src.QueryRow(`SELECT hash FROM blocks WHERE block_index=?`, last).Scan(&srcHash); err != nil {
		t.Fatal(err)
	}
	if h != last || tip != srcHash {
		t.Fatalf("replayed tip %d %s, hub block %d %s", h, tip, last, srcHash)
	}
	minted, _, err := svc.econTotalsUnits(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	genesisUnits := HMCToUnits(block.GenesisRewardHMC)
	t.Logf("strict ASERT-validated blocks: %d, pre-ASERT (header M trusted, PoH verified): %d, order-linked: %d", n-preASERT, preASERT, orderBlocks)
	t.Logf("replayed %d blocks to tip %d (%s) in %s; minted %d units (genesis %d + blocks %d), schedule says %d",
		n, h, tip[:16], time.Since(start).Round(time.Second), minted, genesisUnits, minted-genesisUnits, hubEmission)
	if maxBlocks == 0 {
		var hubMinted string
		_ = src.QueryRow(`SELECT value FROM meta WHERE key=?`, metaTotalMintedUnits).Scan(&hubMinted)
		t.Logf("hub meta total_minted_units=%s (includes non-block mints, if any)", hubMinted)
	}
	if minted-genesisUnits != hubEmission {
		t.Fatalf("block emission %d != reward schedule %d", minted-genesisUnits, hubEmission)
	}
}
