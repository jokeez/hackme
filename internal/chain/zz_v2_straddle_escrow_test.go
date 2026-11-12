package chain

import (
	"database/sql"
	"strconv"
	"testing"

	"hackme/internal/fuzzescrow"
)

// An order opened before Economics v2 and settled after it must keep the terms it
// was opened with: no retroactive 5% burn / 5% platform fee, the same per-run pay,
// and a finalize refund of exactly what is left — and every unit stays accounted for.
// A post-fork order on the same node pays from its 90% pool. (Fork-week audit, gap A.)
func TestV2StraddlingEscrowKeepsPreForkTerms(t *testing.T) {
	const act = 5
	n := newV2Net(t, "5")
	guardOn(t)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_CONFIRM", "5")
	t.Setenv("HACKME_ECONOMICS_V2_MIN_LEAD_BLOCKS", "3")
	fundAddr(t, n.hubDB, n.signer.Address(), HMCToUnits(5))
	if _, err := n.hubDB.Exec(`UPDATE wallet SET balance_units=balance_units+?, balance_hmc=balance_hmc+? WHERE id=1`, HMCToUnits(300), 300.0); err != nil {
		t.Fatal(err)
	}
	if _, err := n.hubDB.Exec(`UPDATE accounts SET balance_units=balance_units+? WHERE address=?`, HMCToUnits(300), n.wallet); err != nil {
		t.Fatal(err)
	}
	if _, err := n.hubDB.Exec(`INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=CAST(value AS INTEGER)+?`, metaTotalMintedUnits, HMCToUnits(305), HMCToUnits(305)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < act-2; i++ {
		n.mine()
	}
	pre, err := n.hub.OpenFuzzEscrowWithFee(n.ctx, "straddle-pre", 100, 100, fuzzescrow.EscrowSplit2080, "HMC")
	if err != nil {
		t.Fatal(err)
	}
	burnedAtOpen := metaUnits(t, n.hubDB, metaTotalBurnedUnits)
	for i := 0; i < 4; i++ { // block 4 (last v1) + 5..7 (v2)
		n.mine()
	}
	if BaseRewardForBlockIndex(7) != 0.1 {
		t.Fatal("lab chain did not cross activation")
	}

	const miner = "HMC-00000000000000a1"
	perRun := HMCToUnits(pre.RunsPoolHMC) / uint64(pre.BudgetRuns)
	for i := 0; i < 3; i++ {
		if _, err := n.hub.PayFuzzRun(n.ctx, "straddle-pre", miner); err != nil {
			t.Fatalf("post-fork pay of pre-fork order: %v", err)
		}
	}
	mh, _ := unitsOf(t, n.hubDB, miner)
	if mh != 3*perRun {
		t.Fatalf("miner got %d units for 3 runs, want %d (pre-fork per-run rate)", mh, 3*perRun)
	}
	if b := metaUnits(t, n.hubDB, metaTotalBurnedUnits); b != burnedAtOpen {
		t.Fatalf("settling a pre-fork order burned %d units after the fork", b-burnedAtOpen)
	}

	walletBefore := walletUnits(t, n.hubDB)
	row, err := n.hub.FinalizeFuzzEscrow(n.ctx, "straddle-pre")
	if err != nil {
		t.Fatalf("post-fork finalize of pre-fork order: %v", err)
	}
	refund := walletUnits(t, n.hubDB) - walletBefore
	if want := HMCToUnits(100) - 3*perRun; refund != want {
		t.Fatalf("refund %d units, want %d (budget − paid runs, no fee)", refund, want)
	}
	if row.Status == "open" {
		t.Fatalf("escrow still open after finalize: %+v", row)
	}
	if b := metaUnits(t, n.hubDB, metaTotalBurnedUnits); b != burnedAtOpen {
		t.Fatalf("finalize burned %d units of a pre-fork order", b-burnedAtOpen)
	}
	// A second finalize must not refund twice.
	before2 := walletUnits(t, n.hubDB)
	_, _ = n.hub.FinalizeFuzzEscrow(n.ctx, "straddle-pre")
	if walletUnits(t, n.hubDB) != before2 {
		t.Fatal("double finalize refunded twice")
	}

	// Post-fork order on the same node: 90% pool, per-run from that pool.
	post, err := n.hub.OpenFuzzEscrowWithFee(n.ctx, "straddle-post", 100, 100, fuzzescrow.EscrowSplit2080, "HMC")
	if err != nil {
		t.Fatal(err)
	}
	if HMCToUnits(post.RunsPoolHMC+post.BountyPoolHMC) != HMCToUnits(90) {
		t.Fatalf("post-fork pools %v+%v, want 90", post.RunsPoolHMC, post.BountyPoolHMC)
	}
	if b := metaUnits(t, n.hubDB, metaTotalBurnedUnits); b-burnedAtOpen != HMCToUnits(5) {
		t.Fatalf("post-fork open burned %d units, want 5 HMC", b-burnedAtOpen)
	}
	if _, err := n.hub.PayFuzzRun(n.ctx, "straddle-post", miner); err != nil {
		t.Fatal(err)
	}
	mh2, _ := unitsOf(t, n.hubDB, miner)
	if got, want := mh2-mh, HMCToUnits(post.RunsPoolHMC)/uint64(post.BudgetRuns); got != want {
		t.Fatalf("post-fork run paid %d, want %d", got, want)
	}
}

func metaUnits(t *testing.T, db *sql.DB, key string) uint64 {
	t.Helper()
	var v string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&v); err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}
	n, _ := strconv.ParseUint(v, 10, 64)
	return n
}

func walletUnits(t *testing.T, db *sql.DB) uint64 {
	t.Helper()
	var u uint64
	if err := db.QueryRow(`SELECT balance_units FROM wallet WHERE id=1`).Scan(&u); err != nil {
		t.Fatal(err)
	}
	return u
}
