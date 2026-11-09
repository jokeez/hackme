package chain

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"hackme/internal/store"
)

func newSUPTestChain(t *testing.T, activation string) (*Service, context.Context, string) {
	t.Helper()
	setV2HeightForTest(t, activation)
	t.Setenv("HACKME_SUP_MINT_ENABLED", "1")
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "chain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	addr := "HMC-testnode"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	if err := svc.InitSUPGenesis(ctx); err != nil {
		t.Fatal(err)
	}
	return svc, ctx, addr
}

// Economics v2 has one SUP emission path: blocks. Admin/settlement mint works before the
// activation height and is refused from it on, so pool accrual cannot mint SUP beside blocks.
func TestSUPAdminMintRetiredAtV2Activation(t *testing.T) {
	svc, ctx, addr := newSUPTestChain(t, "2")
	if code, err := svc.MintSUP(ctx, "HMC-aaaaaaaaaaaaaaaa", 1000, "pre-v2"); err != nil {
		t.Fatalf("mint before activation: %s %v", code, err)
	}
	ec, err := svc.SUPEconomics(ctx)
	if err != nil || ec.AdminMintRetired || ec.RewardPerBlockSUP != 0 {
		t.Fatalf("pre-v2 economics: %+v err=%v", ec, err)
	}
	// Block 1 is still v1; next block (2) is v2 ⇒ mint retired.
	m, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	nonce, eval := firstPoHHit(m)
	if _, err := svc.AppendPoHBlock(ctx, addr, nonce, eval, BaseRewardForBlockIndex(1), m, ""); err != nil {
		t.Fatal(err)
	}
	code, err := svc.MintSUP(ctx, "HMC-aaaaaaaaaaaaaaaa", 1000, "post-v2")
	if code != "sup_mint_retired_v2" || !errors.Is(err, ErrSUPMintRetired) {
		t.Fatalf("mint after activation: code=%q err=%v", code, err)
	}
	ec, err = svc.SUPEconomics(ctx)
	if err != nil || !ec.AdminMintRetired || ec.MintEnabled || ec.MaxSupplySUP != V2SUPMaxSupply || ec.RewardPerBlockSUP != V2SUPInitialReward {
		t.Fatalf("v2 economics: %+v err=%v", ec, err)
	}
}

func TestSUPAdminMintUnaffectedWithoutActivation(t *testing.T) {
	svc, ctx, _ := newSUPTestChain(t, "")
	if code, err := svc.MintSUP(ctx, "HMC-aaaaaaaaaaaaaaaa", 1000, "v1"); err != nil {
		t.Fatalf("v1 mint: %s %v", code, err)
	}
	ec, _ := svc.SUPEconomics(ctx)
	if ec.AdminMintRetired || ec.RewardPerBlockSUP != 0 {
		t.Fatalf("v1 economics: %+v", ec)
	}
}
