// Command workersettle: settle coordinator PoH accrual to on-chain HMC transfers
// using the SQLite units ledger (replaces scripts/ops/settle_worker_payouts.sh).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"hackme/internal/workersettle"
)

func main() {
	asset := flag.String("asset", "hmc", "hmc (coordinator HMC accrual) or sup (Economics v2 SUP by transfer)")
	flag.Parse()
	cfg := workersettle.DefaultConfig()
	if root := findRoot(); root != "" {
		if cfg.PublishScript == "" {
			p := filepath.Join(root, "scripts/ops/publish_settlement_state.sh")
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				cfg.PublishScript = p
			}
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	run := workersettle.Run
	switch *asset {
	case "hmc":
	case "sup":
		run = workersettle.RunSUP
	default:
		fmt.Fprintf(os.Stderr, "[settle-workers] unknown -asset %q\n", *asset)
		os.Exit(2)
	}
	res, err := run(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[settle-workers] FATAL: %v\n", err)
		os.Exit(1)
	}
	if res.SkippedLock {
		os.Exit(0)
	}
}

func findRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	dir := wd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return wd
}
