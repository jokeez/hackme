// hunt-verifier — standalone Hunt ASAN replay consumer for coordinator fuzz DB.
//
//	HACKME_COORDINATOR_FUZZ_DB=/var/lib/hackme/fuzz.db \
//	HACKME_HUNT_VERIFIER_ID=verifier-01 \
//	go run ./cmd/hunt-verifier
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"hackme/internal/poolfuzz"
	"hackme/internal/store"
)

func main() {
	dbPath := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_FUZZ_DB"))
	if dbPath == "" {
		log.Fatal("HACKME_COORDINATOR_FUZZ_DB required")
	}
	db, err := store.OpenFuzz(dbPath)
	if err != nil {
		log.Fatalf("open fuzz db: %v", err)
	}
	defer db.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	svc := &poolfuzz.Service{DB: db}
	poolfuzz.StartHuntReplayWorkers(ctx, svc)
	log.Printf("hunt-verifier id=%s db=%s", envOr("HACKME_HUNT_VERIFIER_ID", "verifier"), dbPath)

	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("hunt-verifier shutdown")
			return
		case <-t.C:
			pending, processing, failed, err := svc.HuntReplayQueueStats(ctx)
			if err != nil {
				log.Printf("queue stats: %v", err)
				continue
			}
			if pending+processing+failed > 0 {
				log.Printf("hunt replay queue pending=%d processing=%d failed=%d", pending, processing, failed)
			}
		}
	}
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
