package main

import (
	"os"
	"path/filepath"
	"testing"
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
		"w1": {SettledHMC: 5.0, LastTxHash: "old"},
	}}
	remote := workerSettlementState{Workers: map[string]workerSettlementStateEntry{
		"w1": {SettledHMC: 2.0, LastTxHash: "new"},
	}}
	if mergeCanonicalSettlementState(&local, remote) {
		t.Fatal("expected no change when remote settled is lower")
	}
	if local.Workers["w1"].SettledHMC != 5.0 {
		t.Fatalf("settled regressed to %v", local.Workers["w1"].SettledHMC)
	}
}

func TestCanonicalSettlementStateFileFromDataDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKME_DATA_DIR", dir)
	t.Setenv("HACKME_SETTLEMENT_CANONICAL_FILE", "")
	t.Setenv("SETTLEMENT_CANONICAL_JSON", "")
	path := filepath.Join(dir, "settlement_canonical_public.json")
	if err := os.WriteFile(path, []byte(`{"workers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := canonicalSettlementStateFile()
	if got != path {
		t.Fatalf("file=%q want %q", got, path)
	}
}
