// Coordinator is a lightweight LAN pool process: POST /api/push_work + GET /api/network/stats
// (same JSON as the HackMe command node). No chain / mining — aggregation only.
// Default: 127.0.0.1:8081, DB data/coordinator.db. Optional HACKME_COORDINATOR_ADMIN_TOKEN for POST.
package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"hackme/internal/lanpool"
	"hackme/internal/logsetup"
	"hackme/internal/poolfuzz"
	"hackme/internal/store"
)

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func envDurationSec(key string, defSec int) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return time.Duration(defSec) * time.Second
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return time.Duration(defSec) * time.Second
	}
	return time.Duration(n) * time.Second
}

const maxCoordinatorPushWorkBodyBytes = 1 << 20

func main() {
	logsetup.ConfigureFromEnv("HACKME_COORDINATOR")
	dbPath := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_DB"))
	if dbPath == "" {
		dbPath = "data/coordinator.db"
	}
	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("sqlite: %v", err)
	}
	defer db.Close()
	absDBPath := store.AbsDBPath(dbPath)

	fuzzDBPath := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_FUZZ_DB"))
	fuzzDB := db
	fuzzDBPathLog := dbPath
	if fuzzDBPath != "" {
		var ferr error
		fuzzDB, ferr = store.OpenFuzz(fuzzDBPath)
		if ferr != nil {
			log.Fatalf("sqlite fuzz: %v", ferr)
		}
		defer fuzzDB.Close()
		fuzzDBPathLog = fuzzDBPath
	}
	absFuzzDBPath := store.AbsDBPath(fuzzDBPathLog)

	startCoordWALMaint := func(label, absPath string, handle *sql.DB) {
		if err := store.SetWALAutocheckpoint(handle, 500); err != nil {
			log.Printf("sqlite %s wal_autocheckpoint: %v", label, err)
		}
		store.StartWALMaintenanceWithConfig(context.Background(), absPath, handle, store.WALMaintenanceConfig{
			Interval:       2 * time.Minute,
			ThresholdBytes: store.CoordinatorWALCheckpointBytes,
			PassiveFirst:   true,
		})
		if wal := store.WALSizeBytes(absPath); wal >= store.CoordinatorWALCheckpointBytes/2 {
			cctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			_ = store.CheckpointPassive(cctx, handle)
			if store.WALSizeBytes(absPath) >= store.CoordinatorWALCheckpointBytes/2 {
				if err := store.CheckpointTruncate(cctx, handle); err != nil {
					log.Printf("sqlite %s startup wal_checkpoint: %v (wal=%d bytes)", label, err, wal)
				} else {
					log.Printf("sqlite %s startup wal_checkpoint: wal %d -> %d bytes", label, wal, store.WALSizeBytes(absPath))
				}
			} else {
				log.Printf("sqlite %s startup wal_checkpoint(PASSIVE): wal %d -> %d bytes", label, wal, store.WALSizeBytes(absPath))
			}
			cancel()
		}
	}
	// More frequent SQLite auto-checkpoints under pool write load (~500 pages ≈ 2MiB).
	startCoordWALMaint("db", absDBPath, db)
	if fuzzDB != db {
		startCoordWALMaint("fuzz_db", absFuzzDBPath, fuzzDB)
	}

	reg := lanpool.NewRegistry()
	if err := loadLANPeers(db, reg); err != nil {
		log.Printf("lan_peer_rigs load: %v", err)
	}

	addr := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_ADDR"))
	if addr == "" {
		addr = "127.0.0.1:8081"
	}
	token := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_ADMIN_TOKEN"))
	workerToken := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_WORKER_TOKEN"))
	allowInsecure := envBool("HACKME_COORDINATOR_ALLOW_INSECURE", false)
	requireToken := envBool("HACKME_COORDINATOR_REQUIRE_ADMIN_TOKEN", true)
	if requireToken && token == "" && !allowInsecure {
		log.Fatal("security: HACKME_COORDINATOR_ADMIN_TOKEN is required (or set HACKME_COORDINATOR_ALLOW_INSECURE=1 for loopback-only dev)")
	}
	if allowInsecure && !coordinatorBindLoopbackOnly(addr) {
		log.Fatal("security: HACKME_COORDINATOR_ALLOW_INSECURE=1 is only allowed on loopback bind (got " + addr + ")")
	}
	if token == "" && !coordinatorBindLoopbackOnly(addr) && !allowInsecure {
		log.Fatal("security: coordinator bind " + addr + " is not loopback-only — set HACKME_COORDINATOR_ADMIN_TOKEN")
	}
	initClientIPTrust(addr)

	mux := http.NewServeMux()
	wm := newWorkManagerFromEnv()
	wm.initPersistentPermaban()
	mux.HandleFunc("/api/network/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		resp := lanpool.RealNetworkStats(reg, "", lanpool.LocalMining{})
		if !coordAdminOK(r, token) || strings.TrimSpace(r.URL.Query().Get("details")) != "1" {
			resp = lanpool.RedactPublicNetworkStats(resp)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/push_work", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if token == "" && allowInsecure {
			// loopback-only dev
		} else if token == "" || !coordAdminOK(r, token) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
			return
		}
		var body lanpool.PushWorkBody
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorPushWorkBodyBytes)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if body.IP == "" {
			body.IP = clientIPKey(r)
		}
		if err := reg.Upsert(r.RemoteAddr, body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		id := strings.TrimSpace(body.WorkerID)
		persistPeer(r.Context(), db, id, reg)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "worker_id": id})
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "coordinator"})
	})
	addWorkRoutes(mux, token, workerToken, allowInsecure, reg, wm, db)
	pf := &poolfuzz.Service{DB: fuzzDB}
	pf.Settler = &poolfuzz.RelaySettler{
		Service:          pf,
		DefaultOrdersURL: wm.ordersProbeURL,
		AdminToken:       wm.ordersAdminToken,
	}
	addFuzzPoolRoutes(mux, token, workerToken, allowInsecure, wm, pf)
	startPoolFuzzTicker(context.Background(), pf)

	log.Printf("HackMe LAN coordinator → http://%s  (db=%s fuzz_db=%s)", addr, dbPath, fuzzDBPathLog)
	if trustClientForwardedFor {
		log.Printf("client IP trust: X-Real-IP / X-Forwarded-For enabled; CF-Connecting-IP only from Cloudflare peers (bind %s)", addr)
	}
	if token != "" {
		log.Printf("HACKME_COORDINATOR_ADMIN_TOKEN is set: admin routes + GET /api/work/stats?details=1")
	}
	if workerToken != "" {
		log.Printf("HACKME_COORDINATOR_WORKER_TOKEN is set: remote miners may claim/submit with worker token (not clear-abuse or stats details)")
	}
	log.Printf("Pool fuzz: POST /api/fuzz/work/claim|submit  POST /api/fuzz/pool/campaigns (admin)  GET /api/fuzz/pool/stats")
	if token == "" && workerToken == "" && allowInsecure {
		log.Printf("security warning: HACKME_COORDINATOR_ALLOW_INSECURE=1 — claim/submit/push_work allowed without token on %s (dev only)", addr)
	}
	log.Printf("Work coordinator: batch=%d target_mod=%d lease=%ds reward_per_m=%.6f found_bonus=%.6f",
		wm.defaultBatch, wm.targetMod, wm.leaseSec, wm.rewardPerM, wm.foundBonus)
	log.Printf("Work anti-abuse: claim_per_min=%d submit_per_min=%d bad_strikes_to_ban=%d ban_sec=%d",
		wm.claimPerMin, wm.submitPerMin, wm.badStrikesToBan, wm.banSec)
	readTO := envDurationSec("HACKME_COORDINATOR_READ_TIMEOUT_SEC", 60)
	writeTO := envDurationSec("HACKME_COORDINATOR_WRITE_TIMEOUT_SEC", 120)
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       readTO,
		WriteTimeout:      writeTO,
		IdleTimeout:       120 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}

func coordinatorBindLoopbackOnly(addr string) bool {
	host := strings.TrimSpace(addr)
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = strings.TrimSpace(h)
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	// Empty host (e.g. ":8081") means all interfaces — NOT loopback-only.
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func coordAdminOK(r *http.Request, expected string) bool {
	if expected == "" {
		return false
	}
	if len(extractCoordAdminSecret(r)) != len(expected) {
		return false
	}
	got := extractCoordAdminSecret(r)
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

func extractCoordAdminSecret(r *http.Request) string {
	if s := strings.TrimSpace(r.Header.Get("X-Hackme-Admin-Token")); s != "" {
		return s
	}
	const p = "Bearer "
	a := r.Header.Get("Authorization")
	if len(a) > len(p) && strings.EqualFold(a[:len(p)], p) {
		return strings.TrimSpace(a[len(p):])
	}
	return ""
}

func loadLANPeers(db *sql.DB, reg *lanpool.Registry) error {
	ctx := context.Background()
	rws, err := store.LoadLANPeerRigs(ctx, db)
	if err != nil {
		return err
	}
	for _, row := range rws {
		reg.SeedFromDBRow(row.WorkerID, row.Name, row.HashrateGHS, row.LastSeenUnix, row.IP, row.SharesAccepted)
	}
	return nil
}

func persistPeer(ctx context.Context, db *sql.DB, workerID string, reg *lanpool.Registry) {
	name, gh, unix, ip, shares, ok := reg.RowForPersist(workerID)
	if !ok {
		return
	}
	_ = store.UpsertLANPeerRig(ctx, db, store.LANPeerRigRow{
		WorkerID:       strings.TrimSpace(workerID),
		Name:           name,
		HashrateGHS:    gh,
		LastSeenUnix:   unix,
		IP:             ip,
		SharesAccepted: shares,
	})
}
