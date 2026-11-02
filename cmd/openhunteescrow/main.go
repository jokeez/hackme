// Open missing Hunt 50/50 escrow for a running campaign (ops heal).
//   go run ./cmd/openhunteescrow -campaign hunt-… -budget 150 -shards 12000
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"hackme/internal/chain"

	_ "modernc.org/sqlite"
)

func main() {
	camp := flag.String("campaign", "", "campaign id")
	budget := flag.Float64("budget", 150, "budget HMC")
	shards := flag.Int("shards", 12000, "budget shards/runs")
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
