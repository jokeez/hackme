package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"hackme/internal/chain"
	"hackme/internal/poolsync"
	"hackme/internal/store"
)

func TestFuzzSettleOutboxAction(t *testing.T) {
	cases := []struct {
		status    string
		kind      string
		wantApply bool
		wantDrain bool
	}{
		{"open", "run", true, false},
		{"open", "finding", true, false},
		{"open", "crash_bonus", true, false},
		{"closed", "run", false, false}, // must not ACK unpaid runs after premature finalize
		{"closed", "finalize", false, true},
		{"bounty_paid", "run", true, false},
		{"bounty_paid", "finding", false, true},
		{"bounty_paid", "finalize", true, false},
		{"unknown", "run", false, false},
	}
	for _, tc := range cases {
		apply, drain := fuzzSettleOutboxAction(tc.status, tc.kind)
		if apply != tc.wantApply || drain != tc.wantDrain {
			t.Errorf("fuzzSettleOutboxAction(%q,%q) = (%v,%v) want (%v,%v)",
				tc.status, tc.kind, apply, drain, tc.wantApply, tc.wantDrain)
		}
	}
}

func TestApplyLocalFuzzSettleOnceNoDoublePay(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "pull.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := chain.New(db)
	payer := "HMC-1111111111111111"
	miner := "HMC-2222222222222222"
	if _, _, err := svc.InitGenesis(ctx, payer); err != nil {
		t.Fatal(err)
	}
	preFundMainEscrow(t, ctx, db, payer, 50)
	if _, err := svc.OpenFuzzEscrow(ctx, "pull-camp", 10.0, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO accounts(address, balance_units, next_nonce, updated_at) VALUES(?,0,0,0)`, miner); err != nil {
		t.Fatal(err)
	}
	a := &app{chain: svc}
	it := poolsync.SettleOutboxItem{
		ID: 7, CampaignID: "pull-camp", Kind: "run", MinerAddress: miner,
	}
	if err := a.applyLocalFuzzSettleOnce(ctx, it); err != nil {
		t.Fatal(err)
	}
	row, err := svc.GetFuzzEscrow(ctx, "pull-camp")
	if err != nil {
		t.Fatal(err)
	}
	paidOnce := row.RunsDone
	if paidOnce != 1 {
		t.Fatalf("first apply runs_done=%d want 1", paidOnce)
	}
	// Simulate lost ACK / re-pull of the same outbox event.
	if err := a.applyLocalFuzzSettleOnce(ctx, it); err != nil {
		t.Fatal(err)
	}
	row, err = svc.GetFuzzEscrow(ctx, "pull-camp")
	if err != nil {
		t.Fatal(err)
	}
	if row.RunsDone != 1 {
		t.Fatalf("re-pull must not double-pay: runs_done=%d", row.RunsDone)
	}
	ok, err := svc.HasFuzzSettleApplied(ctx, chain.FuzzSettleEventID("pull-camp", 7))
	if err != nil || !ok {
		t.Fatalf("applied event missing: ok=%v err=%v", ok, err)
	}
	wantID := "outbox:pull-camp:7"
	if got := chain.FuzzSettleEventID("pull-camp", 7); got != wantID {
		t.Fatalf("event_id format=%q want %q", got, wantID)
	}
}

func preFundMainEscrow(t *testing.T, ctx context.Context, db *sql.DB, addr string, hmc float64) {
	t.Helper()
	units := chain.HMCToUnits(hmc)
	if _, err := db.ExecContext(ctx, `UPDATE wallet SET balance_hmc = ?, balance_units = ? WHERE id = 1`, hmc, units); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE accounts SET balance_units = ? WHERE address = ?`, units, addr); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO meta(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = CASE
		   WHEN CAST(value AS REAL) < CAST(excluded.value AS REAL) THEN excluded.value
		   ELSE value END`,
		"econ_total_minted_hmc", hmc); err != nil {
		t.Fatal(err)
	}
}
