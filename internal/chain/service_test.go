package chain

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"hackme/internal/block"
	"hackme/internal/nodecrypto"
	"hackme/internal/store"
)

func firstPoHHit(mod uint64) (nonce, eval uint64) {
	for n := uint64(0); n < 50_000_000; n++ {
		e := PohEval(n)
		if mod > 0 && e%mod == 0 {
			return n, e
		}
	}
	panic("no hit in range")
}

func TestAppendPoHBlockAndMetaRetarget(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "chain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := New(db)
	addr := "HMC-testnode"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}

	m0, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if m0 != DefaultPoHTargetMod {
		t.Fatalf("expected default mod %d, got %d", DefaultPoHTargetMod, m0)
	}

	nonce, eval := firstPoHHit(m0)
	b, err := svc.AppendPoHBlock(ctx, addr, nonce, eval, 0.01, m0, "")
	if err != nil {
		t.Fatal(err)
	}
	if b.Index != 1 {
		t.Fatalf("index: %d", b.Index)
	}

	var raw string
	if err := db.QueryRowContext(ctx, `SELECT json FROM blocks WHERE block_index = 1`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var stored block.Block
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		t.Fatal(err)
	}
	var meta struct {
		Mod uint64 `json:"mod"`
	}
	if err := json.Unmarshal(stored.Task.Payload, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Mod != m0 {
		t.Fatalf("payload mod: %d want %d", meta.Mod, m0)
	}

	m1, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if m1 < pohRetargetMinMod || m1 > pohRetargetMaxMod {
		t.Fatalf("meta mod out of bounds: %d", m1)
	}
	if m1 == 0 {
		t.Fatalf("after block 1 target mod must stay positive, got %d", m1)
	}

	n2, e2 := firstPoHHit(m1)
	b2, err := svc.AppendPoHBlock(ctx, addr, n2, e2, 0.01, m1, "")
	if err != nil {
		t.Fatal(err)
	}
	if b2.Index != 2 {
		t.Fatalf("index2: %d", b2.Index)
	}
	m2, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if m2 == 0 {
		t.Fatalf("after block 2 target mod must stay positive, got %d", m2)
	}
}

func TestTipReturnsLatestHeightAndHash(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "tip.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := New(db)
	addr := "HMC-tip"
	g, _, err := svc.InitGenesis(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	h0, tip0, err := svc.Tip(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if h0 != 0 || tip0 != g.Hash {
		t.Fatalf("tip after genesis mismatch: h=%d tip=%q want h=0 tip=%q", h0, tip0, g.Hash)
	}

	m0, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	nonce, eval := firstPoHHit(m0)
	b, err := svc.AppendPoHBlock(ctx, addr, nonce, eval, 0.01, m0, "")
	if err != nil {
		t.Fatal(err)
	}
	h1, tip1, err := svc.Tip(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != 1 || tip1 != b.Hash {
		t.Fatalf("tip after block mismatch: h=%d tip=%q want h=1 tip=%q", h1, tip1, b.Hash)
	}
}

func TestAppendPoHBlockStaleModMismatch(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "chain_stale.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := New(db)
	addr := "HMC-stale"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE meta SET value = ? WHERE key = 'poh_target_mod'`, "500000"); err != nil {
		t.Fatal(err)
	}
	nonce, eval := firstPoHHit(1_000_000)
	_, err = svc.AppendPoHBlock(ctx, addr, nonce, eval, 0.01, 1_000_000, "")
	if err == nil {
		t.Fatal("expected mismatch when submitted mod != chain meta")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("expected mismatch, got: %v", err)
	}
}

func TestAppendPoHBlockInvalidEval(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "chain2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := New(db)
	if _, _, err := svc.InitGenesis(ctx, "HMC-x"); err != nil {
		t.Fatal(err)
	}
	_, err = svc.AppendPoHBlock(ctx, "HMC-x", 1, 999, 0.01, DefaultPoHTargetMod, "")
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestAppendPoHBlockRespectsMaxSupply(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "chain_cap.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := New(db)
	addr := "HMC-cap"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	// Near-cap minted totals: unit meta is authoritative; leave ~0.25 HMC headroom for first PoH (requested 1.0 HMC, capped).
	nearCapUnits := HMCToUnits(MaxSupplyHMC) - HMCToUnits(0.25)
	if _, err := db.ExecContext(ctx, `UPDATE meta SET value = ? WHERE key = ?`, strconv.FormatUint(nearCapUnits, 10), metaTotalMintedUnits); err != nil {
		t.Fatal(err)
	}
	nearHMC := UnitsToHMC(nearCapUnits)
	if _, err := db.ExecContext(ctx, `UPDATE meta SET value = ? WHERE key = ?`, strconv.FormatFloat(nearHMC, 'f', -1, 64), metaTotalMintedHMC); err != nil {
		t.Fatal(err)
	}
	m, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n1, e1 := firstPoHHit(m)
	if _, err := svc.AppendPoHBlock(ctx, addr, n1, e1, 1.0, m, ""); err != nil {
		t.Fatal(err)
	}
	_, bal, err := svc.Wallet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if bal < 0.2499 || bal > 0.2501 {
		t.Fatalf("wallet after capped reward: %v", bal)
	}
	ec, err := svc.Economics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ec.TotalMinted < MaxSupplyHMC-1e-9 || ec.TotalMinted > MaxSupplyHMC+1e-9 {
		t.Fatalf("minted should hit cap: %v", ec.TotalMinted)
	}
	m2, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var n2, e2 uint64
	for x := n1 + 1; x < n1+20_000_000; x++ {
		ev := PohEval(x)
		if ev%m2 == 0 {
			n2, e2 = x, ev
			break
		}
	}
	if n2 == 0 {
		t.Fatal("no second hit found")
	}
	if _, err := svc.AppendPoHBlock(ctx, addr, n2, e2, 1.0, m2, ""); err != nil {
		t.Fatal(err)
	}
	_, bal2, err := svc.Wallet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if bal2 < bal-1e-9 || bal2 > bal+1e-9 {
		t.Fatalf("wallet must not grow after cap reached: before=%v after=%v", bal, bal2)
	}
}

func TestAppendPoHBlockRejectsEconomicInvariantViolation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "chain_inv.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := New(db)
	addr := "HMC-inv"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	// Corrupt economics state: burned > minted must be rejected at block append (unit meta is authoritative).
	// Burn must stay above minted even after this PoH credits ~0.01 HMC (see AppendPoHBlock meta updates).
	burnUnits := HMCToUnits(MaxSupplyHMC) + 1
	if _, err := db.ExecContext(ctx, `UPDATE meta SET value = ? WHERE key = ?`, strconv.FormatUint(burnUnits, 10), metaTotalBurnedUnits); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE meta SET value = ? WHERE key = ?`, "99999999", metaTotalBurnedHMC); err != nil {
		t.Fatal(err)
	}
	m, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n, e := firstPoHHit(m)
	_, err = svc.AppendPoHBlock(ctx, addr, n, e, 0.01, m, "")
	if err == nil {
		t.Fatal("expected economic invariant violation")
	}
	if !strings.Contains(err.Error(), "economic invariant") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPoHBlockCountSinceAndSummaries(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "chain_reports.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := New(db)
	addr := "HMC-reports"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	m0, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	nonce, eval := firstPoHHit(m0)
	if _, err := svc.AppendPoHBlock(ctx, addr, nonce, eval, 0.01, m0, ""); err != nil {
		t.Fatal(err)
	}

	future := time.Now().Unix() + 3600
	if n, err := svc.PoHBlockCountSince(ctx, future); err != nil {
		t.Fatal(err)
	} else if n != 0 {
		t.Fatalf("future since: %d", n)
	}
	if n, err := svc.PoHBlockCountSince(ctx, 0); err != nil {
		t.Fatal(err)
	} else if n != 1 {
		t.Fatalf("since 0: want 1 PoH got %d", n)
	}

	sums, err := svc.ListRecentBlockSummaries(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) < 2 {
		t.Fatalf("want genesis+poh, got %d", len(sums))
	}
	if sums[0].Index < sums[1].Index {
		t.Fatal("expected newest-first order")
	}
	if sums[0].TaskKind != block.PoHBlockKind {
		t.Fatalf("tip task kind: %q", sums[0].TaskKind)
	}
	if sums[0].MinerAddress != addr {
		t.Fatalf("tip miner_address: want %q got %q", addr, sums[0].MinerAddress)
	}
	got, ok, err := svc.GetBlockSummaryByIndex(ctx, sums[0].Index)
	if err != nil || !ok {
		t.Fatalf("GetBlockSummaryByIndex: ok=%v err=%v", ok, err)
	}
	if got.MinerAddress != addr || got.TaskID != sums[0].TaskID {
		t.Fatalf("GetBlockSummaryByIndex mismatch: %+v vs tip %+v", got, sums[0])
	}
}

// PoH rewards must credit accounts[wallet.address]; /api/wallet reads that row, not miner_address on the block argument.
func TestAppendPoHCreditsPrimaryWalletNotMinerArg(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "wallet_drift.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	oldAddr := "HMC-olddriftaa"
	newAddr := "HMC-newdriftbb"
	if _, _, err := svc.InitGenesis(ctx, oldAddr); err != nil {
		t.Fatal(err)
	}
	m0, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n, ev := firstPoHHit(m0)
	if _, err := svc.AppendPoHBlock(ctx, oldAddr, n, ev, 0.01, m0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE wallet SET address = ? WHERE id = 1`, newAddr); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE accounts SET address = ? WHERE address = ?`, newAddr, oldAddr); err != nil {
		t.Fatal(err)
	}
	var walBalBefore uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address = ?`, newAddr).Scan(&walBalBefore); err != nil {
		t.Fatal(err)
	}
	m1, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n2, ev2 := firstPoHHit(m1)
	if _, err := svc.AppendPoHBlock(ctx, oldAddr, n2, ev2, 0.01, m1, ""); err != nil {
		t.Fatal(err)
	}
	var staleBal uint64
	switch err := db.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address = ?`, oldAddr).Scan(&staleBal); {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		t.Fatal(err)
	default:
		if staleBal != 0 {
			t.Fatalf("miner argument account %s unexpectedly holds %d units after rewards redirected to wallet row", oldAddr, staleBal)
		}
	}
	var balNew uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address = ?`, newAddr).Scan(&balNew); err != nil {
		t.Fatal(err)
	}
	if balNew <= walBalBefore {
		t.Fatalf("primary wallet account did not grow: before=%d after=%d", walBalBefore, balNew)
	}
}

func TestBlocksAreSignedWhenSignerConfigured(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "chain_signed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	signer, err := nodecrypto.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewWithSigner(db, signer)
	addr := "HMC-signed"
	gen, _, err := svc.InitGenesis(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	if gen.MinerPubKey == "" || gen.MinerSig == "" {
		t.Fatalf("genesis signature missing: %+v", gen)
	}
	pub, err := hex.DecodeString(gen.MinerPubKey)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := hex.DecodeString(gen.MinerSig)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(gen.Hash), sig) {
		t.Fatal("genesis signature verify failed")
	}
	m0, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	nonce, eval := firstPoHHit(m0)
	b, err := svc.AppendPoHBlock(ctx, addr, nonce, eval, 0.01, m0, "")
	if err != nil {
		t.Fatal(err)
	}
	if b.MinerPubKey == "" || b.MinerSig == "" {
		t.Fatalf("poh signature missing: %+v", b)
	}
	sig2, err := hex.DecodeString(b.MinerSig)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(b.Hash), sig2) {
		t.Fatal("poh signature verify failed")
	}
}

func TestEconomicsPrefersUnitsMetaOverLegacyFloat(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "econ_units_pref.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := New(db)
	addr := "HMC-econ-pref"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	// Intentionally diverge legacy float keys; Economics must use *_units as source of truth.
	if _, err := db.ExecContext(ctx, `UPDATE meta SET value='9999.0' WHERE key=?`, metaTotalMintedHMC); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE meta SET value='0.0' WHERE key=?`, metaTotalBurnedHMC); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE meta SET value=? WHERE key=?`, "250000000", metaTotalMintedUnits); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE meta SET value=? WHERE key=?`, "50000000", metaTotalBurnedUnits); err != nil {
		t.Fatal(err)
	}
	ec, err := svc.Economics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// 250,000,000 units = 2.5 HMC ; 50,000,000 units = 0.5 HMC
	if ec.TotalMinted < 2.499999 || ec.TotalMinted > 2.500001 {
		t.Fatalf("minted from units expected 2.5, got %v", ec.TotalMinted)
	}
	if ec.TotalBurned < 0.499999 || ec.TotalBurned > 0.500001 {
		t.Fatalf("burned from units expected 0.5, got %v", ec.TotalBurned)
	}
}

func TestEconomicsCirculatingPlusMintRemainingEqualsMaxMinusBurned(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "econ_sum_identity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := New(db)
	addr := "HMC-econ-sum-id"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	ec, err := svc.Economics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sum := ec.Circulating + ec.MintRemaining
	want := ec.MaxSupplyHMC - ec.TotalBurned
	const eps = 1e-6
	if sum < want-eps || sum > want+eps {
		t.Fatalf("circulating(%v)+mint_remaining(%v)=%v want max-burned=%v (minted=%v burned=%v)",
			ec.Circulating, ec.MintRemaining, sum, want, ec.TotalMinted, ec.TotalBurned)
	}
	if ec.Circulating < 0 || ec.MintRemaining < 0 {
		t.Fatalf("negative field: %+v", ec)
	}
}

func TestEconomicInvariantUsesUnitsTotals(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "econ_inv_units.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := New(db)
	addr := "HMC-econ-inv-units"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	// Make legacy floats look valid, but units invalid (burned > minted).
	if _, err := db.ExecContext(ctx, `UPDATE meta SET value='10.0' WHERE key=?`, metaTotalMintedHMC); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE meta SET value='1.0' WHERE key=?`, metaTotalBurnedHMC); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE meta SET value=? WHERE key=?`, "1000", metaTotalMintedUnits); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE meta SET value=? WHERE key=?`, "2000", metaTotalBurnedUnits); err != nil {
		t.Fatal(err)
	}
	if err := svc.checkEconomicInvariants(ctx, db); err == nil {
		t.Fatal("expected economic invariant violation from units meta")
	}
}
