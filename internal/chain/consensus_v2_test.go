package chain

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/block"
	"hackme/internal/nodecrypto"
	"hackme/internal/store"
)

type v2Net struct {
	t       *testing.T
	ctx     context.Context
	dir     string
	hub     *Service
	hubDB   *sql.DB
	signer  *nodecrypto.Signer
	wallet  string
	genesis string // VACUUM INTO snapshot right after genesis (followers start from it)
	n       int
}

const v2FollowerWallet = "HMC-follower000001"

// newV2Net starts a signing hub with Economics v2 from block `activation` and pins the hub key
// for followers via HACKME_CHAIN_AUTHORITY_PUBKEY.
func newV2Net(t *testing.T, activation string) *v2Net {
	t.Helper()
	ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(ResetEconomicsV2ActivationHeightForTest)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", activation)
	ResetEconomicsV2ActivationHeightForTest()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	signer, err := nodecrypto.LoadOrCreate(filepath.Join(dir, "hubkey"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_CHAIN_AUTHORITY_PUBKEY", signer.PublicKeyHex())
	hub := NewWithSigner(db, signer)
	wallet := "HMC-hubwallet00001"
	if _, _, err := hub.InitGenesis(ctx, wallet); err != nil {
		t.Fatal(err)
	}
	snap := filepath.Join(dir, "genesis.db")
	if _, err := db.Exec(`VACUUM INTO ?`, snap); err != nil {
		t.Fatal(err)
	}
	return &v2Net{t: t, ctx: ctx, dir: dir, hub: hub, hubDB: db, signer: signer, wallet: wallet, genesis: snap}
}

// follower opens a fresh copy of the genesis state whose local wallet is a different address.
func (n *v2Net) follower() (*Service, *sql.DB) {
	n.t.Helper()
	n.n++
	path := filepath.Join(n.dir, "follower"+strings.Repeat("x", n.n)+".db")
	src, err := sql.Open("sqlite", n.genesis)
	if err != nil {
		n.t.Fatal(err)
	}
	if _, err := src.Exec(`VACUUM INTO ?`, path); err != nil {
		n.t.Fatal(err)
	}
	_ = src.Close()
	db, err := store.Open(path)
	if err != nil {
		n.t.Fatal(err)
	}
	n.t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`UPDATE wallet SET address=? WHERE id=1`, v2FollowerWallet); err != nil {
		n.t.Fatal(err)
	}
	return New(db), db
}

func (n *v2Net) mine() *block.Block {
	n.t.Helper()
	m, err := n.hub.PoHTargetMod(n.ctx)
	if err != nil {
		n.t.Fatal(err)
	}
	h, _, _ := n.hub.Tip(n.ctx)
	nonce, eval := firstPoHHit(m)
	b, err := n.hub.AppendPoHBlock(n.ctx, "HMC-poolworker0001", nonce, eval, BaseRewardForBlockIndex(h+1), m, "")
	if err != nil {
		n.t.Fatal(err)
	}
	return b
}

func unitsOf(t *testing.T, db *sql.DB, addr string) (hmc, sup uint64) {
	t.Helper()
	err := db.QueryRow(`SELECT balance_units, COALESCE(balance_sup_units,0) FROM accounts WHERE address=?`, addr).Scan(&hmc, &sup)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	return
}

// The hub signs chain_id + reward_to into every v2 block; a follower with a different local
// wallet credits the header recipient, so both nodes hold the same balances.
func TestV2HubAndFollowerAgreeOnRewardRecipient(t *testing.T) {
	n := newV2Net(t, "1")
	b := n.mine()
	if b.ChainID != V2ChainID || b.RewardTo != n.wallet || b.MinerPubKey != n.signer.PublicKeyHex() || b.MinerSig == "" {
		t.Fatalf("hub v2 header: chain_id=%q reward_to=%q pub=%q", b.ChainID, b.RewardTo, b.MinerPubKey)
	}
	if pin, _ := n.hub.authorityPubKeyQ(n.ctx, n.hubDB); pin != n.signer.PublicKeyHex() {
		t.Fatalf("authority pin %q", pin)
	}
	f, fdb := n.follower()
	if err := f.ImportPoHBlock(n.ctx, b); err != nil {
		t.Fatalf("follower import: %v", err)
	}
	hubHMC, hubSUP := unitsOf(t, n.hubDB, n.wallet)
	gotHMC, gotSUP := unitsOf(t, fdb, n.wallet)
	if gotHMC != hubHMC || gotSUP != hubSUP || gotHMC == 0 || gotSUP == 0 {
		t.Fatalf("follower %d HMC/%d SUP for %s, hub %d/%d", gotHMC, gotSUP, n.wallet, hubHMC, hubSUP)
	}
	if h, s := unitsOf(t, fdb, v2FollowerWallet); h != 0 || s != 0 {
		t.Fatalf("follower's own wallet credited %d HMC / %d SUP", h, s)
	}
}

func TestV2ImportRejectsBadHeaders(t *testing.T) {
	n := newV2Net(t, "1")
	good := n.mine()
	other, err := nodecrypto.LoadOrCreate(filepath.Join(n.dir, "otherkey"))
	if err != nil {
		t.Fatal(err)
	}
	resign := func(b *block.Block, s *nodecrypto.Signer) {
		b.SetHash()
		b.MinerPubKey = s.PublicKeyHex()
		b.MinerSig = s.SignHex([]byte(b.Hash))
	}
	cases := []struct {
		name string
		mut  func(b *block.Block)
		want string
	}{
		{"unsigned", func(b *block.Block) { b.MinerPubKey, b.MinerSig = "", "" }, "authority signature required"},
		{"foreign key", func(b *block.Block) { resign(b, other) }, "authority is"},
		{"other chain_id", func(b *block.Block) { b.ChainID = "hackme-dev-mainnet"; resign(b, n.signer) }, "chain_id"},
		{"no reward_to", func(b *block.Block) { b.RewardTo = ""; resign(b, n.signer) }, "reward_to required"},
		{"reward_to swapped, old hash", func(b *block.Block) { b.RewardTo = "HMC-attacker000001" }, "hash mismatch"},
		{"reward_to swapped, rehash", func(b *block.Block) { b.RewardTo = "HMC-attacker000001"; b.SetHash() }, "signature verify failed"},
		{"reward_to swapped by foreign key", func(b *block.Block) { b.RewardTo = "HMC-attacker000001"; resign(b, other) }, "authority is"},
	}
	for _, c := range cases {
		b := *good
		c.mut(&b)
		f, _ := n.follower()
		err := f.ImportPoHBlock(n.ctx, &b)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: err=%v, want %q", c.name, err, c.want)
		}
	}
	// Control: the untouched block imports.
	f, _ := n.follower()
	if err := f.ImportPoHBlock(n.ctx, good); err != nil {
		t.Fatalf("control import: %v", err)
	}
}

// A follower that knows no authority key refuses v2 blocks instead of trusting the signer.
func TestV2FollowerWithoutAuthorityPinFailsClosed(t *testing.T) {
	n := newV2Net(t, "1")
	b := n.mine()
	t.Setenv("HACKME_CHAIN_AUTHORITY_PUBKEY", "")
	f, _ := n.follower()
	if err := f.ImportPoHBlock(n.ctx, b); !errors.Is(err, ErrAuthorityPubKeyCfg) {
		t.Fatalf("want ErrAuthorityPubKeyCfg, got %v", err)
	}
}

func TestV2AuthorityPinRejectsEnvChange(t *testing.T) {
	n := newV2Net(t, "1")
	n.mine()
	other, _ := nodecrypto.LoadOrCreate(filepath.Join(n.dir, "otherkey"))
	t.Setenv("HACKME_CHAIN_AUTHORITY_PUBKEY", other.PublicKeyHex())
	m, _ := n.hub.PoHTargetMod(n.ctx)
	h, _, _ := n.hub.Tip(n.ctx)
	nonce, eval := firstPoHHit(m)
	if _, err := n.hub.AppendPoHBlock(n.ctx, "HMC-poolworker0001", nonce, eval, BaseRewardForBlockIndex(h+1), m, ""); err == nil || !strings.Contains(err.Error(), "disagrees with pinned authority") {
		t.Fatalf("env change after pin: %v", err)
	}
}

// Before the height nothing changes: no v2 fields, no mandatory signature, legacy hashes.
func TestV2HeaderFieldsRefusedBeforeActivation(t *testing.T) {
	n := newV2Net(t, "50")
	b := n.mine()
	if b.ChainID != "" || b.RewardTo != "" {
		t.Fatalf("pre-v2 block carries v2 fields: %+v", b)
	}
	f, _ := n.follower()
	forged := *b
	forged.ChainID = V2ChainID
	forged.SetHash()
	forged.MinerSig = n.signer.SignHex([]byte(forged.Hash))
	if err := f.ImportPoHBlock(n.ctx, &forged); err == nil || !strings.Contains(err.Error(), "before activation") {
		t.Fatalf("pre-v2 block with chain_id: %v", err)
	}
	if err := f.ImportPoHBlock(n.ctx, b); err != nil {
		t.Fatalf("legacy import: %v", err)
	}
}

// Empty v2 fields must not change the legacy header hash (history keeps verifying).
func TestLegacyHeaderHashUnchanged(t *testing.T) {
	b := block.NewPoHBlock(7, block.ZeroPrevHash, "HMC-x", 1, 2, 3, "", "algebraic")
	before := b.HeaderHashHex()
	b.ChainID, b.RewardTo = "", ""
	if b.HeaderHashHex() != before {
		t.Fatal("hash changed with empty v2 fields")
	}
	b.RewardTo = "HMC-y"
	if b.HeaderHashHex() == before {
		t.Fatal("reward_to must be covered by the header hash")
	}
}
