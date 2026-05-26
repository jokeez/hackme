// live_fuzz_seed submits a known detector violation into a running fuzz campaign DB.
// Used for demos when DeriveInput mutations rarely hit scripted guards.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/poolfuzz"
	"hackme/internal/store"
)

func main() {
	dbPath := flag.String("db", filepath.Join("data", "hackme.db"), "hackme sqlite path")
	campaignID := flag.String("campaign", "", "fuzz campaign id (required)")
	inputHex := flag.String("input", "0x2094c", "violation input (0x4c|521<<8)")
	flag.Parse()
	if *campaignID == "" {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/live_fuzz_seed -campaign <id> [-db data/hackme.db] [-input 0x2094c]")
		os.Exit(2)
	}
	in, err := parseU64(*inputHex)
	if err != nil {
		fmt.Fprintln(os.Stderr, "input:", err)
		os.Exit(2)
	}
	db, err := store.Open(*dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "db:", err)
		os.Exit(1)
	}
	defer db.Close()
	svc := &poolfuzz.Service{DB: db}
	ctx := context.Background()
	now := time.Now().Unix()
	if err := svc.EnsureWorkItems(ctx, *campaignID, now); err != nil {
		fmt.Fprintln(os.Stderr, "ensure work:", err)
		os.Exit(1)
	}
	w, ok, err := svc.Claim(ctx, "live-fuzz-seed", now)
	if err != nil {
		fmt.Fprintln(os.Stderr, "claim:", err)
		os.Exit(1)
	}
	if !ok {
		fmt.Fprintln(os.Stderr, "no work to claim")
		os.Exit(1)
	}
	wasmHex := w.WasmCheckHex
	if wasmHex == "" {
		fmt.Fprintln(os.Stderr, "campaign has no wasm_check_hex")
		os.Exit(1)
	}
	cr, _, trap, err := poolfuzz.ExecuteLocally(ctx, wasmHex, in, 800)
	if err != nil {
		fmt.Fprintln(os.Stderr, "execute:", err)
		os.Exit(1)
	}
	if err := svc.Submit(ctx, poolfuzz.SubmitRequest{
		WorkerID:    "live-fuzz-seed",
		WorkID:      w.WorkID,
		CampaignID:  w.CampaignID,
		ItemID:      w.ItemID,
		InputN:      w.InputN,
		ActualInput: in,
		CheckResult: cr,
		DurationMS:  1,
		Trap:        trap,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "submit:", err)
		os.Exit(1)
	}
	ft, sev, _ := fuzzengine.ClassifyCheckFail(in, true, fuzzengine.SemanticsDetector)
	fmt.Printf("seeded finding campaign=%s input=0x%x check_ret=%d type=%s severity=%s trap=%q\n",
		*campaignID, in, cr, ft, sev, trap)
}

func parseU64(s string) (uint64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if strings.HasPrefix(s, "0x") {
		return strconv.ParseUint(s[2:], 16, 64)
	}
	return strconv.ParseUint(s, 10, 64)
}
