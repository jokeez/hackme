// HMS lane coordinator — deploy on Heavy VPS #2 (not hub).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"hackme/internal/hms"
	"hackme/internal/logsetup"
)

func main() {
	logsetup.ConfigureFromEnv("HMS_COORDINATOR")
	dbPath := strings.TrimSpace(os.Getenv("HMS_COORDINATOR_DB"))
	if dbPath == "" {
		dbPath = "data/hms_coordinator.db"
	}
	db, err := hms.OpenDB(dbPath)
	if err != nil {
		log.Fatalf("hms db: %v", err)
	}
	defer db.Close()

	cfg := hms.ConfigFromEnv()
	coord := hms.NewCoordinator(db, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go coord.RunEpochLoop(ctx)
	go coord.RunHealthLoop(ctx)
	if os.Getenv("HMS_STRATUM_ENABLE") == "1" {
		go hms.RunStratumBridge(coord, os.Getenv("HMS_STRATUM_ADDR"))
	}

	addr := strings.TrimSpace(os.Getenv("HMS_COORDINATOR_ADDR"))
	if addr == "" {
		addr = "127.0.0.1:18082"
	}
	admin := strings.TrimSpace(os.Getenv("HMS_COORDINATOR_ADMIN_TOKEN"))
	worker := strings.TrimSpace(os.Getenv("HMS_COORDINATOR_WORKER_TOKEN"))
	mux := http.NewServeMux()
	hms.RegisterHTTP(mux, coord, admin, worker)

	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		log.Printf("hmscoordinator listening on %s pool=%s epoch=%s", addr, cfg.PoolID, cfg.EpochDuration)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	shCtx, shCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shCancel()
	_ = srv.Shutdown(shCtx)
}
