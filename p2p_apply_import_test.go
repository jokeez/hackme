package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hackme/internal/block"
	"hackme/internal/chain"
	"hackme/internal/store"
)

func TestApplyStagedLinearTailUsesImportPoHBlock(t *testing.T) {
	t.Setenv("HACKME_P2P_ALLOW_UNSIGNED_SYNC", "1")
	t.Setenv("HACKME_P2P_IMPORT_ORDER_ESCROW", "")
	a, db := newP2PApplyTestApp(t)
	ctx := context.Background()
	addr := "HMC-applyimport00001"
	if _, _, err := a.chain.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	var walletBefore uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM wallet WHERE id=1`).Scan(&walletBefore); err != nil {
		t.Fatal(err)
	}
	m, err := a.chain.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n, ev := firstPoHHitMain(m)
	h, tip, err := a.chain.Tip(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b := block.NewPoHBlock(h+1, tip, addr, n, ev, m, "", chain.PoHFormulaLabelForIndex(h+1))
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	stageBlock(t, ctx, db, b, string(raw))

	res, err := a.applyStagedLinearTail(ctx, 8)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Applied != 1 || res.Reason != "ok" {
		t.Fatalf("apply: %+v", res)
	}
	h2, tip2, _ := a.chain.Tip(ctx)
	if h2 != h+1 || tip2 != b.Hash {
		t.Fatalf("tip not advanced via import: h=%d tip=%s", h2, tip2)
	}
	var walletAfter uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM wallet WHERE id=1`).Scan(&walletAfter); err != nil {
		t.Fatal(err)
	}
	want := chain.HMCToUnits(chain.BaseRewardForBlockIndex(h + 1))
	if walletAfter != walletBefore+want {
		t.Fatalf("raw INSERT would skip reward credit; before=%d after=%d want +%d", walletBefore, walletAfter, want)
	}
	var staged int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM p2p_sync_stage`).Scan(&staged); err != nil {
		t.Fatal(err)
	}
	if staged != 0 {
		t.Fatalf("staged rows left=%d", staged)
	}
}

func TestApplyStagedLinearTailOrderEscrowGate(t *testing.T) {
	t.Setenv("HACKME_P2P_ALLOW_UNSIGNED_SYNC", "1")
	t.Setenv("HACKME_P2P_IMPORT_ORDER_ESCROW", "")
	a, db := newP2PApplyTestApp(t)
	ctx := context.Background()
	addr := "HMC-applyordgate0001"
	if _, _, err := a.chain.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	m, _ := a.chain.PoHTargetMod(ctx)
	n, ev := firstPoHHitMain(m)
	h, tip, _ := a.chain.Tip(ctx)
	b := block.NewPoHBlock(h+1, tip, addr, n, ev, m, "ord-ghost", chain.PoHFormulaLabelForIndex(h+1))
	raw, _ := json.Marshal(b)
	stageBlock(t, ctx, db, b, string(raw))

	res, err := a.applyStagedLinearTail(ctx, 8)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Applied != 0 {
		t.Fatalf("order-escrow block must not apply by default: %+v", res)
	}
	if !strings.Contains(res.Reason, "order_escrow_import_denied") {
		t.Fatalf("want order_escrow_import_denied reason, got %q", res.Reason)
	}
	h2, tip2, _ := a.chain.Tip(ctx)
	if h2 != h || tip2 != tip {
		t.Fatalf("tip moved despite escrow deny: h=%d tip=%s", h2, tip2)
	}
}

func newP2PApplyTestApp(t *testing.T) (*app, *sql.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "p2p-apply.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &app{chain: chain.New(db), db: db}, db
}

func stageBlock(t *testing.T, ctx context.Context, db *sql.DB, b *block.Block, raw string) {
	t.Helper()
	_, err := db.ExecContext(ctx,
		`INSERT INTO p2p_sync_stage (block_hash, block_index, prev_hash, peer_url, block_json, fetched_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		b.Hash, b.Index, b.PrevHash, "http://127.0.0.1:9", raw, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
}

func firstPoHHitMain(mod uint64) (nonce, eval uint64) {
	for n := uint64(0); n < 50_000_000; n++ {
		e := chain.PohEval(n)
		if mod > 0 && e%mod == 0 {
			return n, e
		}
	}
	panic("no hit in range")
}
