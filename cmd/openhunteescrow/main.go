// Open missing Hunt 50/50 escrow or finalize an open one (ops heal).
//
//	go run ./cmd/openhunteescrow -campaign hunt-… -budget 150 -shards 12000
//	go run ./cmd/openhunteescrow -campaign hunt-… -finalize -confirm-campaign hunt-… -db …
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hackme/internal/chain"

	_ "modernc.org/sqlite"
)

func main() {
	camp := flag.String("campaign", "", "campaign id")
	budget := flag.Float64("budget", 150, "budget HMC (open only)")
	shards := flag.Int("shards", 12000, "budget shards/runs (open only)")
	finalize := flag.Bool("finalize", false, "FinalizeFuzzEscrow (refund unused + close)")
	confirm := flag.String("confirm-campaign", "", "must equal -campaign when using -finalize (anti-footgun)")
	dbPath := flag.String("db", "", "hackme.db path")
	flag.Parse()
	if *camp == "" {
		fmt.Fprintln(os.Stderr, "need -campaign")
		os.Exit(2)
	}
	root := os.Getenv("HACKME_REPO_ROOT")
	if root == "" {
		root = "/opt/hackme"
	}
	dbFile := *dbPath
	if dbFile == "" {
		dbFile = filepath.Join(root, "data", "hackme.db")
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)", dbFile)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sql open: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	svc := chain.New(db)
	if *finalize {
		if err := authorizeFinalize(*camp, *confirm, os.Getenv("HACKME_ESCROW_FINALIZE_OK")); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(2)
		}
		row, err := svc.FinalizeFuzzEscrow(ctx, *camp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FinalizeFuzzEscrow: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("ok finalize campaign=%s status=%s runs_paid=%.4f refunded_runs=%.4f refunded_bounty=%.4f\n",
			row.CampaignID, row.Status, row.RunsPaidHMC, row.RefundedRunsHMC, row.RefundedBountyHMC)
		return
	}
	row, err := svc.OpenHuntEscrow(ctx, *camp, *budget, *shards)
	if err != nil {
		fmt.Fprintf(os.Stderr, "OpenHuntEscrow: %v\n", err)
		os.Exit(1)
	}
	per := 0.0
	if *shards > 0 {
		per = row.RunsPoolHMC / float64(*shards)
	}
	fmt.Printf("ok campaign=%s status=%s budget_hmc=%.4f runs_pool=%.4f bounty_pool=%.4f per_run≈%.6f\n",
		row.CampaignID, row.Status, row.BudgetHMC, row.RunsPoolHMC, row.BountyPoolHMC, per)
}

// authorizeFinalize requires an explicit second copy of the campaign id so a stray
// -finalize on a live DB cannot refund escrow by accident (audit H3). Either
// -confirm-campaign or HACKME_ESCROW_FINALIZE_OK must equal the campaign id.
func authorizeFinalize(campaign, confirmFlag, confirmEnv string) error {
	camp := strings.TrimSpace(campaign)
	if camp == "" {
		return fmt.Errorf("refuse finalize: empty campaign")
	}
	okFlag := strings.TrimSpace(confirmFlag) == camp
	okEnv := strings.TrimSpace(confirmEnv) == camp
	if okFlag || okEnv {
		return nil
	}
	return fmt.Errorf("refuse finalize: pass -confirm-campaign=%s (or HACKME_ESCROW_FINALIZE_OK with the same id)", camp)
}
