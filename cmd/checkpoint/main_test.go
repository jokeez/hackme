package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hackme/internal/chain"
	"hackme/internal/netutil"
	"hackme/internal/nodecrypto"
	"hackme/internal/store"
)

func TestCreateVerifyEndToEnd(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()
	dataDir := filepath.Join(work, "data")
	dbPath := filepath.Join(dataDir, "hackme.db")
	cpDir := filepath.Join(work, "repo", "checkpoints")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	signer, err := nodecrypto.LoadOrCreate(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	svc := chain.New(db)
	addr := signer.Address()
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	create := func() error {
		return runCreate([]string{"-db", dbPath, "-data-dir", dataDir, "-dir", cpDir})
	}
	if err := create(); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if err := create(); err == nil || !strings.Contains(err.Error(), "has not advanced") {
		t.Fatalf("create on a stalled tip must refuse, got %v", err)
	}
	// Append one block, then checkpoint again.
	m, _ := svc.PoHTargetMod(ctx)
	_, tip, _ := svc.Tip(ctx)
	for n := uint64(0); ; n++ {
		if v := chain.PohWorkForIndex(1, n, tip, addr); v%m == 0 {
			if _, err := svc.AppendPoHBlock(ctx, addr, n, v, 0.01, m, ""); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if err := create(); err != nil {
		t.Fatalf("second create: %v", err)
	}
	if err := runVerify([]string{"-dir", cpDir, "-signer", addr}); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := runVerify([]string{"-dir", cpDir, "-signer", "HMC-0000000000000000"}); err == nil {
		t.Fatal("wrong expected signer must fail")
	}

	// Public API double: serve the real hashes, then a rewritten block 1.
	hashes := map[uint64]string{}
	for _, h := range []uint64{0, 1} {
		row, ok, err := svc.GetBlockSummaryByIndex(ctx, h)
		if err != nil || !ok {
			t.Fatalf("block %d: ok=%v err=%v", h, ok, err)
		}
		hashes[h] = row.Hash
	}
	rewritten := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var h uint64
		fmt.Sscan(r.URL.Query().Get("q"), &h)
		hash := hashes[h]
		if rewritten && h == 1 {
			hash = strings.Repeat("e", 64)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"lookup": true, "found": true,
			"blocks": []map[string]any{{"index": h, "hash": hash, "miner_address": addr}},
		})
	}))
	defer srv.Close()
	if err := runVerify([]string{"-dir", cpDir, "-api", srv.URL}); err != nil {
		t.Fatalf("api verify: %v", err)
	}
	rewritten = true
	if err := runVerify([]string{"-dir", cpDir, "-api", srv.URL}); err == nil || !strings.Contains(err.Error(), "HISTORY CHANGED") {
		t.Fatalf("rewritten block must be reported, got %v", err)
	}

	// Tampering with a published file breaks verification and blocks further creates.
	files, _ := filepath.Glob(filepath.Join(cpDir, "*.json"))
	if len(files) != 2 {
		t.Fatalf("want 2 checkpoint files, got %v", files)
	}
	raw, _ := os.ReadFile(files[0])
	if err := os.WriteFile(files[0], []byte(strings.Replace(string(raw), `"accounts": `, `"accounts": 9`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runVerify([]string{"-dir", cpDir}); err == nil {
		t.Fatal("tampered file must fail verify")
	}
	if err := create(); err == nil {
		t.Fatal("create must refuse to extend a chain that does not verify")
	}
	_ = db.Close()
}

// CKPT-REDIR-01: API hash compare must not follow 3xx to a forged block payload.
func TestFetchBlockRefusesRedirect(t *testing.T) {
	var sawSecond bool
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawSecond = true
		_ = json.NewEncoder(w).Encode(map[string]any{
			"lookup": true, "found": true,
			"blocks": []map[string]any{{"index": uint64(1), "hash": strings.Repeat("a", 64)}},
		})
	}))
	defer collector.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, collector.URL+"/reports/blocks?q=1", http.StatusFound)
	}))
	defer redirector.Close()

	_, err := fetchBlock(context.Background(), netutil.HTTPClientNoRedirect(5*time.Second), redirector.URL, 1)
	if err == nil || !strings.Contains(err.Error(), "refusing HTTP redirect") {
		t.Fatalf("expected redirect refusal, got %v", err)
	}
	if sawSecond {
		t.Fatal("verify client must not follow redirect")
	}
}
