// workerfuzz — pool distributed fuzz worker (claims WASM check work from coordinator).
//
//	COORD_URL=https://hackme.tech/pool/coordinator COORD_TOKEN=... WORKER_ID=rig-fuzz-01 \
//	HACKME_MINER_ED25519_SEED_HEX=... go run ./cmd/workerfuzz
//
// Prefer HACKME_WORKER_HYBRID_FUZZ=1 on workerpoh for one worker_id that also digs fuzz.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"hackme/internal/workerfuzzloop"
)

func main() {
	coordURL := flag.String("coord", strings.TrimSpace(os.Getenv("COORD_URL")), "coordinator base URL")
	token := flag.String("token", strings.TrimSpace(os.Getenv("COORD_TOKEN")), "coordinator worker or admin token")
	workerID := flag.String("worker", strings.TrimSpace(os.Getenv("WORKER_ID")), "worker id")
	minerAddr := flag.String("miner", strings.TrimSpace(os.Getenv("MINER_ADDRESS")), "HMC payout address (optional with hybrid sig)")
	timeoutMS := flag.Int("timeout-ms", workerfuzzloop.EnvInt("WORKERFUZZ_TIMEOUT_MS", 500), "WASM check timeout ms")
	flag.Parse()
	if *coordURL == "" {
		*coordURL = "http://127.0.0.1:18081"
	}
	if *token == "" {
		if b, err := os.ReadFile(".secrets/hackme_coordinator_worker_token"); err == nil {
			*token = strings.TrimSpace(string(b))
		}
	}
	if *workerID == "" {
		*workerID = "workerfuzz-1"
	}
	priv, pubHex, derivedAddr, hybrid, err := workerfuzzloop.LoadHybridKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "workerfuzz: %v\n", err)
		os.Exit(1)
	}
	if hybrid {
		if *minerAddr == "" {
			*minerAddr = derivedAddr
		} else if !strings.EqualFold(strings.TrimSpace(*minerAddr), derivedAddr) {
			fmt.Fprintf(os.Stderr, "workerfuzz: MINER_ADDRESS=%s ignored; hybrid signer binds payout=%s\n",
				strings.TrimSpace(*minerAddr), derivedAddr)
			*minerAddr = derivedAddr
		}
		fmt.Fprintf(os.Stderr, "workerfuzz: hybrid signer payout=%s\n", derivedAddr)
	}
	base := strings.TrimRight(*coordURL, "/")
	cl := &http.Client{Timeout: workerfuzzloop.HTTPTimeoutFromEnv()}
	fmt.Fprintf(os.Stderr, "workerfuzz: coord=%s worker=%s\n", base, *workerID)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg := workerfuzzloop.Config{
		CoordURL:    base,
		Token:       *token,
		WorkerID:    *workerID,
		MinerAddr:   *minerAddr,
		TimeoutMS:   *timeoutMS,
		HTTPClient:  cl,
		Priv:        priv,
		PubHex:      pubHex,
		Hybrid:      hybrid,
		Concurrency: workerfuzzloop.EnvInt("HACKME_WORKER_HYBRID_FUZZ_CONCURRENCY", 1),
		MinClaimGap: workerfuzzloop.EnvDurationMS("HACKME_WORKER_HYBRID_FUZZ_CLAIM_GAP_MS", 50),
		LogPrefix:   "workerfuzz",
	}
	var st workerfuzzloop.Stats
	if err := workerfuzzloop.Run(ctx, cfg, &st); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "workerfuzz: %v\n", err)
		os.Exit(1)
	}
}
