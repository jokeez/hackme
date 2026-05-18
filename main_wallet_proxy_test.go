package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/chain"
	"hackme/internal/nodecrypto"
	"hackme/internal/store"
)

func newWalletTestApp(t *testing.T) (*app, *sql.DB) {
	t.Helper()
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "hackme.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	signer, err := nodecrypto.LoadOrCreate(dataDir)
	if err != nil {
		t.Fatalf("load signer: %v", err)
	}
	ch := chain.NewWithSigner(db, signer)
	if _, _, err := ch.InitGenesis(context.Background(), signer.Address()); err != nil {
		t.Fatalf("init genesis: %v", err)
	}
	return &app{
		chain: ch,
		miner: chain.NewMiner(0.01, nil, nil, chain.InternalTaskProvider{}),
		db:    db,
	}, db
}

func TestHandleWalletUsesCanonicalPeerSnapshot(t *testing.T) {
	a, _ := newWalletTestApp(t)
	addr, _, err := a.chain.Wallet(context.Background())
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	const remoteUnits = uint64(1_234_567_890)
	const remoteNonce = uint64(42)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/address/" + addr:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"address":       addr,
				"balance_units": remoteUnits,
				"next_nonce":    remoteNonce,
			})
		case "/api/wallet":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"address":     addr,
				"balance_hmc": 12.3456789,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer peer.Close()
	t.Setenv("HACKME_CANONICAL_CHAIN_URL", peer.URL)

	req := httptest.NewRequest(http.MethodGet, "/api/wallet", nil)
	rec := httptest.NewRecorder()
	a.handleWallet(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode wallet: %v", err)
	}
	if got, _ := out["wallet_source"].(string); got != "canonical_peer" {
		t.Fatalf("wallet_source=%q", got)
	}
	if got := uint64(out["balance_units"].(float64)); got != remoteUnits {
		t.Fatalf("balance_units=%d want %d", got, remoteUnits)
	}
	if got := uint64(out["next_nonce"].(float64)); got != remoteNonce {
		t.Fatalf("next_nonce=%d want %d", got, remoteNonce)
	}
}

func TestHandleWalletLocalSourceWithoutCanonical(t *testing.T) {
	a, _ := newWalletTestApp(t)
	// Ensure canonical overlays are fully disabled for this local-source assertion.
	t.Setenv("HACKME_CANONICAL_CHAIN_URL", "")
	t.Setenv("HACKME_P2P_PEERS", "")
	t.Setenv("HACKME_POOL_COORDINATOR_URL", "")
	t.Setenv("HACKME_PUBLIC_AUTHORITY_BASE", "")
	t.Setenv("HACKME_DESKTOP_MODE", "")
	req := httptest.NewRequest(http.MethodGet, "/api/wallet", nil)
	rec := httptest.NewRecorder()
	a.handleWallet(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode wallet: %v", err)
	}
	if got, _ := out["wallet_source"].(string); got != "local_db" {
		t.Fatalf("wallet_source=%q", got)
	}
}

func TestHandleWalletEarningsUsesLocalLedgerWhileMiningWhenNetworked(t *testing.T) {
	a, _ := newWalletTestApp(t)
	addr, _, err := a.chain.Wallet(context.Background())
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}

	const remoteNet24 = 99.991234
	var peerQueryAddress string
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/wallet/earnings" {
			http.NotFound(w, r)
			return
		}
		peerQueryAddress = r.URL.Query().Get("address")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"source": "local_db",
			"data": map[string]any{
				"address":             addr,
				"window_hours":        24,
				"bucket_sec":          3600,
				"net_24h_hmc":         remoteNet24,
				"total_net_hmc":       remoteNet24,
				"received_24h_hmc":    0.0,
				"sent_24h_hmc":        0.0,
				"tx_count_24h":        float64(0),
				"tx_count_window":     float64(0),
				"buckets":             []any{},
				"settled_out_24h_hmc": 0.0,
			},
		})
	}))
	defer peer.Close()

	t.Setenv("HACKME_CANONICAL_CHAIN_URL", peer.URL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	miner := &chain.Miner{}
	a.miner = miner
	miner.Start(ctx)
	defer miner.Stop()
	if !miner.Running() {
		t.Fatal("expected miner running for test premise")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/wallet/earnings?window_hours=24&bucket_sec=3600", nil)
	rec := httptest.NewRecorder()
	a.handleWalletEarnings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode earnings: %v", err)
	}
	if got, _ := out["source"].(string); got != "local_db" {
		t.Fatalf("source=%q want local_db (leader must not self-proxy earnings while mining)", got)
	}
	if peerQueryAddress != "" {
		t.Fatalf("canonical earnings should not be fetched while miner running, got address query %q", peerQueryAddress)
	}
}

func TestHandleWalletEarningsProxiesCommandLedgerPayload(t *testing.T) {
	a, _ := newWalletTestApp(t)
	addr, _, err := a.chain.Wallet(context.Background())
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	const sent24 = 0.000011
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/wallet/earnings" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"source": "local_db",
			"data": map[string]any{
				"address":         addr,
				"window_hours":    24,
				"bucket_sec":      3600,
				"net_24h_hmc":     0.014,
				"sent_24h_hmc":    sent24,
				"tx_count_window": float64(19),
				"tx_count_24h":    float64(18),
				"buckets":         []any{},
			},
			"canonical_earnings_unavailable": true,
		})
	}))
	defer peer.Close()
	t.Setenv("HACKME_CANONICAL_CHAIN_URL", peer.URL)

	req := httptest.NewRequest(http.MethodGet, "/api/wallet/earnings?window_hours=24&bucket_sec=3600", nil)
	rec := httptest.NewRecorder()
	a.handleWalletEarnings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, _ := out["source"].(string); got != "canonical_peer" {
		t.Fatalf("source=%q want canonical_peer", got)
	}
	if _, has := out["canonical_earnings_unavailable"]; has {
		t.Fatalf("canonical_earnings_unavailable should be stripped on proxy success")
	}
	data, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing")
	}
	if int(data["tx_count_window"].(float64)) != 19 {
		t.Fatalf("tx_count_window=%v", data["tx_count_window"])
	}
}

func TestHandleWalletEarningsCanonicalUnavailableSetsFlag(t *testing.T) {
	a, _ := newWalletTestApp(t)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer peer.Close()
	t.Setenv("HACKME_CANONICAL_CHAIN_URL", peer.URL)

	req := httptest.NewRequest(http.MethodGet, "/api/wallet/earnings?window_hours=24&bucket_sec=3600", nil)
	rec := httptest.NewRecorder()
	a.handleWalletEarnings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, _ := out["source"].(string); got != "local_db" {
		t.Fatalf("source=%q want local_db", got)
	}
	cu, ok := out["canonical_earnings_unavailable"].(bool)
	if !ok || !cu {
		t.Fatalf("canonical_earnings_unavailable=%v ok=%v", out["canonical_earnings_unavailable"], ok)
	}
	if _, has := out["fork_hint"]; has {
		t.Fatalf("fork_hint should be omitted when P2P is not wired (nil manager)")
	}
}

func TestHandleWalletEarningsLocalLedgerWhenCanonicalFailsWithTxHistory(t *testing.T) {
	a, db := newWalletTestApp(t)
	addr, _, err := a.chain.Wallet(context.Background())
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	now := time.Now().Unix()
	_, err = db.Exec(
		`INSERT INTO tx_history (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, applied_at)
		 VALUES (?, '{}', 'HMC-treasury', ?, 0, 0, 1000000, 'included', ?)`,
		"earn-test-1", addr, now-3600,
	)
	if err != nil {
		t.Fatalf("insert tx: %v", err)
	}
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer peer.Close()
	t.Setenv("HACKME_CANONICAL_CHAIN_URL", peer.URL)

	req := httptest.NewRequest(http.MethodGet, "/api/wallet/earnings?window_hours=24&bucket_sec=3600", nil)
	rec := httptest.NewRecorder()
	a.handleWalletEarnings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, _ := out["source"].(string); got != "canonical_ledger" {
		t.Fatalf("source=%q want canonical_ledger when local tx history exists", got)
	}
	if _, has := out["canonical_earnings_unavailable"]; has {
		t.Fatalf("canonical_earnings_unavailable should not be set when local ledger has txs")
	}
}

func TestHandleWalletDoesNotBlendForkLocalSQLiteOverCanonical(t *testing.T) {
	a, db := newWalletTestApp(t)
	t.Setenv("HACKME_DESKTOP_MODE", "1") // would enable blend by default
	addr, _, err := a.chain.Wallet(context.Background())
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	ctx := context.Background()
	const inflatedUnits = uint64(50_000 * 100_000_000) // 50k HMC in accounts — fork phantom scenario
	if err := store.UpsertAccount(ctx, db, store.AccountRow{Address: addr, BalanceUnits: inflatedUnits, NextNonce: 0}); err != nil {
		t.Fatalf("upsert account: %v", err)
	}
	const remoteUnits = uint64(100)
	const remoteNonce = uint64(1)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/address/" + addr:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"address":       addr,
				"balance_units": remoteUnits,
				"next_nonce":    remoteNonce,
			})
		case "/api/wallet":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"address":     addr,
				"balance_hmc": float64(remoteUnits) / 100_000_000.0,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer peer.Close()
	t.Setenv("HACKME_CANONICAL_CHAIN_URL", peer.URL)

	req := httptest.NewRequest(http.MethodGet, "/api/wallet", nil)
	rec := httptest.NewRecorder()
	a.handleWallet(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode wallet: %v", err)
	}
	if got, _ := out["balance_display_mode"].(string); got != "authoritative" {
		t.Fatalf("balance_display_mode=%q want authoritative", got)
	}
	disp := out["balance_display_hmc"].(float64)
	want := float64(remoteUnits) / 100_000_000.0
	if math.Abs(disp-want) > 1e-12 {
		t.Fatalf("balance_display_hmc=%v want %v (must not use inflated local SQLite)", disp, want)
	}
	if mirror := out["balance_local_mirror_hmc"].(float64); mirror <= want {
		t.Fatalf("balance_local_mirror_hmc=%v should exceed canonical for test premise", mirror)
	}
}

func TestHandleWalletStaleCanonicalCacheIgnored(t *testing.T) {
	a, _ := newWalletTestApp(t)
	addr, _, err := a.chain.Wallet(context.Background())
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	const staleUnits = uint64(1_449_069)
	const remoteUnits = uint64(1_789_448_069)
	a.cacheCanonicalWallet(addr, float64(staleUnits)/100_000_000.0, staleUnits, 1)
	a.canonMu.Lock()
	a.canonWalletCachedUnix = time.Now().Unix() - 120
	a.canonMu.Unlock()

	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/address/"+addr {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"address":       addr,
			"balance_units": remoteUnits,
			"next_nonce":    1,
		})
	}))
	defer peer.Close()
	t.Setenv("HACKME_CANONICAL_CHAIN_URL", peer.URL)
	t.Setenv("HACKME_P2P_PEERS", "http://127.0.0.1:1")

	req := httptest.NewRequest(http.MethodGet, "/api/wallet", nil)
	rec := httptest.NewRecorder()
	a.handleWallet(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, _ := out["wallet_source"].(string); got != "canonical_peer" {
		t.Fatalf("wallet_source=%q want canonical_peer (stale cache must not win)", got)
	}
	if got := uint64(out["balance_units"].(float64)); got != remoteUnits {
		t.Fatalf("balance_units=%d want %d", got, remoteUnits)
	}
}
