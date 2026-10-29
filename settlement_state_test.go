package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadCanonicalSettlementStateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settlement_canonical_public.json")
	raw := `{"workers":{"w1":{"settled_hmc":1.5,"payout_address":"HMC-91fe007e4036c602"}}}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := readCanonicalSettlementStateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Workers["w1"].SettledHMC != 1.5 {
		t.Fatalf("settled=%v", st.Workers["w1"].SettledHMC)
	}
}

func TestMergeCanonicalSettlementState(t *testing.T) {
	local := workerSettlementState{Workers: map[string]workerSettlementStateEntry{
		"w1": {SettledHMC: 1.0},
	}}
	remote := workerSettlementState{Workers: map[string]workerSettlementStateEntry{
		"w1": {SettledHMC: 2.0, LastTxHash: "abc", LastSettleUnix: 100},
	}}
	if !mergeCanonicalSettlementState(&local, remote) {
		t.Fatal("expected change when remote settled advanced")
	}
	if local.Workers["w1"].SettledHMC != 2.0 {
		t.Fatalf("settled=%v", local.Workers["w1"].SettledHMC)
	}
	if local.Workers["w1"].LastTxHash != "abc" {
		t.Fatalf("tx=%q", local.Workers["w1"].LastTxHash)
	}
}

func TestMergeCanonicalSettlementStateNoRegression(t *testing.T) {
	local := workerSettlementState{Workers: map[string]workerSettlementStateEntry{
		"w1": {SettledHMC: 5.0, LastTxHash: "old", LastSettleUnix: 200},
	}}
	remote := workerSettlementState{Workers: map[string]workerSettlementStateEntry{
		"w1": {SettledHMC: 2.0, LastTxHash: "new", LastSettleUnix: 100},
	}}
	if mergeCanonicalSettlementState(&local, remote) {
		t.Fatal("expected no change when remote settled is lower and older")
	}
	if local.Workers["w1"].SettledHMC != 5.0 {
		t.Fatalf("settled regressed to %v", local.Workers["w1"].SettledHMC)
	}
}

func TestMergeCanonicalSettlementStateRejectsUnsignedRaise(t *testing.T) {
	local := workerSettlementState{Workers: map[string]workerSettlementStateEntry{
		"w1": {SettledHMC: 1.0, LastSettleUnix: 50},
	}}
	remote := workerSettlementState{Workers: map[string]workerSettlementStateEntry{
		"w1": {SettledHMC: 99.0, LastTxHash: "poison", LastSettleUnix: 50},
	}}
	if mergeCanonicalSettlementState(&local, remote) {
		t.Fatal("must not raise SettledHMC without fresher LastSettleUnix")
	}
	if local.Workers["w1"].SettledHMC != 1.0 {
		t.Fatalf("settled=%v", local.Workers["w1"].SettledHMC)
	}
}

func TestMergeCanonicalSettlementStateRejectsZeroTimestampRaise(t *testing.T) {
	local := workerSettlementState{Workers: map[string]workerSettlementStateEntry{
		"w1": {SettledHMC: 1.0, LastSettleUnix: 10},
	}}
	remote := workerSettlementState{Workers: map[string]workerSettlementStateEntry{
		"w1": {SettledHMC: 9.0, LastSettleUnix: 0},
	}}
	if mergeCanonicalSettlementState(&local, remote) {
		t.Fatal("must not raise SettledHMC from untimestamped remote")
	}
}

func TestMergeCanonicalSettlementStateFresherRemoteMayLower(t *testing.T) {
	local := workerSettlementState{Workers: map[string]workerSettlementStateEntry{
		"w1": {SettledHMC: 9.2, LastTxHash: "poison", LastSettleUnix: 100},
	}}
	remote := workerSettlementState{Workers: map[string]workerSettlementStateEntry{
		"w1": {SettledHMC: 8.8, LastTxHash: "vps", LastSettleUnix: 200, PayoutAddress: "HMC-91fe007e4036c602"},
	}}
	if !mergeCanonicalSettlementState(&local, remote) {
		t.Fatal("expected change when remote settle is fresher")
	}
	if local.Workers["w1"].SettledHMC != 8.8 {
		t.Fatalf("settled=%v want 8.8", local.Workers["w1"].SettledHMC)
	}
	if local.Workers["w1"].LastTxHash != "vps" {
		t.Fatalf("tx=%q", local.Workers["w1"].LastTxHash)
	}
}

func TestSettlementEntryForWorkerResolvesPublicPseudonym(t *testing.T) {
	rawID := "worker-kapa-pc"
	pseudo := publicWorkerPseudoKey(rawID)
	if pseudo == "" {
		t.Fatal("expected publicWorkerPseudoKey")
	}
	state := map[string]workerSettlementStateEntry{
		rawID: {SettledSUP: 1576.02, SettledHMC: 1250.87, LastSettleUnix: 100},
	}
	ent := settlementEntryForWorker(state, pseudo)
	if ent.SettledSUP != 1576.02 {
		t.Fatalf("settled_sup=%v via pseudo", ent.SettledSUP)
	}
	ent2 := settlementEntryForWorker(state, rawID)
	if ent2.SettledHMC != 1250.87 {
		t.Fatalf("settled_hmc=%v via raw", ent2.SettledHMC)
	}
	if settlementEntryForWorker(state, "missing").SettledSUP != 0 {
		t.Fatal("expected empty for unknown id")
	}
}

func TestFetchCanonicalSettlementStateIgnoresPoisonedDiskOnHTTPFail(t *testing.T) {
	dir := t.TempDir()
	poison := filepath.Join(dir, "settlement_canonical_public.json")
	raw := `{"workers":{"worker-kapa-pc":{"settled_hmc":99.0,"last_settle_unix":1,"payout_address":"HMC-91fe007e4036c602"}}}`
	if err := os.WriteFile(poison, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_SETTLEMENT_CANONICAL_FILE", poison)
	t.Setenv("HACKME_SETTLEMENT_CANONICAL_URL", "http://127.0.0.1:1/api/settlement/canonical.json")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := fetchCanonicalSettlementState(ctx)
	if err == nil {
		t.Fatal("expected HTTP failure without disk fallback")
	}
}

func TestCanonicalSettlementStateFileFromDataDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKME_DATA_DIR", dir)
	t.Setenv("HACKME_SETTLEMENT_CANONICAL_FILE", "")
	t.Setenv("SETTLEMENT_CANONICAL_JSON", "")
	t.Setenv("HACKME_WORKER_SETTLEMENT_STATE_FILE", filepath.Join(dir, "worker_settlement_state.json"))
	t.Setenv("WORKER_SETTLEMENT_STATE_FILE", "")
	path := filepath.Join(dir, "settlement_canonical_public.json")
	if err := os.WriteFile(path, []byte(`{"workers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := canonicalSettlementStateFile()
	if got != path {
		t.Fatalf("file=%q want %q", got, path)
	}
}
