package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"hackme/internal/block"
	"hackme/internal/chain"
	"hackme/internal/integrator"
	"hackme/internal/lanpool"
	"hackme/internal/logsetup"
	"hackme/internal/nodecrypto"
	"hackme/internal/p2p"
	"hackme/internal/sandbox"
	"hackme/internal/store"
	"hackme/internal/workerid"
)

//go:embed dashboard.html
var embeddedDashboard string

//go:embed explorer.html
var embeddedExplorer string

//go:embed web/site/assets/logo-hex.png
var embeddedBrandLogoPNG []byte

//go:embed web/site/favicon.ico
var embeddedFaviconICO []byte

// Build metadata (overridden by -ldflags in release builds).
var (
	Version   = "0.1.0-rc11i"
	Commit    = "nogit"
	BuildDate = "unknown"
)

type pageData struct {
	NodeID              string
	ChainID             string
	BeginnerInlineToken string // legacy field; beginner solo env was removed
}

func computeNodeID(signer *nodecrypto.Signer) string {
	if signer != nil {
		return signer.Address()
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "unknown-host"
	}
	// Legacy fallback for defensive compatibility if signer is unavailable.
	// Keep format stable with previous host-derived node ids.
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", host, runtime.GOARCH, runtime.NumCPU())))
	return "HMC-" + hex.EncodeToString(sum[:])[:16]
}

func addressFromPubKeyHex(pubHex string) string {
	raw := strings.TrimSpace(pubHex)
	if raw == "" {
		return ""
	}
	pub, err := hex.DecodeString(raw)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return ""
	}
	sum := sha256.Sum256(pub)
	return "HMC-" + hex.EncodeToString(sum[:])[:16]
}

type app struct {
	tmpl         *template.Template
	explorerTmpl *template.Template
	chain        *chain.Service
	miner        *chain.Miner
	nodeID       string
	dataDir      string // absolute, for /api/wallet (where node_ed25519.seed lives)
	dbPath       string // absolute hackme.db path
	rigs         *lanpool.Registry
	db           *sql.DB
	signer       *nodecrypto.Signer
	p2p          *p2p.Manager
	p2pIngress   p2pIngressPolicy
	rlMu         sync.Mutex
	rlHits       map[string]rateSlot
	rlBan        map[string]int64
	p2pTokenFail map[string]int
	// Limits concurrent expensive P2P sync operations under storm load.
	p2pSyncHeavySem chan struct{}
	poolSyncOnce      sync.Once
	poolSyncCh        chan poolSyncJob
	poolSyncMu        sync.Mutex
	poolSyncFailed    map[string]string // campaign_id → last error
	poolSyncQueued    map[string]struct{}
	// Throttles stale rig pruning to keep network stats cheap.
	rigPruneLastUnix      int64
	workerMu              sync.Mutex
	workerCmd             *exec.Cmd
	workerLogPath         string
	workerStartedAt       int64
	workerCoordURL        string
	workerID              string
	workerBatchSize       uint64
	workerHashrate        float64
	canonMu               sync.RWMutex
	canonHasGenesis       bool
	canonTipHeight        uint64
	canonTipHash          string
	canonWalletAddr       string
	canonWalletHMC        float64
	canonWalletUnits      uint64
	canonWalletNonce      uint64
	canonWalletCachedUnix int64
	lastDesktopPruneUnix  int64
}

const (
	maxJSONBodyBytes         = 1 << 20 // 1 MiB
	rateSlotsMaxKeys         = 4096
	maxP2PHandshakeBodyBytes = 64 << 10
	maxP2PTxBodyBytes        = 256 << 10
)

func logAdminAction(r *http.Request, action string) {
	log.Printf("admin action=%s ip=%s ua=%q", action, clientIP(r), strings.TrimSpace(r.UserAgent()))
}

type rateSlot struct {
	sec   int64
	count int
}

type p2pIngressPolicy struct {
	allowCIDRs    []netip.Prefix
	denyCIDRs     []netip.Prefix
	maxPeersPer24 int
	tokenBanSec   int64
}

type orderVerificationPayload struct {
	Version       string `json:"version"`
	VerifiedBy    string `json:"verified_by"`
	PoolID        string `json:"pool_id"`
	OrderID       string `json:"order_id"`
	RequestSHA256 string `json:"request_sha256"`
	IssuedAtUnix  int64  `json:"issued_at_unix"`
	SignerPubKey  string `json:"signer_pubkey_ed25519"`
}

type orderVerificationReceipt struct {
	Version             string `json:"version"`
	VerifiedBy          string `json:"verified_by"`
	PoolID              string `json:"pool_id"`
	OrderID             string `json:"order_id"`
	RequestSHA256       string `json:"request_sha256"`
	IssuedAtUnix        int64  `json:"issued_at_unix"`
	SignerPubKeyEd25519 string `json:"signer_pubkey_ed25519"`
	SignatureEd25519    string `json:"signature_ed25519"`
}

func (a *app) buildOrderVerificationReceipt(orderID string, raw []byte) orderVerificationReceipt {
	hash := sha256.Sum256(raw)
	issuedAt := time.Now().Unix()
	poolID := strings.TrimSpace(os.Getenv("HACKME_POOL_ID"))
	if poolID == "" {
		poolID = a.nodeID
	}
	pub := ""
	sig := ""
	if a.signer != nil {
		pub = a.signer.PublicKeyHex()
		payload := orderVerificationPayload{
			Version:       "pool_receipt_v1",
			VerifiedBy:    "HackMe Pool",
			PoolID:        poolID,
			OrderID:       strings.TrimSpace(orderID),
			RequestSHA256: hex.EncodeToString(hash[:]),
			IssuedAtUnix:  issuedAt,
			SignerPubKey:  pub,
		}
		if b, err := json.Marshal(payload); err == nil {
			sig = a.signer.SignHex(b)
		}
	}
	return orderVerificationReceipt{
		Version:             "pool_receipt_v1",
		VerifiedBy:          "HackMe Pool",
		PoolID:              poolID,
		OrderID:             strings.TrimSpace(orderID),
		RequestSHA256:       hex.EncodeToString(hash[:]),
		IssuedAtUnix:        issuedAt,
		SignerPubKeyEd25519: pub,
		SignatureEd25519:    sig,
	}
}

func (a *app) cacheCanonicalTip(hasGenesis bool, height uint64, tipHash string) {
	if a == nil || strings.TrimSpace(tipHash) == "" {
		return
	}
	a.canonMu.Lock()
	a.canonHasGenesis = hasGenesis
	a.canonTipHeight = height
	a.canonTipHash = strings.TrimSpace(tipHash)
	a.canonMu.Unlock()
}

func (a *app) readCanonicalTipCache() (bool, uint64, string, bool) {
	if a == nil {
		return false, 0, "", false
	}
	a.canonMu.RLock()
	defer a.canonMu.RUnlock()
	if strings.TrimSpace(a.canonTipHash) == "" {
		return false, 0, "", false
	}
	return a.canonHasGenesis, a.canonTipHeight, a.canonTipHash, true
}

func canonicalWalletCacheTTL() int64 {
	sec := envInt("HACKME_WALLET_CANON_CACHE_SEC", 45)
	if sec <= 0 {
		return 45
	}
	return int64(sec)
}

func (a *app) cacheCanonicalWallet(addr string, hmc float64, units, nonce uint64) {
	if a == nil || strings.TrimSpace(addr) == "" {
		return
	}
	a.canonMu.Lock()
	a.canonWalletAddr = strings.TrimSpace(addr)
	a.canonWalletHMC = hmc
	a.canonWalletUnits = units
	a.canonWalletNonce = nonce
	a.canonWalletCachedUnix = time.Now().Unix()
	a.canonMu.Unlock()
}

func (a *app) readCanonicalWalletCache(addr string) (float64, uint64, uint64, bool) {
	if a == nil {
		return 0, 0, 0, false
	}
	want := strings.TrimSpace(addr)
	now := time.Now().Unix()
	ttl := canonicalWalletCacheTTL()
	a.canonMu.RLock()
	defer a.canonMu.RUnlock()
	if want == "" || !strings.EqualFold(want, strings.TrimSpace(a.canonWalletAddr)) {
		return 0, 0, 0, false
	}
	if a.canonWalletCachedUnix > 0 && now-a.canonWalletCachedUnix > ttl {
		return 0, 0, 0, false
	}
	return a.canonWalletHMC, a.canonWalletUnits, a.canonWalletNonce, true
}

func main() {
	ensureStableWorkingDir()
	loadHackmeDotEnv()
	applyRigProfileAtStartup()
	ensurePoolCoordinatorTokenEnv()
	applyFromCodeToolchainEnv()
	logsetup.ConfigureFromEnv("HACKME_NODE")
	applyPublicAuthorityBaseEnv()
	if os.Geteuid() == 0 {
		log.Printf("security warning: running as root may create root-owned data files (e.g. node_ed25519.seed) and break non-root transfer/signing scripts")
	}
	dataDir := filepath.Join(".", "data")
	if v := strings.TrimSpace(os.Getenv("HACKME_DATA_DIR")); v != "" {
		dataDir = v
	}
	if !filepath.IsAbs(dataDir) {
		if p, err := filepath.Abs(dataDir); err == nil {
			dataDir = p
		}
	}
	dbPath := filepath.Join(dataDir, "hackme.db")
	absDataDir := dataDir
	absDBPath := dbPath
	if p, err := filepath.Abs(dbPath); err == nil {
		absDBPath = p
	}
	if strings.TrimSpace(os.Getenv("HACKME_DATA_DIR")) != "" {
		log.Printf("using HACKME_DATA_DIR=%s (db %s)", absDataDir, absDBPath)
	}
	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("sqlite: %v", err)
	}
	defer db.Close()

	signer, err := nodecrypto.LoadOrCreate(dataDir)
	if err != nil {
		log.Fatalf("node signing key: %v", err)
	}
	nodeID := computeNodeID(signer)
	ch := chain.NewWithSigner(db, signer)
	if err := ch.RebindWalletToSigner(context.Background()); err != nil {
		log.Printf("wallet signer rebind skipped: %v", err)
	}
	if err := ch.MirrorWalletRowFromAccounts(context.Background()); err != nil {
		log.Printf("wallet mirror row: %v", err)
	}
	log.Printf("Task WASM artifact root: %s (env HACKME_TASK_ARTIFACT_DIR)", chain.DefaultArtifactRoot())
	tasksDir := filepath.Join(".", "tasks")
	taskProv := chain.NewStoreTaskProvider(ch, chain.NewFileTaskProvider(tasksDir, chain.InternalTaskProvider{}))
	var miner *chain.Miner
	miner = chain.NewMiner(chain.InitialBaseRewardHMC,
		func(ctx context.Context) (uint64, error) { return ch.PoHTargetMod(ctx) },
		func(ctx context.Context, nonce, v, targetMod uint64) error {
			ts := miner.TaskSnapshot()
			orderID := ""
			if ts.Source == chain.TaskSourceOrder {
				orderID = ts.ID
			}
			reward := miner.RewardForSolve()
			if orderID == "" {
				if baseReward, err := ch.BaseRewardForNextBlock(ctx); err == nil {
					reward = baseReward
				}
			}
			b, err := ch.AppendPoHBlock(ctx, nodeID, nonce, v, reward, targetMod, orderID)
			if err != nil {
				return err
			}
			// Log the task active at solve time (Snapshot is refreshed only after onSolve returns).
			log.Printf("POH block hash=%s index=%d nonce=%d target_mod=%d reward=%.4f task_id=%q source=%s",
				b.Hash, b.Index, nonce, targetMod, reward, ts.ID, ts.Source)
			raw, _ := json.MarshalIndent(b, "", "  ")
			log.Printf("POH block JSON:\n%s", string(raw))
			return nil
		},
		taskProv,
	)

	if st, err := integrator.Open(absDataDir); err != nil {
		log.Printf("integrator store init: %v", err)
	} else {
		integratorStore = st
		log.Printf("integrator store: %s (%d active)", filepath.Join(absDataDir, "integrator_tokens.json"), st.ActiveCount())
	}

	a := &app{
		tmpl:            template.Must(template.New("dash").Parse(embeddedDashboard)),
		explorerTmpl:    template.Must(template.New("explorer").Parse(embeddedExplorer)),
		chain:           ch,
		miner:           miner,
		nodeID:          nodeID,
		dataDir:         absDataDir,
		dbPath:          absDBPath,
		rigs:            lanpool.NewRegistry(),
		db:              db,
		signer:          signer,
		p2pIngress:      readP2PIngressPolicyFromEnv(),
		rlHits:          make(map[string]rateSlot),
		rlBan:           make(map[string]int64),
		p2pTokenFail:    make(map[string]int),
		p2pSyncHeavySem: make(chan struct{}, 3),
		p2p: p2p.NewManager(nodeID,
			strings.Split(strings.TrimSpace(os.Getenv("HACKME_P2P_PEERS")), ","),
			strings.TrimSpace(os.Getenv("HACKME_P2P_TOKEN")),
			envBool("HACKME_P2P_DISCOVERY", false),
			strings.TrimSpace(os.Getenv("HACKME_P2P_ADVERTISE_URL")),
			ch.PolicyHash(),
		),
	}
	if err := loadLANPeersIntoRegistry(db, a.rigs); err != nil {
		log.Printf("lan_peer_rigs load: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/assets/logo-hex.png", a.handleBrandLogo)
	mux.HandleFunc("/favicon.ico", a.handleFaviconICO)
	mux.HandleFunc("/api/metrics", a.handleMetrics)
	mux.HandleFunc("/api/global/metrics", a.handleGlobalMetrics)
	mux.HandleFunc("/api/network/stats", a.handleNetworkStats)
	mux.HandleFunc("/api/work/stats", a.handleWorkStats)
	mux.HandleFunc("/api/work/by-wallet", a.handleWorkByWallet)
	mux.HandleFunc("/api/push_work", a.handlePushWork)
	mux.HandleFunc("/api/genesis", a.handleGenesis)
	mux.HandleFunc("/api/wallet", a.handleWallet)
	mux.HandleFunc("/api/wallet/earnings", a.handleWalletEarnings)
	mux.HandleFunc("/api/chain", a.handleChain)
	mux.HandleFunc("/api/reports/blocks", a.handleReportsBlocks)
	mux.HandleFunc("/api/reports/block", a.handleReportsBlockLookup)
	mux.HandleFunc("/api/reports/hardware", a.handleReportsHardware)
	mux.HandleFunc("/api/status", a.handleStatus)
	mux.HandleFunc("/api/mining/start", a.handleMiningStart)
	mux.HandleFunc("/api/mining/stop", a.handleMiningStop)
	mux.HandleFunc("/api/worker/start", a.handleWorkerStart)
	mux.HandleFunc("/api/worker/stop", a.handleWorkerStop)
	mux.HandleFunc("/api/worker/status", a.handleWorkerStatus)
	mux.HandleFunc("/api/desktop/local-auth", handleDesktopLocalAuth)
	mux.HandleFunc("/api/worker/settlement", a.handleWorkerSettlement)
	mux.HandleFunc("/api/mining/logs", a.handleMiningLogs)
	mux.HandleFunc("/api/mining/logs/stream", a.handleMiningLogsStream)
	mux.HandleFunc("/api/hardware/tune", a.handleHardwareTune)
	mux.HandleFunc("/api/gpu/tune", a.handleHardwareTune) // alias
	mux.HandleFunc("/api/hardware/rig-profiles", a.handleRigProfiles)
	mux.HandleFunc("/api/hardware/rig-profiles/detect", a.handleRigProfilesDetect)
	mux.HandleFunc("/api/hardware/rig-profiles/apply", a.handleRigProfilesApply)
	mux.HandleFunc("/api/tasks", a.handleTasks)
	mux.HandleFunc("/api/poh/solve-order", a.handlePohSolveOrder)
	mux.HandleFunc("/api/tasks/from_code", a.handleTaskFromCode)
	mux.HandleFunc("/api/security-audit", a.handleSecurityAudit)
	mux.HandleFunc("/api/integrator/", a.handleIntegratorAPI)
	mux.HandleFunc("/api/integrator", a.handleIntegratorAPI)
	mux.HandleFunc("/api/fuzz/campaigns", a.handleFuzzCampaigns)
	mux.HandleFunc("/api/fuzz/campaigns/", a.handleFuzzCampaigns)
	mux.HandleFunc("/api/fuzz/pool/settle", a.handleFuzzPoolSettle)
	mux.HandleFunc("/api/fuzz/housekeeping", a.handleFuzzHousekeeping)
	mux.HandleFunc("/api/fuzz/artifacts/cleanup", a.handleFuzzArtifactsCleanup)
	mux.HandleFunc("/api/tx/send", a.handleTransferSend)
	mux.HandleFunc("/api/tx/pool", a.handleTransferPool)
	mux.HandleFunc("/api/tx/", a.handleTransferByHash)
	mux.HandleFunc("/api/address/", a.handleTransferAddressState)
	mux.HandleFunc("/api/p2p/handshake", a.handleP2PHandshake)
	mux.HandleFunc("/api/p2p/tx", a.handleP2PTx)
	mux.HandleFunc("/api/p2p/peers", a.handleP2PPeers)
	mux.HandleFunc("/api/p2p/sync", a.handleP2PSync)
	mux.HandleFunc("/api/p2p/sync/pull", a.handleP2PSyncPull)
	mux.HandleFunc("/api/p2p/sync/stage", a.handleP2PSyncStage)
	mux.HandleFunc("/api/p2p/sync/apply", a.handleP2PSyncApply)
	mux.HandleFunc("/api/p2p/sync/run", a.handleP2PSyncRun)
	mux.HandleFunc("/api/mining/devices", a.handleMiningDevices)
	mux.HandleFunc("/explorer", a.handleExplorer)
	mux.HandleFunc("/dashboard", a.handleDashboardRedirect)
	mux.HandleFunc("/", a.handleIndex)

	addr := "127.0.0.1:8080"
	if v := strings.TrimSpace(os.Getenv("HACKME_BIND_ADDR")); v != "" {
		addr = v
	}
	requireAdmin := true
	if v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_REQUIRE_ADMIN_TOKEN"))); v != "" {
		requireAdmin = v == "1" || v == "true" || v == "yes" || v == "on"
	}
	if requireAdmin && !adminAuthEnabled() {
		log.Fatal("security: HACKME_ADMIN_TOKEN is required (set HACKME_ADMIN_TOKEN or explicitly disable with HACKME_REQUIRE_ADMIN_TOKEN=0)")
	}
	uiPort := "8080"
	if _, p, err := net.SplitHostPort(addr); err == nil && p != "" {
		uiPort = p
	}
	log.Printf("HackMe [%s] listening %s (db %s); dashboard: http://127.0.0.1:%s/", block.ChainID, addr, dbPath, uiPort)
	if adminAuthEnabled() {
		log.Printf("HACKME_ADMIN_TOKEN is set: mutating POST routes require X-Hackme-Admin-Token or Authorization: Bearer (see docs/SECURITY.md)")
	}
	if envBool("HACKME_BEGINNER_SOLO", false) {
		log.Fatal("HACKME_BEGINNER_SOLO was removed: use pool/worker flow (HACKME_POOL_COORDINATOR_URL + POST /api/worker/start) or scripts/release/windows/start_hackme_dashboard.bat — see README worker-mode section")
	}
	if os.Getenv("HACKME_NETWORK_MOCK") == "1" || strings.EqualFold(os.Getenv("HACKME_NETWORK_MOCK"), "true") {
		log.Printf("HACKME_NETWORK_MOCK: /api/network/stats uses simulated global TH/miners (legacy demo)")
	} else {
		log.Printf("LAN pool: GET /api/network/stats aggregates real push_work + local PoH (persisted in lan_peer_rigs)")
	}
	if u := strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_URL")); u != "" {
		log.Printf("Pool coordinator proxy enabled: %s (token set=%v)", u, strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_TOKEN")) != "")
	}
	if a.p2p != nil && a.p2p.Enabled() {
		if a.p2p.DiscoveryEnabled() {
			log.Printf("P2P discovery enabled (HACKME_P2P_DISCOVERY=1), advertise=%q", strings.TrimSpace(os.Getenv("HACKME_P2P_ADVERTISE_URL")))
		}
		log.Printf("P2P peers configured via HACKME_P2P_PEERS")
		a.p2p.Start(context.Background(), func(ctx context.Context) (uint64, string, error) {
			h, tip, err := a.chain.Tip(ctx)
			return h, tip, err
		})
		a.startP2PBackgroundSync(context.Background())
	}
	if envBool("HACKME_AUTO_START_MINING", false) {
		if !envBool("HACKME_CHAIN_LEADER_LOCAL_POH", false) {
			log.Printf("HACKME_AUTO_START_MINING ignored: set HACKME_CHAIN_LEADER_LOCAL_POH=1 only on the chain command node that may run local WASM PoH")
		} else {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				has, err := a.chain.HasGenesis(ctx)
				if err != nil {
					log.Printf("auto mining start skipped: genesis check failed: %v", err)
					return
				}
				if !has {
					log.Printf("auto mining start skipped: genesis missing")
					return
				}
				if a.miner.Running() {
					return
				}
				a.refreshMinerDevicePolicy(ctx)
				a.miner.Start(context.Background())
				log.Printf("auto mining start enabled (HACKME_AUTO_START_MINING=1 + HACKME_CHAIN_LEADER_LOCAL_POH=1)")
			}()
		}
	}
	a.startFuzzAutoRunner(context.Background())
	a.startPoolWorkerWatchdog()
	srv := &http.Server{
		Addr:              addr,
		Handler:           hardenHTTPHandler(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func hardenHTTPHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Security baseline for both UI and API.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		corsOrigin := strings.TrimSpace(os.Getenv("HACKME_HTTP_CORS_ALLOW_ORIGIN"))
		apiPath := strings.HasPrefix(r.URL.Path, "/api/")
		if apiPath && corsOrigin != "" {
			// Allow browser dashboards on another origin to read public JSON API (set explicitly for open networks).
			w.Header().Set("Access-Control-Allow-Origin", corsOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Hackme-Admin-Token, X-Hackme-P2P-Token")
			w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
		} else {
			w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		}

		// Disallow TRACE to reduce accidental request-header reflection on proxies.
		if r.Method == http.MethodTrace {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if apiPath && r.Method == http.MethodOptions && corsOrigin != "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if apiPath {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func coordinatorURLLooksRemote(coordURL string) bool {
	u := strings.TrimSpace(strings.ToLower(coordURL))
	if u == "" {
		return false
	}
	return !strings.Contains(u, "127.0.0.1") && !strings.Contains(u, "localhost") && !strings.Contains(u, "::1")
}

// Worker submits use HACKME_WORKER_SIGN_SUBMITS in worker_loop.sh; public coordinators often require signatures (hybrid).
func workerSignSubmitsEffective(coordURL string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_WORKER_SIGN_SUBMITS")))
	switch v {
	case "0", "false", "no", "off":
		return false
	case "1", "true", "yes", "on":
		return true
	default:
		return coordinatorURLLooksRemote(coordURL)
	}
}

func validateMinerSeedHex(h string) error {
	h = strings.TrimSpace(strings.ToLower(h))
	if len(h) != 64 {
		return errors.New("miner seed hex must be 64 chars (32 bytes)")
	}
	if _, err := hex.DecodeString(h); err != nil {
		return errors.New("invalid miner seed hex")
	}
	return nil
}

// minerSubmitSeedHexForDataDir resolves Ed25519 seed hex for coordinator hybrid submits.
// When unified/desktop is on (default laptop staging): node_ed25519.seed wins first so hybrid payout address
// tracks wallet after seed rotation — stale HACKME_MINER_ED25519_SEED_HEX in the shell cannot override.
// Fleet workers should set HACKME_UNIFIED_MINER_NODE_SEED=0 (default follows DESKTOP_MODE off) and use HACKME_MINER_ED25519_SEED_HEX.
// Final fallback: miner_submit_ed25519_seed.hex → generate new file.
func minerSubmitSeedHexForDataDir(dataDir string) (string, error) {
	base := strings.TrimSpace(dataDir)
	if base == "" {
		base = "data"
	}
	unified := envBool("HACKME_UNIFIED_MINER_NODE_SEED", envBool("HACKME_DESKTOP_MODE", false))

	readNodeSeed := func() (string, bool) {
		nodePath := filepath.Join(base, "node_ed25519.seed")
		b, err := os.ReadFile(nodePath)
		if err != nil {
			return "", false
		}
		h := strings.TrimSpace(strings.ToLower(string(b)))
		if err := validateMinerSeedHex(h); err != nil {
			return "", false
		}
		return h, true
	}

	envMiner := strings.TrimSpace(os.Getenv("HACKME_MINER_ED25519_SEED_HEX"))
	if envMiner != "" {
		if err := validateMinerSeedHex(envMiner); err != nil {
			return "", err
		}
		envMiner = strings.TrimSpace(strings.ToLower(envMiner))
	}

	if unified {
		if h, ok := readNodeSeed(); ok {
			return h, nil
		}
		if envMiner != "" {
			return envMiner, nil
		}
	} else {
		if envMiner != "" {
			return envMiner, nil
		}
		if h, ok := readNodeSeed(); ok {
			return h, nil
		}
	}

	p := filepath.Join(base, "miner_submit_ed25519_seed.hex")
	if b, err := os.ReadFile(p); err == nil {
		h := strings.TrimSpace(string(b))
		if err := validateMinerSeedHex(h); err != nil {
			return "", fmt.Errorf("%s: %w", p, err)
		}
		return strings.ToLower(h), nil
	}
	if err := os.MkdirAll(base, 0o750); err != nil {
		return "", err
	}
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	h := hex.EncodeToString(buf[:])
	if err := os.WriteFile(p, []byte(h+"\n"), 0o600); err != nil {
		return "", err
	}
	log.Printf("miner submit signing: created %s (treat like wallet seed; backup offline)", p)
	return h, nil
}

func resolveMinersignBinPath() string {
	if v := strings.TrimSpace(os.Getenv("HACKME_MINERSIGN_BIN")); v != "" {
		if st, err := os.Stat(v); err == nil && !st.IsDir() {
			abs, err := filepath.Abs(v)
			if err != nil {
				return ""
			}
			return abs
		}
		return ""
	}
	candidates := []string{
		filepath.Join(".", "minersign"),
		filepath.Join(".", "bin", "minersign"),
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(dir, "minersign"))
		// Desktop script builds the node to <repo>/logs/desktop/hackme-node-desktop; minersign is often only in repo root.
		if filepath.Base(exe) == "hackme-node-desktop" {
			candidates = append(candidates, filepath.Clean(filepath.Join(dir, "..", "..", "minersign")))
		}
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			abs, err := filepath.Abs(p)
			if err != nil {
				continue
			}
			return abs
		}
	}
	return ""
}

// resolveWorkerRepoRoot finds the checkout root that contains scripts/ops/worker_loop.sh.
// dataDir's parent is wrong when the binary lives under logs/desktop/ and uses ./data next to it.
func resolveWorkerRepoRoot(dataDir string) string {
	candidates := make([]string, 0, 6)
	if d := strings.TrimSpace(dataDir); d != "" {
		candidates = append(candidates, filepath.Clean(filepath.Join(d, "..")))
	}
	if wd, err := os.Getwd(); err == nil {
		if abs, err := filepath.Abs(wd); err == nil {
			candidates = append(candidates, abs)
		}
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Clean(filepath.Join(dir, "..", "..")),
			filepath.Clean(filepath.Join(dir, "..")),
			dir,
		)
	}
	for _, root := range candidates {
		if root == "" || root == "." {
			continue
		}
		script := filepath.Join(root, "scripts", "ops", "worker_loop.sh")
		if st, err := os.Stat(script); err == nil && !st.IsDir() {
			if abs, err := filepath.Abs(root); err == nil {
				return abs
			}
			return root
		}
	}
	if d := strings.TrimSpace(dataDir); d != "" {
		return filepath.Clean(filepath.Join(d, ".."))
	}
	return "."
}

func effectiveHTTPBindAddr() string {
	addr := "127.0.0.1:8080"
	if v := strings.TrimSpace(os.Getenv("HACKME_BIND_ADDR")); v != "" {
		addr = v
	}
	return addr
}

func bindAddrAllowsBeginnerSolo(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	switch strings.TrimSpace(strings.ToLower(host)) {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func parseCIDRList(raw string) []netip.Prefix {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	out := make([]netip.Prefix, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s == "" {
			continue
		}
		pr, err := netip.ParsePrefix(s)
		if err != nil {
			continue
		}
		out = append(out, pr)
	}
	return out
}

func readP2PIngressPolicyFromEnv() p2pIngressPolicy {
	maxPer24 := envInt("HACKME_P2P_MAX_PEERS_PER_24", 16)
	if maxPer24 < 1 {
		maxPer24 = 1
	}
	if maxPer24 > 128 {
		maxPer24 = 128
	}
	tokenBanSec := int64(envInt("HACKME_P2P_TOKEN_FAIL_BAN_SEC", 300))
	if tokenBanSec < 30 {
		tokenBanSec = 30
	}
	if tokenBanSec > 3600 {
		tokenBanSec = 3600
	}
	return p2pIngressPolicy{
		allowCIDRs:    parseCIDRList(os.Getenv("HACKME_P2P_ALLOW_CIDRS")),
		denyCIDRs:     parseCIDRList(os.Getenv("HACKME_P2P_DENY_CIDRS")),
		maxPeersPer24: maxPer24,
		tokenBanSec:   tokenBanSec,
	}
}

func (a *app) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	chainID := a.chain.ChainID(ctx)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Prevent stale embedded dashboard UI after hotfix deploys.
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	data := pageData{
		NodeID:  a.nodeID,
		ChainID: chainID,
	}
	chainEsc := template.HTMLEscapeString(data.ChainID)
	nodeEsc := template.HTMLEscapeString(data.NodeID)
	dashRev := strings.TrimSpace(Commit)
	if dashRev == "" {
		dashRev = "nogit"
	}
	if bd := strings.TrimSpace(BuildDate); bd != "" {
		dashRev = dashRev + " · " + bd
	}
	dashRevEsc := template.HTMLEscapeString(dashRev)
	// dashboard.html currently contains only simple {{ .ChainID }} / {{ .NodeID }}
	// placeholders, so raw replacement avoids html/template JS context failures.
	out := strings.ReplaceAll(embeddedDashboard, "{{ .ChainID }}", chainEsc)
	out = strings.ReplaceAll(out, "{{ .NodeID }}", nodeEsc)
	out = strings.ReplaceAll(out, "{{ .DashboardRev }}", dashRevEsc)
	embedTok := ""
	if envBool("HACKME_DESKTOP_MODE", false) && requestFromLoopback(r) && adminAuthEnabled() {
		if t := adminTokenFromEnv(); t != "" {
			b, _ := json.Marshal(t)
			embedTok = `<script>window.__HACKME_EMBEDDED_ADMIN_TOKEN__=` + string(b) + `;</script>`
		}
	}
	out = strings.ReplaceAll(out, "{{ .DesktopAdminTokenScript }}", embedTok)
	_, _ = io.WriteString(w, out)
}

func (a *app) handleBrandLogo(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/assets/logo-hex.png" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(embeddedBrandLogoPNG)
}

func (a *app) handleFaviconICO(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/favicon.ico" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "image/x-icon")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(embeddedFaviconICO)
}

func (a *app) handleDashboardRedirect(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/dashboard" && r.URL.Path != "/dashboard/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *app) handleExplorer(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/explorer" {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	chainID := a.chain.ChainID(ctx)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := pageData{
		NodeID:  a.nodeID,
		ChainID: chainID,
	}
	if err := a.explorerTmpl.Execute(w, data); err != nil {
		log.Printf("explorer template: %v", err)
	}
}

func (a *app) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s := collector.snapshot()
	if h, _, err := a.chain.Tip(r.Context()); err == nil {
		s.BlockHeight = h
	}
	if ec, err := a.chain.Economics(r.Context()); err == nil {
		s.EconMaxSupplyHMC = ec.MaxSupplyHMC
		s.EconMintedHMC = ec.TotalMinted
		s.EconBurnedHMC = ec.TotalBurned
		s.EconCirculating = ec.Circulating
		s.EconMintRemaining = ec.MintRemaining
		s.EconOrderBurnRate = ec.BurnRateOrder
		s.EconBurnImpactPct = chain.BurnImpactPct(ec)
		s.EconRewardTailFloorHMC = chain.RewardTailFloorHMC
		s.EconRewardHalvingInterval = chain.RewardHalvingIntervalBlocks
		s.EconNextHalvingBlock = chain.NextHalvingBlock(s.BlockHeight)
		s.EconExpectedEmptyHmcHour = chain.ExpectedEmptyMiningHMCPerHour(s.BlockHeight)
		s.EconBaseRewardNowHMC = chain.BaseRewardForBlockIndex(s.BlockHeight + 1)
	}
	ms := a.miner.Stats()
	s.MiningPoHBackend = ms.PoHBackend
	s.MiningRunning = ms.Running
	s.MiningAttemptsTotal = ms.AttemptsTotal
	s.MiningAttemptsPerSec = ms.AttemptsPerSec
	s.MiningSessionSec = ms.SessionSeconds
	s.MiningLastNonce = ms.LastNonce
	s.MiningLastEval = ms.LastEval
	s.MiningLastMod997 = ms.LastEvalMod
	s.MiningSessionSolves = ms.SessionSolves
	s.MiningTargetMod = ms.TargetMod
	if tm, err := a.chain.PoHTargetMod(r.Context()); err == nil && tm > 0 {
		s.MiningTargetMod = tm
	}
	s.MiningTargetModCap = chain.PoHTargetMaxMod
	s.MiningTargetModAtCap = chain.IsPoHTargetModAtCap(ms.TargetMod)
	s.MiningRewardHMC = ms.RewardHMC
	if ms.TaskSource != chain.TaskSourceOrder {
		if rw, err := a.chain.BaseRewardForNextBlock(r.Context()); err == nil {
			s.MiningRewardHMC = rw
		}
	}
	s.MiningWorkers = ms.Workers
	s.MiningThrottleTarget = ms.ThrottleCPUPct
	s.MiningTaskID = ms.TaskID
	s.MiningTaskKind = ms.TaskKind
	s.MiningTaskSource = ms.TaskSource
	s.MiningTaskArtifactHash = ms.TaskArtifactHash
	s.MiningTaskManifestPath = ms.TaskManifestPath
	nvGPU := queryNVIDIAMulti()
	amdGPU := queryAMDGPUMulti()
	var gpuDevs []MiningGPUDeviceMetrics
	var gpuRigs []MiningRigMetrics
	var sumGPUgh float64
	for _, g := range ms.GPUPoHDevices {
		tempC := g.TempC
		if g.Backend == "cuda" {
			for _, nv := range nvGPU {
				if nv.Index == g.Index {
					tempC = nv.TempC
					break
				}
			}
		} else if g.Backend == "opencl" && tempC < 0 {
			for _, am := range amdGPU {
				if am.Index == g.Index {
					tempC = am.TempC
					break
				}
			}
		}
		alias := a.loadGPUAlias(r.Context(), g.Backend, g.Index)
		displayName := strings.TrimSpace(alias)
		if displayName == "" {
			displayName = strings.TrimSpace(g.Name)
		}
		if displayName == "" {
			displayName = g.Label
		}
		sumGPUgh += g.HashrateGHS
		gpuDevs = append(gpuDevs, MiningGPUDeviceMetrics{
			Index: g.Index, Backend: g.Backend, Label: g.Label, Name: displayName,
			HashrateGHS: g.HashrateGHS, TempC: tempC,
		})
		gpuRigs = append(gpuRigs, MiningRigMetrics{
			WorkerID:     fmt.Sprintf("%s-gpu-%d", a.nodeID, g.Index),
			Name:         g.Label + " · " + displayName,
			HashrateGHS:  g.HashrateGHS,
			LastSeenUnix: time.Now().Unix(),
			IP:           "127.0.0.1",
			Online:       true,
			Source:       "local-gpu",
			GPUBackend:   g.Backend,
			GPUTempC:     tempC,
		})
	}
	s.MiningGPUDevices = gpuDevs
	s.MiningGPUTotalGHS = round2(sumGPUgh)
	s.MiningGPUCount = len(gpuDevs)
	s.MiningRigs = buildMiningRigsForMetrics(a.nodeID, ms.Running, ms.AttemptsPerSec, a.rigs, gpuRigs)
	if ms.Running {
		// Approximate “mining pressure” from the same CPU sample as telemetry.
		s.MiningLoad = s.CPUPct
	} else {
		s.MiningLoad = -1
	}

	ctx := r.Context()
	// Keep /api/metrics fast for dashboard telemetry (canonical overlay is best-effort).
	overlayCtx, overlayCancel := context.WithTimeout(ctx, 2*time.Second)
	canonMiningOverlay := a.overlayCanonicalMiningIntoSnapshot(overlayCtx, &s)
	overlayCancel()

	since1h := time.Now().Unix() - 3600
	if rw, err := a.chain.RewardWindowBreakdownSince(ctx, since1h); err == nil {
		localHasChain := rw.Blocks > 0 || rw.TotalHMC > 1e-12
		if localHasChain || !canonMiningOverlay {
			s.EconWindowSec = 3600
			s.EconWindowBlocks = rw.Blocks
			s.EconWindowBaseBlocks = rw.BaseBlocks
			s.EconWindowOrderBlocks = rw.OrderBlocks
			s.EconWindowBaseHMC = rw.BaseHMC
			s.EconWindowOrderHMC = rw.OrderHMC
			s.EconWindowTotalHMC = rw.TotalHMC
			if rw.TotalHMC > 1e-12 {
				s.EconWindowOrderShare = 100 * rw.OrderHMC / rw.TotalHMC
			}
			s.MiningPohBlocksLast1h = rw.Blocks
			s.MiningHmcLastHourApprox = rw.TotalHMC
		}
	}
	s.MiningInsightNote = "ETA/progress: heuristic (M × eval/s). HMC/hour = PoH blocks on this chain in the window; wallet follows /api/wallet/*."
	if canonMiningOverlay {
		s.MiningInsightNote = "Chain economics from canonical when local tip is empty. " + s.MiningInsightNote
	}
	s.MiningTargetBlockSec = chain.PoHRetargetTargetSec
	if s.MiningObservedBlockSec <= 0 {
		if avgSec, err := a.chain.RecentPoHAvgBlockSec(ctx, int(chain.PoHRetargetWindowBlocks)*4); err == nil && avgSec > 0 {
			s.MiningObservedBlockSec = avgSec
		} else {
			s.MiningObservedBlockSec = -1
		}
	}
	if ms.Running && ms.TargetMod >= 251 && ms.AttemptsPerSec >= 1e-6 {
		eta, prog, proj := computeMiningInsight(ms.AttemptsTotal, ms.TargetMod, ms.AttemptsPerSec, ms.RewardHMC)
		s.MiningEtaSecEst = eta
		s.MiningEtaProgress = prog
		s.MiningProjectedHmcHour = proj
	} else {
		s.MiningEtaSecEst = -1
		s.MiningEtaProgress = 0
		s.MiningProjectedHmcHour = 0
	}
	a.fillMetricsFleetHashrate(ctx, &s)
	writeMetricsJSON(w, s)
}

func miningGPUAliasKey(backend string, idx int) string {
	b := strings.ToLower(strings.TrimSpace(backend))
	if b == "" {
		b = "gpu"
	}
	return fmt.Sprintf("gpu_alias_%s_%d", b, idx)
}

func miningGPUEnabledKey(backend string, idx int) string {
	b := strings.ToLower(strings.TrimSpace(backend))
	if b == "" {
		b = "gpu"
	}
	return fmt.Sprintf("gpu_enabled_%s_%d", b, idx)
}

func miningGPUPriorityKey(backend string, idx int) string {
	b := strings.ToLower(strings.TrimSpace(backend))
	if b == "" {
		b = "gpu"
	}
	return fmt.Sprintf("gpu_priority_%s_%d", b, idx)
}

const miningProfileMetaKey = "mining_profile_mode"

func (a *app) loadGPUAlias(ctx context.Context, backend string, idx int) string {
	key := miningGPUAliasKey(backend, idx)
	var val string
	_ = a.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&val)
	return strings.TrimSpace(val)
}

func (a *app) loadGPUEnabled(ctx context.Context, backend string, idx int) bool {
	key := miningGPUEnabledKey(backend, idx)
	var val string
	_ = a.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&val)
	val = strings.ToLower(strings.TrimSpace(val))
	return val != "0" && val != "false" && val != "off"
}

func (a *app) loadGPUPriority(ctx context.Context, backend string, idx int) int {
	key := miningGPUPriorityKey(backend, idx)
	var val string
	_ = a.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&val)
	p, err := strconv.Atoi(strings.TrimSpace(val))
	if err != nil || p < 0 {
		return 100
	}
	if p > 9999 {
		return 9999
	}
	return p
}

func (a *app) loadMiningProfile(ctx context.Context) string {
	var val string
	_ = a.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, miningProfileMetaKey).Scan(&val)
	v := strings.ToLower(strings.TrimSpace(val))
	switch v {
	case "cpu", "gpu", "mixed":
		return v
	default:
		return "mixed"
	}
}

func (a *app) refreshMinerDevicePolicy(ctx context.Context) {
	if a.miner == nil {
		return
	}
	a.miner.SetMiningDevicePolicy(chain.MiningDevicePolicy{
		Profile: a.loadMiningProfile(ctx),
		GPUEnabled: func(backend string, idx int) bool {
			return a.loadGPUEnabled(ctx, backend, idx)
		},
		GPUPriority: func(backend string, idx int) int {
			return a.loadGPUPriority(ctx, backend, idx)
		},
		CPUEnabled: func() bool {
			return a.loadGPUEnabled(ctx, "cpu", 0)
		},
	})
}

func (a *app) upsertGPUAlias(ctx context.Context, backend string, idx int, alias string) error {
	key := miningGPUAliasKey(backend, idx)
	alias = strings.TrimSpace(alias)
	_, err := a.db.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, alias,
	)
	return err
}

func (a *app) upsertGPUEnabled(ctx context.Context, backend string, idx int, enabled bool) error {
	key := miningGPUEnabledKey(backend, idx)
	val := "0"
	if enabled {
		val = "1"
	}
	_, err := a.db.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, val,
	)
	return err
}

func (a *app) upsertGPUPriority(ctx context.Context, backend string, idx int, priority int) error {
	if priority < 0 {
		priority = 0
	}
	if priority > 9999 {
		priority = 9999
	}
	key := miningGPUPriorityKey(backend, idx)
	_, err := a.db.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, strconv.Itoa(priority),
	)
	return err
}

func (a *app) upsertMiningProfile(ctx context.Context, profile string) error {
	profile = strings.ToLower(strings.TrimSpace(profile))
	if profile == "" {
		profile = "mixed"
	}
	if profile != "cpu" && profile != "gpu" && profile != "mixed" {
		return fmt.Errorf("invalid profile")
	}
	_, err := a.db.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		miningProfileMetaKey, profile,
	)
	return err
}

func (a *app) handleGenesis(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireAdminAuth(w, r) {
		return
	}
	logAdminAction(r, "genesis")
	ctx := r.Context()
	b, bal, err := a.chain.InitGenesis(ctx, a.nodeID)
	if err != nil {
		if err == chain.ErrGenesisExists {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "genesis already initialized"})
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("GENESIS block hash=%s index=%d miner=%s", b.Hash, b.Index, b.MinerAddress)
	raw, _ := json.MarshalIndent(b, "", "  ")
	log.Printf("GENESIS block JSON:\n%s", string(raw))

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"block":    b,
		"balance":  bal,
		"chain_id": block.ChainID,
	})
}

func (a *app) handleWallet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	addr, bal, err := a.chain.Wallet(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	lookupAddr := strings.TrimSpace(addr)
	if lookupAddr == "" {
		lookupAddr = strings.TrimSpace(a.nodeID)
	}
	if a.signer != nil {
		if sa := strings.TrimSpace(a.signer.Address()); sa != "" {
			if lookupAddr == "" {
				lookupAddr = sa
			}
		}
	}
	var balanceUnits uint64
	var nextNonce uint64
	walletSource := "local_db"
	if lookupAddr != "" {
		if st, err := a.chain.TransferAddressState(ctx, lookupAddr); err == nil {
			balanceUnits = st.BalanceUnits
			nextNonce = st.NextNonce
		}
	}
	localMirrorUnits := balanceUnits
	localMirrorHMC := float64(localMirrorUnits) / 100_000_000.0
	skipCache := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("fresh")), "1") ||
		strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("fresh")), "true")
	// In worker/follower mode, prefer canonical wallet/address snapshot so UI
	// always matches VPS leader even when P2P peers are not configured.
	wantCanon := a.shouldUseCanonicalChainAPI() || a.networkModeActive()
	if wantCanon && lookupAddr != "" && !skipCache {
		if hmc, units, nonce, ok := a.readCanonicalWalletCache(lookupAddr); ok {
			bal = hmc
			balanceUnits = units
			nextNonce = nonce
			walletSource = "canonical_peer_cache"
		}
	}
	if wantCanon && lookupAddr != "" && (skipCache || walletSource != "canonical_peer_cache") {
		peerTimeout := 8 * time.Second
		if envBool("HACKME_DESKTOP_MODE", false) {
			peerTimeout = 12 * time.Second
		}
		peerCtx, cancel := context.WithTimeout(ctx, peerTimeout)
		if units, nonce, ok := a.fetchCanonicalAddressState(peerCtx, lookupAddr); ok {
			balanceUnits = units
			nextNonce = nonce
			bal = float64(units) / 100_000_000.0
			walletSource = "canonical_peer"
		}
		cancel()
	}
	if walletSource == "canonical_peer" {
		a.cacheCanonicalWallet(lookupAddr, bal, balanceUnits, nextNonce)
	}
	blendLocal := envBool("HACKME_WALLET_DESKTOP_BLEND_LOCAL", envBool("HACKME_DESKTOP_MODE", false))
	// Never overlay fork-local SQLite balances when canonical peer data exists — forked accounts rows could show bogus multi‑thousand HMC until reseed.
	if blendLocal && (walletSource == "canonical_peer" || walletSource == "canonical_peer_cache") {
		blendLocal = false
	}
	dispH := bal
	dispU := balanceUnits
	dispMode := "authoritative"
	if blendLocal && localMirrorHMC > bal+1e-12 {
		dispH = localMirrorHMC
		dispU = localMirrorUnits
		dispMode = "desktop_blend_local_gt_canonical"
	}
	displayAddr := strings.TrimSpace(addr)
	if displayAddr == "" {
		displayAddr = lookupAddr
	}
	out := map[string]any{
		"address":                    displayAddr,
		"balance_hmc":                bal,
		"balance_units":              balanceUnits,
		"next_nonce":                 nextNonce,
		"wallet_source":              walletSource,
		"balance_local_mirror_hmc":   localMirrorHMC,
		"balance_local_mirror_units": localMirrorUnits,
		"balance_display_hmc":        dispH,
		"balance_display_units":      dispU,
		"balance_display_mode":       dispMode,
	}
	if adminRequestAuthed(r) {
		if strings.TrimSpace(a.dataDir) != "" {
			out["data_directory"] = a.dataDir
		}
		if strings.TrimSpace(a.dbPath) != "" {
			out["database_file"] = a.dbPath
		}
		if a.signer != nil {
			out["signing_address"] = strings.TrimSpace(a.signer.Address())
		}
		devAddr := chain.DevFeeAddress
		if ec, err := a.chain.Economics(ctx); err == nil {
			if v := strings.TrimSpace(ec.DevFeeAddress); v != "" {
				devAddr = v
			}
		}
		out["transfer_fee_platform_address"] = devAddr
		out["network_fee_dev_share"] = chain.NetworkFeeDevShare
		out["network_fee_burn_share"] = chain.NetworkFeeBurnShare
	}
	a.writeWalletResponse(w, r, out)
}

func walletEarningsInt64Field(m map[string]any, key string) int64 {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case json.Number:
		n, err := v.Int64()
		if err == nil {
			return n
		}
	}
	return 0
}

func walletEarningsDataTxCount(out map[string]any) int64 {
	if out == nil {
		return 0
	}
	data, ok := out["data"].(map[string]any)
	if !ok || data == nil {
		if raw := out["data"]; raw != nil {
			b, err := json.Marshal(raw)
			if err == nil {
				var m map[string]any
				if json.Unmarshal(b, &m) == nil {
					data = m
				}
			}
		}
	}
	if data == nil {
		return 0
	}
	if n := walletEarningsInt64Field(data, "tx_count_window"); n > 0 {
		return n
	}
	if n := walletEarningsInt64Field(data, "tx_count_24h"); n > 0 {
		return n
	}
	if buckets, ok := data["buckets"].([]any); ok {
		var sum int64
		for _, b := range buckets {
			bm, ok := b.(map[string]any)
			if !ok {
				continue
			}
			sum += walletEarningsInt64Field(bm, "tx_count")
		}
		if sum > 0 {
			return sum
		}
	}
	return 0
}

func walletEarningsRemoteUsable(remote map[string]any, earnAddr string) bool {
	if remote == nil {
		return false
	}
	okb, pok := remote["ok"].(bool)
	if !pok || !okb {
		return false
	}
	data, ok := remote["data"].(map[string]any)
	if !ok || data == nil {
		return false
	}
	if earnAddr != "" {
		if a, _ := data["address"].(string); strings.TrimSpace(a) != "" &&
			!strings.EqualFold(strings.TrimSpace(a), earnAddr) {
			return false
		}
	}
	return true
}

func finalizeWalletEarningsCanonicalProxy(remote map[string]any) {
	if remote == nil {
		return
	}
	remote["source"] = "canonical_peer"
	delete(remote, "canonical_earnings_unavailable")
	delete(remote, "fork_hint")
}

// parseOptionalHMCAddress returns s if it matches the fixed HMC- + 16-hex display form; otherwise "".
func parseOptionalHMCAddress(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "HMC-") || len(s) != 20 {
		return ""
	}
	suf := strings.TrimPrefix(s, "HMC-")
	if len(suf) != 16 {
		return ""
	}
	if _, err := hex.DecodeString(suf); err != nil {
		return ""
	}
	return s
}

func (a *app) handleWalletEarnings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 32*time.Second)
	defer cancel()
	windowHours := 24
	if v := strings.TrimSpace(r.URL.Query().Get("window_hours")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			windowHours = n
		}
	}
	bucketSec := 3600
	if v := strings.TrimSpace(r.URL.Query().Get("bucket_sec")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			bucketSec = n
		}
	}
	addr, _, err := a.chain.Wallet(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	earnAddr := strings.TrimSpace(addr)
	if earnAddr == "" {
		earnAddr = strings.TrimSpace(a.nodeID)
	}
	if q := parseOptionalHMCAddress(r.URL.Query().Get("address")); q != "" {
		earnAddr = q
	}

	source := "local_db"
	canonAttempted := false
	canonOK := false
	// Followers overlay canonical earnings; desktop always uses canonical even when local PoH miner runs.
	// Command leaders keep local ledger while mining to avoid self-proxy loops.
	useCanonEarnings := a.shouldUseCanonicalChainAPI() && a.networkModeActive() &&
		(!a.miner.Running() || envBool("HACKME_DESKTOP_MODE", false))
	if useCanonEarnings {
		if base := strings.TrimRight(strings.TrimSpace(a.canonicalChainBaseURL()), "/"); base != "" {
			if u, err := url.Parse(base); err == nil {
				host := strings.ToLower(strings.TrimSpace(u.Host))
				loopback := host == "127.0.0.1:8080" || host == "localhost:8080" ||
					host == "127.0.0.1:18080" || host == "localhost:18080"
				if !loopback {
					canonAttempted = true
					qv := url.Values{}
					qv.Set("window_hours", strconv.Itoa(windowHours))
					qv.Set("bucket_sec", strconv.Itoa(bucketSec))
					if earnAddr != "" {
						qv.Set("address", earnAddr)
					}
					earnURL := strings.TrimRight(base, "/") + "/api/wallet/earnings?" + qv.Encode()
					peerSec := 12
					curlSec := 16
					if envBool("HACKME_DESKTOP_MODE", false) {
						peerSec = 5
						curlSec = 6
					}
					peerCtx, cancelPeer := context.WithTimeout(ctx, time.Duration(peerSec)*time.Second)
					req, err := http.NewRequestWithContext(peerCtx, http.MethodGet, earnURL, nil)
					if err == nil {
						client := &http.Client{Timeout: time.Duration(peerSec) * time.Second}
						if resp, err := client.Do(req); err == nil && resp != nil {
							if resp.StatusCode == http.StatusOK {
								var remote map[string]any
								if err := json.NewDecoder(resp.Body).Decode(&remote); err == nil {
									if walletEarningsRemoteUsable(remote, earnAddr) {
										_ = resp.Body.Close()
										cancelPeer()
										canonOK = true
										finalizeWalletEarningsCanonicalProxy(remote)
										writeJSON(w, remote)
										return
									}
								}
							}
							_ = resp.Body.Close()
						}
					}
					cancelPeer()
					curlCtx, cancelCurl := context.WithTimeout(ctx, time.Duration(curlSec+2)*time.Second)
					parsed, curlErr := fetchJSONViaCurlMax(curlCtx, earnURL, nil, curlSec)
					cancelCurl()
					if curlErr == nil && walletEarningsRemoteUsable(parsed, earnAddr) {
						canonOK = true
						finalizeWalletEarningsCanonicalProxy(parsed)
						writeJSON(w, parsed)
						return
					}
				}
			}
		}
	}

	earnings, err := a.chain.WalletEarningsSummary(ctx, earnAddr, windowHours, bucketSec)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := map[string]any{
		"ok":     true,
		"source": source,
		"data":   earnings,
	}
	a.attachWalletEarningsSyncMeta(ctx, out, canonAttempted, canonOK)
	writeJSON(w, out)
}

func (a *app) handleChain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := 20
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			limit = n
		}
	}
	var blocks []json.RawMessage
	var err error
	if fh := strings.TrimSpace(r.URL.Query().Get("from_height")); fh != "" {
		if minH, perr := strconv.ParseUint(fh, 10, 64); perr == nil {
			blocks, err = a.chain.ListBlocksFromHeight(r.Context(), minH, limit)
		} else {
			http.Error(w, "bad from_height", http.StatusBadRequest)
			return
		}
	} else {
		blocks, err = a.chain.ListBlocks(r.Context(), limit)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// When legacy/synced JSON omitted miner_address but pubkey is present, expose
	// miner_address_effective (same derivation as block.EffectiveMinerAddress) for explorers.
	out := make([]any, 0, len(blocks))
	for _, raw := range blocks {
		var b block.Block
		if err := json.Unmarshal(raw, &b); err != nil {
			out = append(out, raw)
			continue
		}
		eff := strings.TrimSpace(b.EffectiveMinerAddress())
		producer := strings.TrimSpace(addressFromPubKeyHex(b.MinerPubKey))
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			out = append(out, raw)
			continue
		}
		if eff != "" {
			m["miner_address_effective"] = eff
		}
		if producer != "" {
			m["miner_producer_address"] = producer
		}
		fixed, err := json.Marshal(m)
		if err != nil {
			out = append(out, raw)
			continue
		}
		out = append(out, json.RawMessage(fixed))
	}
	writeJSON(w, map[string]any{"blocks": out})
}

// mergeCanonicalEconomicsIntoStatus overlays economics and policy JSON from the canonical
// node's /api/status when a remote canonical base is configured. Keeps follower/worker
// dashboards and release gates aligned with canon without implying local ledger economics.
func (a *app) mergeCanonicalEconomicsIntoStatus(ctx context.Context, statusBody map[string]any) {
	if a.miner.Running() {
		return
	}
	base := strings.TrimRight(strings.TrimSpace(a.canonicalChainBaseURL()), "/")
	if base == "" {
		return
	}
	u, err := url.Parse(base)
	if err != nil {
		return
	}
	host := strings.ToLower(strings.TrimSpace(u.Host))
	if host == "127.0.0.1:8080" || host == "localhost:8080" {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/status", nil)
	if err != nil {
		return
	}
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp == nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var remote map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&remote); err != nil {
		return
	}
	if v, ok := remote["economics"]; ok {
		statusBody["economics"] = v
	}
	if v, ok := remote["crypto_policy"]; ok {
		statusBody["crypto_policy"] = v
	}
	if v, ok := remote["consensus_policy"]; ok {
		statusBody["consensus_policy"] = v
	}
}

func statusQueryLite(r *http.Request) bool {
	q := strings.TrimSpace(r.URL.Query().Get("lite"))
	return q == "1" || strings.EqualFold(q, "true")
}

// buildStatusLite serves fast polls for desktop dashboards: no coordinator fan-out,
// no remote economics merge, minimal SQLite reads.
func (a *app) buildStatusLite(ctx context.Context) map[string]any {
	networkModeActive := a.networkModeActive()
	localSoloAllowed := envBool("HACKME_CHAIN_LEADER_LOCAL_POH", false)
	poolCoordURL := strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_URL"))
	coordEff := strings.TrimRight(strings.TrimSpace(a.coordinatorBaseURL()), "/")
	canonBase := strings.TrimRight(strings.TrimSpace(a.canonicalChainBaseURL()), "/")
	body := map[string]any{
		"chain_id":                       block.ChainID,
		"version":                        Version,
		"commit":                         Commit,
		"build_date":                     BuildDate,
		"has_genesis":                    false,
		"tip_height":                     uint64(0),
		"tip_hash":                       "",
		"mining":                         a.miner.Running(),
		"node_address":                   a.nodeID,
		"network_mode_active":            networkModeActive,
		"local_solo_allowed":             localSoloAllowed,
		"chain_leader_local_poh":         localSoloAllowed,
		"pool_coordinator_url":           poolCoordURL,
		"pool_coordinator_url_effective": coordEff,
		"desktop_mode":                   envBool("HACKME_DESKTOP_MODE", false),
		"status_lite":                    true,
		"tip_sync_source":                "local_ledger",
	}
	// Hot dashboard poll: never block on remote canonical /api/status (curl fallback can take 6s).
	if hasC, hC, tipC, ok := a.readCanonicalTipCache(); ok {
		body["canonical_tip_height"] = hC
		body["canonical_tip_hash"] = tipC
		body["canonical_tip_has_genesis"] = hasC
		body["canonical_tip_ok"] = true
		body["canonical_tip_sync_source"] = "canonical_peer_cache"
	}
	localCtx, localCancel := context.WithTimeout(ctx, 250*time.Millisecond)
	has, _ := a.chain.HasGenesis(localCtx)
	h, tip, _ := a.chain.Tip(localCtx)
	localCancel()
	body["has_genesis"] = has
	body["tip_height"] = h
	body["tip_hash"] = tip
	if canonBase != "" {
		body["canonical_chain_base_url"] = canonBase
	}
	if pub := strings.TrimSpace(os.Getenv("HACKME_PUBLIC_AUTHORITY_BASE")); pub != "" {
		body["public_authority_base"] = pub
	}
	a.attachPoolLaneCached(body)
	if strings.TrimSpace(a.coordinatorBaseURL()) != "" {
		a.warmWorkStatsCacheAsync(a.coordinatorBaseURL(), false)
	}
	return body
}

func (a *app) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if statusQueryLite(r) {
		writeJSON(w, a.buildStatusLite(r.Context()))
		return
	}
	ctx := r.Context()
	// Keep /api/status responsive even under transient DB contention.
	statusCtx, cancelStatus := context.WithTimeout(ctx, 5*time.Second)
	defer cancelStatus()
	networkModeActive := a.networkModeActive()
	has := false
	h := uint64(0)
	tip := ""
	// Only chain command nodes may HTTP-start local WASM PoH; followers use worker/coordinator.
	localSoloAllowed := envBool("HACKME_CHAIN_LEADER_LOCAL_POH", false)
	sv := 0
	economics := any(nil)
	// Always report local SQLite schema revision for migrations and private_stage_gate.
	if v, err := a.chain.SchemaVersion(statusCtx); err == nil {
		sv = v
	}
	// Always snapshot local ledger tip + economics. Canonical / public-chain heights are separate fields.
	has, _ = a.chain.HasGenesis(statusCtx)
	h, tip, _ = a.chain.Tip(statusCtx)
	if ec, err := a.chain.Economics(statusCtx); err == nil {
		economics = ec
	}
	poolCoordURL := strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_URL"))
	statusBody := map[string]any{
		"chain_id":               block.ChainID,
		"version":                Version,
		"commit":                 Commit,
		"build_date":             BuildDate,
		"has_genesis":            has,
		"tip_height":             h,
		"tip_hash":               tip,
		"mining":                 a.miner.Running(),
		"node_address":           a.nodeID,
		"schema_version":         sv,
		"schema_expected":        store.CurrentSchemaVersion,
		"admin_auth_enabled":     adminAuthEnabled(),
		"network_mode_active":    networkModeActive,
		"local_solo_allowed":     localSoloAllowed,
		"chain_leader_local_poh": localSoloAllowed,
		"pool_coordinator_url":   poolCoordURL,
		"desktop_mode":           envBool("HACKME_DESKTOP_MODE", false),
		"economics":              economics,
		"sandbox_policy":         sandbox.Policy(),
		"p2p_policy": map[string]any{
			"ingress_allow_cidrs_count": len(a.p2pIngress.allowCIDRs),
			"ingress_deny_cidrs_count":  len(a.p2pIngress.denyCIDRs),
			"max_peers_per_24":          a.p2pIngress.maxPeersPer24,
			"token_fail_ban_sec":        a.p2pIngress.tokenBanSec,
			"sync_heavy_limit":          cap(a.p2pSyncHeavySem),
			"sync_heavy_inflight":       len(a.p2pSyncHeavySem),
		},
		"crypto_policy": map[string]any{
			"block_sig_algs_supported":    []string{chain.TransferSigAlgEd25519},
			"transfer_sig_algs_supported": []string{chain.TransferSigAlgEd25519},
			"pq_ready_wire_versioning":    true,
			"pq_note":                     "wire fields are versioned for future post-quantum signature rollout",
		},
		"consensus_policy": map[string]any{
			"simultaneous_block_rule": "first_valid_block_on_canonical_node_wins",
			"fork_resolution":         "no_reorg_v1_fail_closed",
			"fork_action":             "followers_stop_mining_and_reseed_from_canonical",
			"hybrid_signer_enabled":   envBool("HACKME_POOL_HYBRID_SIGNER_ENABLED", false),
		},
		"pool_sync": a.poolSyncStatusPayload(),
	}
	if networkModeActive && !a.miner.Running() {
		// Canonical snapshot for pool/network dashboards — never replaces local SQLite tip_height.
		if hasC, hC, tipC, ok := a.readCanonicalTipCache(); ok {
			statusBody["canonical_tip_height"] = hC
			statusBody["canonical_tip_hash"] = tipC
			statusBody["canonical_tip_has_genesis"] = hasC
			statusBody["canonical_tip_ok"] = true
			statusBody["canonical_tip_sync_source"] = "canonical_peer_cache"
		}
		quickCtx, quickCancel := context.WithTimeout(ctx, 900*time.Millisecond)
		hasGen, hNow, tipNow, okNow := a.fetchCanonicalStatusTip(quickCtx)
		quickCancel()
		if okNow && strings.TrimSpace(tipNow) != "" {
			statusBody["canonical_tip_height"] = hNow
			statusBody["canonical_tip_hash"] = tipNow
			statusBody["canonical_tip_has_genesis"] = hasGen
			statusBody["canonical_tip_ok"] = true
			statusBody["canonical_tip_sync_source"] = "canonical_peer"
			a.cacheCanonicalTip(hasGen, hNow, tipNow)
		}
		a.mergeCanonicalEconomicsIntoStatus(statusCtx, statusBody)
	} else {
		a.mergeCanonicalEconomicsIntoStatus(statusCtx, statusBody)
		a.applyCanonicalChainTipToStatusMap(statusCtx, statusBody)
		if ct := asUint64(statusBody["canonical_tip_height"]); ct > 0 && strings.TrimSpace(asString(statusBody["canonical_tip_hash"])) != "" {
			hasG, _ := statusBody["canonical_tip_has_genesis"].(bool)
			a.cacheCanonicalTip(hasG, ct, asString(statusBody["canonical_tip_hash"]))
		}
	}

	coordEff := strings.TrimRight(strings.TrimSpace(a.coordinatorBaseURL()), "/")
	canonBase := strings.TrimRight(strings.TrimSpace(a.canonicalChainBaseURL()), "/")
	p2pConfigured := strings.TrimSpace(os.Getenv("HACKME_P2P_PEERS")) != ""
	canonConfigured := canonBase != "" && walletCanonicalBaseUsable(canonBase)
	canonDisplayHost := ""
	if canonConfigured {
		if u, err := url.Parse(canonBase); err == nil {
			canonDisplayHost = strings.TrimSpace(u.Host)
		}
	}
	remoteCanon := false
	if tipOk, _ := statusBody["canonical_tip_ok"].(bool); tipOk && canonConfigured {
		remoteCanon = true
	}
	if !remoteCanon && canonConfigured {
		probeCtx, probeCancel := context.WithTimeout(ctx, 2*time.Second)
		_, _, _, remoteCanon = a.fetchCanonicalStatusTip(probeCtx)
		probeCancel()
	}
	if !remoteCanon && canonConfigured {
		if u, err := url.Parse(canonBase); err == nil {
			h := strings.ToLower(strings.TrimSpace(u.Host))
			remoteCanon = h != "" && h != "127.0.0.1:8080" && h != "localhost:8080"
		}
	}
	hints := make([]string, 0, 6)
	stagingJoin := PublicStagingJoinEnvExport()
	pubAuth := strings.TrimSpace(os.Getenv("HACKME_PUBLIC_AUTHORITY_BASE")) != ""
	if !networkModeActive {
		hints = append(hints, "Standalone: local SQLite only. Public pool: network mode + Copy env snippet + restart + Mining → Start worker.")
	}
	if networkModeActive && coordEff == "" {
		hints = append(hints, "Set HACKME_POOL_COORDINATOR_URL or start the worker once so a coordinator URL is stored.")
	}
	if networkModeActive && canonBase == "" {
		hints = append(hints, "Set HACKME_CANONICAL_CHAIN_URL or HACKME_P2P_PEERS so tip/balance match the pool.")
	}
	if networkModeActive && !p2pConfigured && remoteCanon {
		if pubAuth {
			hints = append(hints, "HACKME_PUBLIC_AUTHORITY_BASE pins pool + canonical; add HACKME_P2P_PEERS only to sync local SQLite block height.")
		} else {
			hints = append(hints, "Optional HACKME_P2P_PEERS keeps local SQLite height closer to the network.")
		}
	}
	if len(hints) == 0 && networkModeActive && coordEff != "" && remoteCanon {
		hints = append(hints, "Pool path ready — Mining → Start worker.")
	}

	// Open-network sync diagnostics (fast path: in-memory P2P peer heights, no extra HTTP).
	netSync := map[string]any{
		"p2p_enabled":                  a.p2p != nil && a.p2p.Enabled(),
		"state_replay_enabled":         envBool("HACKME_P2P_SYNC_STATE_REPLAY_ENABLED", false),
		"background_sync_interval_sec": p2pBackgroundSyncIntervalSec(),
		"bind_addr":                    strings.TrimSpace(os.Getenv("HACKME_BIND_ADDR")),
		"http_cors_configured":         strings.TrimSpace(os.Getenv("HACKME_HTTP_CORS_ALLOW_ORIGIN")) != "",
		"public_authority_base":        strings.TrimSpace(os.Getenv("HACKME_PUBLIC_AUTHORITY_BASE")),
	}
	if a.p2p != nil && a.p2p.Enabled() {
		ph := a.p2p.BuildSyncHint(h, tip)
		netSync["p2p_sync_hint"] = ph
		if ph.LagBlocks > 0 {
			hints = append(hints, fmt.Sprintf("P2P: local height %d lags healthy peer by %d blocks (best peer %s). Enable HACKME_P2P_SYNC_STATE_REPLAY_ENABLED=1 and optional HACKME_P2P_BACKGROUND_SYNC_SEC=30 on followers, or POST /api/p2p/sync/run with admin token.",
				ph.LocalHeight, ph.LagBlocks, strings.TrimSpace(ph.BestPeerURL)))
		}
	}
	if ct := asUint64(statusBody["canonical_tip_height"]); ct > 0 && has {
		lag := int64(ct) - int64(h)
		if lag < 0 {
			lag = 0
		}
		netSync["canonical_ledger_lag_blocks"] = uint64(lag)
		if lag > 0 && networkModeActive {
			hints = append(hints, fmt.Sprintf("SQLite is %d blocks behind public tip — expected until P2P/bootstrap catches up (same policy_hash).", uint64(lag)))
		}
	} else {
		netSync["canonical_ledger_lag_blocks"] = 0
	}
	statusBody["network_sync"] = netSync

	// tip_height / tip_hash are always local SQLite; explorer badges use this + canonical_tip_*.
	statusBody["tip_sync_source"] = "local_ledger"
	statusBody["pool_coordinator_url_effective"] = coordEff
	statusBody["miner_public_ux"] = map[string]any{
		"coordinator_url_effective":  coordEff,
		"canonical_chain_base":       canonBase,
		"canonical_chain_configured": canonConfigured,
		"canonical_display_host":     canonDisplayHost,
		"canonical_remote_reachable": remoteCanon,
		"p2p_peers_configured":       p2pConfigured,
		"worker_subprocess_running":  a.workerProcessRunning(),
		"hints_en":                   hints,
		"staging_command_base":       defaultPublicStagingCommandBase,
		"staging_coordinator_base":   defaultPublicStagingCoordinatorBase,
		"staging_join_env_export":    stagingJoin,
	}

	poolLaneCtx, poolLaneCancel := context.WithTimeout(ctx, 3*time.Second)
	a.attachPoolLaneToStatus(poolLaneCtx, statusBody)
	poolLaneCancel()

	writeJSON(w, statusBody)
}

func (a *app) handleMiningStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireAdminAuth(w, r) {
		return
	}
	logAdminAction(r, "mining_start")
	ok, err := a.chain.HasGenesis(r.Context())
	if err != nil || !ok {
		http.Error(w, "genesis required", http.StatusPreconditionFailed)
		return
	}
	// Local WASM PoH is restricted to explicit chain command nodes; everyone else uses worker/coordinator.
	allowChainLeaderPoH := envBool("HACKME_CHAIN_LEADER_LOCAL_POH", false)
	networkModeActive := a.networkModeActive()
	if !allowChainLeaderPoH {
		w.WriteHeader(http.StatusConflict)
		code := "local_poh_disabled"
		msg := "local WASM PoH via HTTP is disabled on this node; use POST /api/worker/start with HACKME_POOL_COORDINATOR_URL (public pool mining). Set HACKME_CHAIN_LEADER_LOCAL_POH=1 only on the dedicated chain command process that produces canonical blocks."
		if networkModeActive {
			code = "local_poh_disabled_in_network_mode"
			msg = "local WASM PoH is disabled in network mode; use POST /api/worker/start. Chain command nodes may set HACKME_CHAIN_LEADER_LOCAL_POH=1 if they must run local block production."
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"code":  code,
			"error": msg,
		})
		return
	}
	a.refreshMinerDevicePolicy(r.Context())
	a.miner.Start(context.Background())
	writeJSON(w, map[string]any{"ok": true})
}

func (a *app) handleMiningStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireAdminAuth(w, r) {
		return
	}
	logAdminAction(r, "mining_stop")
	a.miner.Stop()
	writeJSON(w, map[string]any{"ok": true})
}

// resolveWorkerpohExePath returns workerpoh.exe next to hackme (backend-aware when possible).
func resolveWorkerpohExePath() (string, error) {
	backend := strings.TrimSpace(os.Getenv("HACKME_GPU_BACKEND"))
	return resolveWorkerpohExePathForBackend(backend)
}

func resolveWorkerpohExePathForBackend(backend string) (string, error) {
	tryDir := func(dir string) (string, bool) {
		names := []string{"workerpoh-opencl.exe", "workerpoh.exe"}
		if strings.EqualFold(backend, "cuda") {
			names = []string{"workerpoh-cuda.exe", "workerpoh-opencl.exe", "workerpoh.exe"}
		}
		for _, name := range names {
			wp := filepath.Join(dir, name)
			if st, err := os.Stat(wp); err == nil && !st.IsDir() {
				if abs, err := filepath.Abs(wp); err == nil {
					return abs, true
				}
				return wp, true
			}
		}
		return "", false
	}
	exe, err := os.Executable()
	if err == nil {
		if sym, err := filepath.EvalSymlinks(exe); err == nil && sym != "" {
			exe = sym
		}
		if p, ok := tryDir(filepath.Clean(filepath.Dir(exe))); ok {
			return p, nil
		}
	}
	if p, ok := tryDir("."); ok {
		return p, nil
	}
	return "", errors.New("workerpoh.exe not found next to hackme.exe — use the full Windows zip from the release page")
}

type workerStartRequest struct {
	CoordURL    string  `json:"coord_url"`
	CoordToken  string  `json:"coord_token"`
	WorkerID    string  `json:"worker_id"`
	BatchSize   uint64  `json:"batch_size"`
	HashrateGHS float64 `json:"hashrate_gh_s"`
	GPUBackend  string  `json:"gpu_backend"`
}

func (a *app) handleWorkerStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireAdminAuth(w, r) {
		return
	}
	logAdminAction(r, "worker_start")
	var req workerStartRequest
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	coordURL := strings.TrimRight(strings.TrimSpace(req.CoordURL), "/")
	if coordURL == "" {
		coordURL = a.coordinatorBaseURL()
	}
	if coordURL == "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusPreconditionFailed)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"code":  "coordinator_url_required",
			"error": "coordinator url required: set HACKME_POOL_COORDINATOR_URL, or pass coord_url in JSON (dashboard default), or HACKME_P2P_PEERS for infer",
		})
		return
	}
	workerID := strings.TrimSpace(req.WorkerID)
	if workerID == "" {
		workerID = strings.TrimSpace(os.Getenv("WORKER_ID"))
	}
	if workerID == "" {
		if h, err := os.Hostname(); err == nil && strings.TrimSpace(h) != "" {
			workerID = "worker-" + sanitizeWorkerIDHost(h)
		} else {
			workerID = "worker-local-01"
		}
	}
	remoteCoord := coordinatorURLLooksRemote(coordURL)
	batchSize := req.BatchSize
	if batchSize == 0 {
		if v := strings.TrimSpace(os.Getenv("HACKME_WORKER_BATCH_SIZE")); v != "" {
			if x, err := strconv.ParseUint(v, 10, 64); err == nil && x > 0 {
				batchSize = x
			}
		}
		if batchSize == 0 {
			if remoteCoord {
				// GPU desktop: large batches; CPU-only remote workers should set HACKME_WORKER_BATCH_SIZE lower in env.
				if strings.EqualFold(strings.TrimSpace(os.Getenv("HACKME_GPU_DISABLE")), "1") {
					batchSize = 1_048_576
				} else if v := strings.TrimSpace(os.Getenv("HACKME_WORKER_ENGINE")); v == "loop" || v == "curl" {
					batchSize = 1_048_576
				} else {
					batchSize = 4_194_304
				}
			} else {
				batchSize = 4_000_000
			}
		}
	}
	hashrateGHS := req.HashrateGHS
	if hashrateGHS <= 0 {
		if v := strings.TrimSpace(os.Getenv("HACKME_WORKER_HASHRATE_GHS")); v != "" {
			if x, err := strconv.ParseFloat(v, 64); err == nil && x > 0 {
				hashrateGHS = x
			}
		}
		// Leave 0 — workerpoh reports live GH/s to coordinator; no fake 0.9 GH/s floor on CPU rigs.
	}
	coordToken := resolveCoordinatorToken(strings.TrimSpace(req.CoordToken))
	if coordToken == "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusPreconditionFailed)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"code":  "coordinator_token_required",
			"error": "coordinator token required: set HACKME_POOL_COORDINATOR_TOKEN or HACKME_ADMIN_TOKEN on the node, or paste the coordinator admin token in the dashboard admin field / send X-Hackme-Admin-Token, or pass coord_token in JSON",
		})
		return
	}

	a.workerMu.Lock()
	defer a.workerMu.Unlock()
	if a.workerCmd != nil && a.workerCmd.Process != nil && a.workerCmd.ProcessState == nil {
		writeJSON(w, map[string]any{
			"ok":            true,
			"already":       true,
			"running":       true,
			"pid":           a.workerCmd.Process.Pid,
			"coord_url":     a.workerCoordURL,
			"worker_id":     a.workerID,
			"batch_size":    a.workerBatchSize,
			"hashrate_gh_s": a.workerHashrate,
			"log_path":      a.workerLogPath,
		})
		return
	}

	// Safety: keep follower node mining OFF while using coordinator worker mode.
	a.miner.Stop()

	logDir := filepath.Join(".", "logs")
	_ = os.MkdirAll(logDir, 0o755)
	logPath := filepath.Join(logDir, "worker_participant.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		http.Error(w, "worker log open failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	repoRoot := resolveWorkerRepoRoot(strings.TrimSpace(a.dataDir))
	workerEnv := []string{
		"COORD_URL=" + coordURL,
		"COORD_ADMIN_TOKEN=" + coordToken,
		"COORD_TOKEN=" + coordToken,
		"WORKER_ID=" + workerID,
		"BATCH_SIZE=" + strconv.FormatUint(batchSize, 10),
		"HASHRATE_GHS=" + strconv.FormatFloat(hashrateGHS, 'f', -1, 64),
	}
	if v := strings.TrimSpace(os.Getenv("HACKME_WORKER_HTTP_TIMEOUT")); v != "" {
		workerEnv = append(workerEnv, "HACKME_WORKER_HTTP_TIMEOUT="+v)
	}
	if v := strings.TrimSpace(os.Getenv("HACKME_WORKER_CLAIM_TIMEOUT")); v != "" {
		workerEnv = append(workerEnv, "HACKME_WORKER_CLAIM_TIMEOUT="+v)
	} else if remoteCoord {
		workerEnv = append(workerEnv, "HACKME_WORKER_CLAIM_TIMEOUT=90s")
	} else {
		workerEnv = append(workerEnv, "HACKME_WORKER_CLAIM_TIMEOUT=35s")
	}
	if v := strings.TrimSpace(os.Getenv("HACKME_WORKER_CLAIM_COOLDOWN_MS")); v != "" {
		workerEnv = append(workerEnv, "HACKME_WORKER_CLAIM_COOLDOWN_MS="+v)
	}
	gpuBackend := strings.TrimSpace(req.GPUBackend)
	if gpuBackend == "" {
		gpuBackend = strings.TrimSpace(os.Getenv("HACKME_GPU_BACKEND"))
	}
	if gpuBackend == "" || strings.EqualFold(gpuBackend, "auto") {
		if v := resolveAutoGPUBackend(repoRoot); v != "" {
			gpuBackend = v
		}
	}
	if gpuBackend == "" || strings.EqualFold(gpuBackend, "auto") {
		if v := strings.TrimSpace(os.Getenv("HACKME_GPU_BACKEND")); v != "" && !strings.EqualFold(v, "auto") {
			gpuBackend = v
		}
	}
	if gpuBackend != "" && !strings.EqualFold(gpuBackend, "auto") {
		workerEnv = append(workerEnv, "HACKME_GPU_BACKEND="+gpuBackend)
	}
	if strings.EqualFold(gpuBackend, "cuda") {
		cudaBin := filepath.Join(repoRoot, "bin", "workerpoh-cuda")
		if st, err := os.Stat(cudaBin); err == nil && !st.IsDir() {
			workerEnv = append(workerEnv, "WORKER_BIN="+cudaBin)
		}
	}
	if strings.EqualFold(gpuBackend, "opencl") {
		oclBin := filepath.Join(repoRoot, "bin", "workerpoh-opencl")
		if st, err := os.Stat(oclBin); err == nil && !st.IsDir() {
			workerEnv = append(workerEnv, "WORKER_BIN="+oclBin, "HACKME_FORCE_OPENCL=1")
		}
	}
	if strings.EqualFold(gpuBackend, "cpu") || strings.TrimSpace(os.Getenv("HACKME_GPU_DISABLE")) == "1" {
		cpuBin := filepath.Join(repoRoot, "bin", "workerpoh-cpu")
		if st, err := os.Stat(cpuBin); err == nil && !st.IsDir() {
			workerEnv = append(workerEnv, "WORKER_BIN="+cpuBin)
		}
	}
	if strings.TrimSpace(os.Getenv("HACKME_DESKTOP_GPU_POOL")) == "1" {
		workerEnv = append(workerEnv, "HACKME_DESKTOP_GPU_POOL=1")
	}
	if v := strings.TrimSpace(os.Getenv("GPU_CHUNK")); v != "" {
		workerEnv = append(workerEnv, "GPU_CHUNK="+v)
	}
	if v := strings.TrimSpace(os.Getenv("SEARCH_TIMEOUT_MS")); v != "" {
		workerEnv = append(workerEnv, "SEARCH_TIMEOUT_MS="+v)
	}
	workerEnv = appendWorkerEnvPassthrough(workerEnv)
	if v := strings.TrimSpace(os.Getenv("HACKME_WORKER_SUBMIT_TIMEOUT")); v != "" {
		workerEnv = append(workerEnv, "HACKME_WORKER_SUBMIT_TIMEOUT="+v)
	} else if remoteCoord {
		workerEnv = append(workerEnv, "HACKME_WORKER_SUBMIT_TIMEOUT=120s")
	} else {
		workerEnv = append(workerEnv, "HACKME_WORKER_SUBMIT_TIMEOUT=90s")
	}
	if workerSignSubmitsEffective(coordURL) {
		seedHex, err := minerSubmitSeedHexForDataDir(strings.TrimSpace(a.dataDir))
		if err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusPreconditionFailed)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":    false,
				"code":  "miner_seed_invalid",
				"error": err.Error(),
			})
			return
		}
		if runtime.GOOS == "windows" {
			workerEnv = append(workerEnv, "HACKME_MINER_ED25519_SEED_HEX="+seedHex)
		} else {
			ms := resolveMinersignBinPath()
			if ms == "" {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusPreconditionFailed)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok":    false,
					"code":  "minersign_binary_missing",
					"error": "public pool expects signed submits; build minersign: go build -o minersign ./cmd/minersign (or set HACKME_MINERSIGN_BIN)",
				})
				return
			}
			nonceDir := filepath.Join(repoRoot, "logs")
			_ = os.MkdirAll(nonceDir, 0o755)
			safeWid := strings.Map(func(r rune) rune {
				switch {
				case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
					return r
				default:
					return '_'
				}
			}, workerID)
			if safeWid == "" {
				safeWid = "worker"
			}
			noncePath := filepath.Join(nonceDir, "miner_submit_nonce."+safeWid+".seq")
			workerEnv = append(workerEnv,
				"HACKME_WORKER_SIGN_SUBMITS=1",
				"HACKME_MINER_ED25519_SEED_HEX="+seedHex,
				"MINERSIGN_BIN="+ms,
				"HACKME_MINER_NONCE_FILE="+noncePath,
			)
		}
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		winRoot := repoRoot
		if exe, err := os.Executable(); err == nil {
			winRoot = filepath.Dir(exe)
		}
		winFleetStarted := false
		if fleetEnabledFromEnv() {
			plan := buildWorkerFleetPlan(winRoot, workerID)
			if plan.TotalSlots > 1 {
				if cmds, err := startWorkerFleetProcesses(winRoot, coordURL, coordToken, workerID, batchSize, logPath); err == nil && len(cmds) > 0 {
					cmd = cmds[0]
					winFleetStarted = true
				}
			}
		}
		if !winFleetStarted {
			winBackend := gpuBackend
			if winBackend == "" || strings.EqualFold(winBackend, "auto") {
				if v := resolveAutoGPUBackend(repoRoot); v != "" {
					winBackend = v
				} else {
					winBackend = "auto"
				}
			}
			wp, err := resolveWorkerpohExePathForBackend(winBackend)
			if err != nil {
				_ = f.Close()
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusPreconditionFailed)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok":    false,
					"code":  "workerpoh_missing",
					"error": err.Error(),
				})
				return
			}
			wpArgs := []string{
				"-coord", coordURL,
				"-token", coordToken,
				"-worker", workerID,
				"-batch", strconv.FormatUint(batchSize, 10),
			}
			if !strings.EqualFold(winBackend, "cpu") && !strings.EqualFold(os.Getenv("HACKME_GPU_DISABLE"), "1") {
				wpArgs = append(wpArgs, "-gpu-backend", winBackend)
			}
			if v := strings.TrimSpace(os.Getenv("HACKME_GPU_DEVICE")); v != "" {
				wpArgs = append(wpArgs, "-gpu-device", v)
			}
			cmd = exec.Command(wp, wpArgs...)
		}
	} else {
		useGPUWorker := true
		if v := strings.TrimSpace(os.Getenv("HACKME_WORKER_ENGINE")); v != "" {
			useGPUWorker = !strings.EqualFold(v, "loop") && !strings.EqualFold(v, "curl")
		}
		workerScript := filepath.Clean(filepath.Join(repoRoot, "scripts", "ops", "worker_autostart.sh"))
		if !useGPUWorker {
			workerScript = filepath.Clean(filepath.Join(repoRoot, "scripts", "ops", "worker_loop.sh"))
		}
		if st, err := os.Stat(workerScript); err != nil || st.IsDir() {
			_ = f.Close()
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			msg := "worker script not found"
			if err != nil {
				msg = msg + ": " + err.Error()
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":    false,
				"code":  "worker_script_missing",
				"error": msg + " (" + workerScript + ")",
			})
			return
		}
		workerEnv = append(workerEnv,
			"COORD_TOKEN="+coordToken,
			"HACKME_REPO_ROOT="+repoRoot,
		)
		if seedHex, err := minerSubmitSeedHexForDataDir(strings.TrimSpace(a.dataDir)); err == nil && seedHex != "" {
			workerEnv = append(workerEnv, "HACKME_MINER_ED25519_SEED_HEX="+seedHex)
		}
		cmd = exec.Command("bash", workerScript)
		cmd.Dir = repoRoot
	}
	cmd.Stdout = f
	cmd.Stderr = f
	cmd.Env = append(os.Environ(), workerEnv...)
	if err := cmd.Start(); err != nil {
		_ = f.Close()
		http.Error(w, "worker start failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	a.workerCmd = cmd
	a.workerLogPath = logPath
	a.workerStartedAt = time.Now().Unix()
	a.workerCoordURL = coordURL
	a.workerID = workerID
	a.workerBatchSize = batchSize
	a.workerHashrate = hashrateGHS
	go func(localCmd *exec.Cmd, localF *os.File) {
		waitErr := localCmd.Wait()
		if waitErr != nil {
			log.Printf("worker_loop subprocess: %v", waitErr)
		} else if ps := localCmd.ProcessState; ps != nil {
			log.Printf("worker_loop subprocess: exit code %d", ps.ExitCode())
		}
		_ = localF.Close()
		a.workerMu.Lock()
		if a.workerCmd == localCmd {
			a.workerCmd = nil
			a.workerCoordURL = ""
			a.workerID = ""
			a.workerBatchSize = 0
			a.workerHashrate = 0
		}
		a.workerMu.Unlock()
	}(cmd, f)
	writeJSON(w, map[string]any{
		"ok":            true,
		"running":       true,
		"pid":           cmd.Process.Pid,
		"coord_url":     coordURL,
		"worker_id":     workerID,
		"batch_size":    batchSize,
		"hashrate_gh_s": hashrateGHS,
		"log_path":      logPath,
	})
}

func (a *app) handleWorkerStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireAdminAuth(w, r) {
		return
	}
	logAdminAction(r, "worker_stop")
	a.workerMu.Lock()
	defer a.workerMu.Unlock()
	killExternalWorkerFleet(resolveWorkerRepoRoot(strings.TrimSpace(a.dataDir)))
	if a.workerCmd == nil || a.workerCmd.Process == nil || a.workerCmd.ProcessState != nil {
		a.workerCoordURL = ""
		a.workerID = ""
		a.workerBatchSize = 0
		a.workerHashrate = 0
		writeJSON(w, map[string]any{"ok": true, "running": false})
		return
	}
	pid := a.workerCmd.Process.Pid
	_ = a.workerCmd.Process.Kill()
	a.workerCmd = nil
	a.workerCoordURL = ""
	a.workerID = ""
	a.workerBatchSize = 0
	a.workerHashrate = 0
	writeJSON(w, map[string]any{"ok": true, "running": false, "stopped_pid": pid})
}

func (a *app) handleWorkerStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.workerMu.Lock()
	defer a.workerMu.Unlock()
	running := a.workerCmd != nil && a.workerCmd.Process != nil && a.workerCmd.ProcessState == nil
	pid := 0
	if running {
		pid = a.workerCmd.Process.Pid
	}
	logRoot := filepath.Join(resolveWorkerRepoRoot(strings.TrimSpace(a.dataDir)), "logs")
	measuredGH := parseWorkerpohMeasuredGHs(logRoot)
	logFresh := workerLogFresh(logRoot, 120)
	workerID := a.workerID
	coordURL := a.workerCoordURL
	startedAt := a.workerStartedAt
	logPath := a.workerLogPath
	hashrateGHS := a.workerHashrate
	// Desktop autostart runs workerpoh outside workerCmd; detect via live log tail.
	if !running && logFresh && measuredGH > 0 {
		running = true
		if workerID == "" {
			workerID = workerIDFromLatestWorkerpohLog(logRoot)
		}
		if coordURL == "" {
			coordURL = a.coordinatorBaseURL()
		}
		if logPath == "" {
			logPath = latestWorkerpohLogPath(logRoot)
		}
	}
	if !running && measuredGH > 0 && !logFresh {
		measuredGH = 0
	}
	writeJSON(w, map[string]any{
		"ok":                     true,
		"running":                running,
		"pid":                    pid,
		"started_at_unix":        startedAt,
		"coord_url":              coordURL,
		"worker_id":              workerID,
		"batch_size":             a.workerBatchSize,
		"hashrate_gh_s":          hashrateGHS,
		"measured_hashrate_gh_s": measuredGH,
		"log_path":               logPath,
		"external_worker":        running && pid == 0,
	})
}

type workerSettlementStateEntry struct {
	SettledHMC     float64 `json:"settled_hmc"`
	PayoutAddress  string  `json:"payout_address,omitempty"`
	LastTxHash     string  `json:"last_tx_hash,omitempty"`
	LastSettleUnix int64   `json:"last_settle_unix,omitempty"`
}

type workerSettlementMeta struct {
	LastForceUnix int64 `json:"last_force_unix,omitempty"`
}

type workerSettlementState struct {
	Workers map[string]workerSettlementStateEntry `json:"workers"`
	Meta    workerSettlementMeta                  `json:"meta,omitempty"`
}

func workerPayoutMapFromEnv() map[string]string {
	raw := strings.TrimSpace(os.Getenv("HACKME_WORKER_PAYOUT_MAP"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("WORKER_PAYOUT_MAP"))
	}
	out := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		p := strings.TrimSpace(part)
		if p == "" || !strings.Contains(p, "=") {
			continue
		}
		k, v, _ := strings.Cut(p, "=")
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k != "" && v != "" {
			out[k] = v
		}
	}
	return out
}

func parseAnyFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case int32:
		return float64(t)
	case uint64:
		return float64(t)
	case uint32:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f
	default:
		return 0
	}
}

func workerSettlementStatePath() string {
	if p := strings.TrimSpace(os.Getenv("HACKME_WORKER_SETTLEMENT_STATE_FILE")); p != "" {
		return p
	}
	if p := strings.TrimSpace(os.Getenv("WORKER_SETTLEMENT_STATE_FILE")); p != "" {
		return p
	}
	if dd := strings.TrimSpace(os.Getenv("HACKME_DATA_DIR")); dd != "" {
		return filepath.Join(dd, "worker_settlement_state.json")
	}
	return filepath.Join("data", "worker_settlement_state.json")
}

func settlementWindowConfigNow() (minSettleHMC float64, dailyForceIntervalSec int64, dailyMinSettleHMC float64) {
	minSettleHMC = 0.01
	if v := strings.TrimSpace(os.Getenv("MIN_SETTLE_HMC")); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil && n > 0 {
			minSettleHMC = n
		}
	}
	dailyForceIntervalSec = 86400
	if v := strings.TrimSpace(os.Getenv("DAILY_FORCE_INTERVAL_SEC")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			dailyForceIntervalSec = n
		}
	}
	dailyMinSettleHMC = 0.0001
	if v := strings.TrimSpace(os.Getenv("DAILY_MIN_SETTLE_HMC")); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil && n > 0 {
			dailyMinSettleHMC = n
		}
	}
	return minSettleHMC, dailyForceIntervalSec, dailyMinSettleHMC
}

func (a *app) handleWorkerSettlement(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	base := a.coordinatorBaseURL()
	if base == "" {
		writeJSON(w, map[string]any{
			"ok":     false,
			"reason": "coordinator_not_configured",
		})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	ws, err := fetchCoordinatorWorkStats(ctx, base, true)
	if err != nil {
		writeJSON(w, map[string]any{
			"ok":      false,
			"reason":  "coordinator_unavailable",
			"message": err.Error(),
			"source":  base,
		})
		return
	}
	statePath := workerSettlementStatePath()
	state := workerSettlementState{Workers: map[string]workerSettlementStateEntry{}}
	if raw, err := os.ReadFile(statePath); err == nil {
		_ = json.Unmarshal(raw, &state)
		if state.Workers == nil {
			state.Workers = map[string]workerSettlementStateEntry{}
		}
	}
	ensureCoordinatorWorkersMap(ws)
	if repairWorkerSettlementState(&state, coordinatorWorkersMap(ws)) {
		if b, err := json.MarshalIndent(state, "", "  "); err == nil {
			_ = os.MkdirAll(filepath.Dir(statePath), 0o700)
			_ = os.WriteFile(statePath, b, 0o600)
		}
	}
	workers := coordinatorWorkersMap(ws)
	minSettleHMC, dailyForceIntervalSec, dailyMinSettleHMC := settlementWindowConfigNow()
	nowUnix := time.Now().Unix()
	lastForceUnix := state.Meta.LastForceUnix
	if lastForceUnix < 0 {
		lastForceUnix = 0
	}
	nextSweepUnix := lastForceUnix + dailyForceIntervalSec
	if nextSweepUnix < nowUnix {
		nextSweepUnix = nowUnix
	}
	etaSec := nextSweepUnix - nowUnix
	if etaSec < 0 {
		etaSec = 0
	}
	coordOmittedBreakdown := len(workers) == 0 && asUint64(ws["workers_count"]) > 0
	payoutMap := workerPayoutMapFromEnv()
	displayWallet := settlementDisplayWalletAddress(a.nodeID, payoutMap)
	walletAccrued, walletSettled, walletUnpaid, accrualSource := walletAccrualFromCoordinator(ws, state.Workers, a.nodeID, a.workerID, payoutMap, a.workerProcessRunning())
	desktopWorkerID := strings.TrimSpace(a.workerID)
	if desktopWorkerID == "" {
		desktopWorkerID = "worker-kapa-pc"
	}
	var desktopWorkerAccrued float64
	if row := mapFromAny(workers[desktopWorkerID]); len(row) > 0 {
		desktopWorkerAccrued = parseAnyFloat(row["payout_hmc"])
		if desktopWorkerAccrued < 0 {
			desktopWorkerAccrued = 0
		}
	}
	coordinatorPending := walletUnpaid
	if coordinatorPending <= 0 && desktopWorkerAccrued > 0 {
		coordinatorPending = desktopWorkerAccrued
	}
	var totalAccrued, totalSettled, totalUnpaid float64
	for workerID, v := range workers {
		if coordOmittedBreakdown && workerID == "worker-active" {
			continue
		}
		row := mapFromAny(v)
		accrued := parseAnyFloat(row["payout_hmc"])
		if accrued < 0 {
			accrued = 0
		}
		settled := state.Workers[workerID].SettledHMC
		if settled < 0 {
			settled = 0
		}
		if settled > accrued {
			settled = accrued
		}
		unpaid := accrued - settled
		totalAccrued += accrued
		totalSettled += settled
		totalUnpaid += unpaid
	}
	displayUnpaid := walletUnpaid
	if strings.TrimSpace(a.nodeID) == "" {
		displayUnpaid = totalUnpaid
		accrualSource = "fleet_aggregate"
	}
	// All workers routed to one payout wallet via WORKER_PAYOUT_MAP → show fleet totals on wallet card.
	if len(payoutMap) > 0 && totalUnpaid >= 0 {
		targets := map[string]struct{}{}
		for _, t := range payoutMap {
			t = strings.TrimSpace(t)
			if strings.HasPrefix(t, "HMC-") {
				targets[strings.ToLower(t)] = struct{}{}
			}
		}
		if len(targets) == 1 {
			walletAccrued = totalAccrued
			walletSettled = totalSettled
			walletUnpaid = totalUnpaid
			displayUnpaid = totalUnpaid
			if accrualSource == "none" {
				accrualSource = "unified_payout_map"
			}
		}
	}
	settlementScanSec := int64(120)
	if v := strings.TrimSpace(os.Getenv("SETTLEMENT_TIMER_SEC")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			settlementScanSec = n
		}
	}
	nextScanUnix := nowUnix + settlementScanSec
	scanEtaSec := settlementScanSec
	workerBreakdown := map[string]any{}
	for workerID, v := range workers {
		if coordOmittedBreakdown && workerID == "worker-active" {
			continue
		}
		row := mapFromAny(v)
		accrued := parseAnyFloat(row["payout_hmc"])
		if accrued < 0 {
			accrued = 0
		}
		settled := state.Workers[workerID].SettledHMC
		if settled < 0 {
			settled = 0
		}
		if settled > accrued {
			settled = accrued
		}
		workerBreakdown[workerID] = map[string]any{
			"accrued_hmc": accrued,
			"settled_hmc": settled,
			"unpaid_hmc":  accrued - settled,
		}
	}
	writeJSON(w, map[string]any{
		"ok":                                    true,
		"source":                                base,
		"workers_count":                         len(workers),
		"total_accrued_hmc":                     totalAccrued,
		"total_settled_hmc":                     totalSettled,
		"total_unpaid_hmc":                      displayUnpaid,
		"wallet_accrued_hmc":                    walletAccrued,
		"wallet_settled_hmc":                    walletSettled,
		"wallet_unpaid_hmc":                     walletUnpaid,
		"display_wallet_address":                displayWallet,
		"desktop_worker_id":                     desktopWorkerID,
		"desktop_worker_accrued_hmc":            desktopWorkerAccrued,
		"coordinator_pending_hmc":               coordinatorPending,
		"accrual_source":                        accrualSource,
		"last_signed_miner_address":             strings.TrimSpace(asString(ws["last_signed_miner_address"])),
		"coordinator_total_payout_hmc":          parseAnyFloat(ws["total_payout_hmc"]),
		"fleet_unpaid_hmc":                      totalUnpaid,
		"min_settle_hmc":                        minSettleHMC,
		"daily_min_settle_hmc":                  dailyMinSettleHMC,
		"daily_force_interval_sec":              dailyForceIntervalSec,
		"last_force_unix":                       lastForceUnix,
		"next_daily_sweep_unix":                 nextSweepUnix,
		"daily_sweep_eta_sec":                   etaSec,
		"next_settlement_scan_unix":             nextScanUnix,
		"settlement_scan_eta_sec":               scanEtaSec,
		"settlement_scan_interval_sec":          settlementScanSec,
		"workers_breakdown":                     workerBreakdown,
		"threshold_ready":                       totalUnpaid >= minSettleHMC,
		"coordinator_workers_breakdown_omitted": coordOmittedBreakdown,
		"on_chain_payout_note":                  "UI threshold is not a bank transfer. On-chain payout runs on the VPS/chain host via settle_worker_payouts.sh (ADMIN_TOKEN + CHAIN_BASE + COORD_URL + WORKER_PAYOUT_MAP). Coordinators that omit workers{} require the updated script (synthetic single-map row) or coordinator upgrade.",
		"state_file":                            statePath,
		"updated_unix":                          nowUnix,
	})
}

// tailFileLastLines returns up to maxLines complete lines from the end of a text file,
// reading at most maxRead bytes from EOF (for large logs).
func tailFileLastLines(path string, maxLines int, maxRead int64) ([]string, error) {
	if maxLines <= 0 {
		maxLines = 80
	}
	if maxRead <= 0 {
		maxRead = 512 * 1024
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	sz := fi.Size()
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	start := int64(0)
	if sz > maxRead {
		start = sz - maxRead
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	parts := strings.Split(s, "\n")
	if start > 0 && len(parts) > 0 {
		parts = parts[1:]
	}
	for len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) > maxLines {
		parts = parts[len(parts)-maxLines:]
	}
	return parts, nil
}

// workerLogFresh returns true when the newest workerpoh log was touched within staleSec.
func workerLogFresh(logDir string, staleSec int64) bool {
	p := latestWorkerpohLogPath(logDir)
	if p == "" {
		return false
	}
	fi, err := os.Stat(p)
	if err != nil {
		return false
	}
	if staleSec <= 0 {
		return true
	}
	return time.Since(fi.ModTime()) <= time.Duration(staleSec)*time.Second
}

// workerActiveFromLog reports an external workerpoh session from a fresh log tail ghs= line.
func workerActiveFromLog(logDir string, staleSec int64) bool {
	if !workerLogFresh(logDir, staleSec) {
		return false
	}
	return parseWorkerpohMeasuredGHs(logDir) > 0
}

// parseWorkerpohGHField reads a floating GH/s value after key= on a log line (token-safe:
// avoids matching ghs= inside inst_ghs=).
func parseWorkerpohGHField(line, key string) float64 {
	for _, tok := range strings.Fields(line) {
		if !strings.HasPrefix(tok, key) {
			continue
		}
		rest := strings.TrimSpace(tok[len(key):])
		if f, err := strconv.ParseFloat(rest, 64); err == nil && f > 0 && f <= 500 {
			return f
		}
	}
	return 0
}

// parseWorkerpohMeasuredGHs reads recent submit ok lines; prefers reported ghs= (after floor)
// over raw inst_ghs= so the dashboard matches pool payout hashrate.
func parseWorkerpohMeasuredGHs(logDir string) float64 {
	p := latestWorkerpohLogPath(logDir)
	if p == "" {
		return 0
	}
	lines, err := tailFileLastLines(p, 60, 96*1024)
	if err != nil || len(lines) == 0 {
		return 0
	}
	best := 0.0
	start := len(lines) - 25
	if start < 0 {
		start = 0
	}
	for i := len(lines) - 1; i >= start; i-- {
		line := lines[i]
		if !strings.Contains(line, "submit ok") {
			continue
		}
		for _, key := range []string{"ghs=", "inst_ghs="} {
			if g := parseWorkerpohGHField(line, key); g > best {
				best = g
			}
		}
	}
	return best
}

// sanitizeWorkerIDHost maps hostname to a stable coordinator worker id (matches Windows autostart).
func sanitizeWorkerIDHost(host string) string {
	return workerid.SanitizeHostname(host)
}

func considerNewestLogPath(best string, bestMod time.Time, candidate string) (string, time.Time) {
	fi, err := os.Stat(candidate)
	if err != nil {
		return best, bestMod
	}
	if best == "" || fi.ModTime().After(bestMod) {
		return candidate, fi.ModTime()
	}
	return best, bestMod
}

// latestWorkerpohLogPath returns the newest pool worker log under logDir (workerpoh-*.log or worker_participant.log).
func latestWorkerpohLogPath(logDir string) string {
	var best string
	var bestMod time.Time
	best, bestMod = considerNewestLogPath(best, bestMod, filepath.Join(logDir, "worker_participant.log"))
	matches, err := filepath.Glob(filepath.Join(logDir, "workerpoh-*.log"))
	if err == nil {
		for _, p := range matches {
			best, bestMod = considerNewestLogPath(best, bestMod, p)
		}
	}
	return best
}

// workerIDFromLatestWorkerpohLog infers worker id from newest workerpoh-<id>-<stamp>.log filename.
func workerIDFromLatestWorkerpohLog(logDir string) string {
	p := latestWorkerpohLogPath(logDir)
	if p == "" {
		return ""
	}
	base := filepath.Base(p)
	base = strings.TrimSuffix(base, ".log")
	parts := strings.Split(base, "-")
	// workerpoh-worker-kapa-pc-20260516T115826
	if len(parts) < 4 || parts[0] != "workerpoh" {
		return ""
	}
	// last segment is timestamp; everything between workerpoh and timestamp is worker id
	last := parts[len(parts)-1]
	if len(last) >= 8 && strings.Contains(last, "T") {
		return strings.Join(parts[1:len(parts)-1], "-")
	}
	return ""
}

func (a *app) handleMiningLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	includeWorker := strings.TrimSpace(r.URL.Query().Get("include_worker")) == "1"
	// Desktop loopback: read-only worker log tail is safe without pasting admin token in the browser.
	allowWorkerTail := includeWorker && envBool("HACKME_DESKTOP_MODE", false) && requestFromLoopback(r)
	if includeWorker && !allowWorkerTail && !requireAdminAuth(w, r) {
		return
	}
	minerLines := a.miner.Logs()
	lines := append([]string(nil), minerLines...)
	logMode := "empty"
	if len(minerLines) > 0 {
		logMode = "miner"
	}
	var workerRunning bool
	var logPath string
	a.workerMu.Lock()
	if a.workerCmd != nil && a.workerCmd.Process != nil && a.workerCmd.ProcessState == nil {
		workerRunning = true
	}
	logPath = strings.TrimSpace(a.workerLogPath)
	a.workerMu.Unlock()
	tailPath := logPath
	if workerRunning {
		repoRoot := resolveWorkerRepoRoot(strings.TrimSpace(a.dataDir))
		if wp := latestWorkerpohLogPath(filepath.Join(repoRoot, "logs")); wp != "" {
			tailPath = wp
		}
	}
	if includeWorker && workerRunning && tailPath != "" {
		if tail, err := tailFileLastLines(tailPath, 120, 512*1024); err == nil && len(tail) > 0 {
			if len(lines) > 0 {
				lines = append(lines, "")
				lines = append(lines, "--- pool worker ("+filepath.Base(tailPath)+") ---")
				logMode = "both"
			} else {
				logMode = "worker"
			}
			lines = append(lines, tail...)
		}
	}
	writeJSON(w, map[string]any{
		"lines":           lines,
		"mode":            logMode,
		"worker_running":  workerRunning,
		"worker_log_path": tailPath,
		"miner_log_lines": len(minerLines),
		"total_log_lines": len(lines),
	})
}

// handleMiningLogsStream serves Server-Sent Events: event "snapshot" (JSON array of strings),
// then repeated default events with JSON object {"line":"..."} for each new miner line.
func (a *app) handleMiningLogsStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.miner.Running() {
		http.Error(w, "mining not active", http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")

	ctx := r.Context()
	ch := a.miner.SubscribeLogLines(ctx)

	lines := a.miner.Logs()
	snap, _ := json.Marshal(lines)
	fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", snap)
	flusher.Flush()

	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-ch:
			if !ok {
				return
			}
			payload, err := json.Marshal(map[string]string{"line": line})
			if err != nil {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func (a *app) handleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ctx := r.Context()
		var rows []chain.OrderTaskRow
		// Follower UX: prefer a remote /api/tasks list so the dashboard orders tab
		// matches paid orders on the network. Base URL: first HACKME_P2P_PEERS, else
		// canonicalChainBaseURL when it would not loop back to this listener.
		// Proxy runs before local SQLite so follower nodes are not blocked on chain
		// mutex/expire while P2P sync or fuzz jobs hold the store.
		// (Previously proxy ran only while the local miner was idle, which hid VPS
		// orders whenever PoW/pool search was active.) Dev-only local rows:
		// HACKME_TASKS_LIST_LOCAL_ONLY=1.
		tasksBase := strings.TrimRight(strings.TrimSpace(strings.Split(os.Getenv("HACKME_P2P_PEERS"), ",")[0]), "/")
		if tasksBase == "" {
			if b := strings.TrimRight(strings.TrimSpace(a.canonicalChainBaseURL()), "/"); b != "" && !canonicalBaseWouldLoopbackProxy(r, b) {
				tasksBase = b
			}
		}
		localTasksOnly := strings.EqualFold(strings.TrimSpace(os.Getenv("HACKME_TASKS_LIST_LOCAL_ONLY")), "1") ||
			strings.EqualFold(strings.TrimSpace(os.Getenv("HACKME_TASKS_LIST_LOCAL_ONLY")), "true")
		proxied := false
		if tasksBase != "" && !localTasksOnly {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, tasksBase+"/api/tasks", nil)
			if tok := strings.TrimSpace(extractAdminSecret(r)); tok != "" {
				req.Header.Set("X-Hackme-Admin-Token", tok)
			}
			cl := &http.Client{Timeout: 12 * time.Second}
			if resp, err := cl.Do(req); err == nil && resp != nil {
				func() {
					defer resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						var remote struct {
							Tasks []chain.OrderTaskRow `json:"tasks"`
						}
						if err := json.NewDecoder(resp.Body).Decode(&remote); err == nil {
							w.Header().Set("X-Hackme-Tasks-Source", "canonical-peer")
							rows = remote.Tasks
							proxied = true
							log.Printf("tasks: canonical proxy active base=%s tasks=%d", tasksBase, len(rows))
						} else {
							log.Printf("tasks: canonical proxy decode failed base=%s err=%v", tasksBase, err)
						}
					} else {
						log.Printf("tasks: canonical proxy http=%d base=%s", resp.StatusCode, tasksBase)
					}
				}()
			} else if err != nil {
				log.Printf("tasks: canonical proxy failed base=%s err=%v", tasksBase, err)
			}
		}
		if !proxied {
			var err error
			rows, err = a.chain.ListOrderTasks(ctx)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if rows == nil {
			rows = []chain.OrderTaskRow{}
		}
		if !tasksListShowsDetails(r) {
			for i := range rows {
				rows[i].PayerRef = ""
				rows[i].ManifestJSON = ""
			}
		}
		writeJSON(w, map[string]any{"tasks": rows})
	case http.MethodPost:
		if !a.allowRate("tasks_post:"+clientIP(r), 3) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "rate limited", "code": "rate_limited"})
			return
		}
		if !requireDeveloperTasksAuth(w, r) {
			return
		}
		logAdminAction(r, "tasks_post")
		var raw json.RawMessage
		r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		res, err := a.chain.InsertOrderTask(r.Context(), []byte(raw))
		if err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			code := http.StatusBadRequest
			if errors.Is(err, chain.ErrInsufficientBalance) {
				code = http.StatusPaymentRequired
			} else if errors.Is(err, chain.ErrOrderEscrowRateLimited) {
				code = http.StatusTooManyRequests
			}
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		receipt := a.buildOrderVerificationReceipt(res.ID, []byte(raw))
		writeJSON(w, map[string]any{
			"ok":                         true,
			"id":                         res.ID,
			"payer_ref":                  res.PayerRef,
			"prepaid_hmc":                res.PrepaidHMC,
			"order_fee_hmc":              res.OrderFeeHMC,
			"total_debit_hmc":            res.TotalDebitHMC,
			"order_burn_hmc":             res.BurnHMC,
			"balance_after":              res.BalanceAfter,
			"expires_at":                 res.ExpiresAtUnix,
			"ttl_sec":                    res.TTLSeconds,
			"signature_ed25519":          a.signer.SignHex([]byte(raw)),
			"signing_public_key_ed25519": a.signer.PublicKeyHex(),
			"verified_by":                receipt.VerifiedBy,
			"verification_receipt":       receipt,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *app) allowLoopbackAdminTxSend(r *http.Request) bool {
	if !adminAuthEnabled() {
		return false
	}
	ip, ok := parseIP(clientIP(r))
	if !ok || !ip.IsLoopback() {
		return false
	}
	return secretsEqualConstantTime(extractAdminSecret(r), adminTokenFromEnv())
}

// allowLoopbackDesktopDashboardAuth lets same-origin desktop dashboard POST transfers without
// repeating the admin header (wallet is already loopback-trusted). Does not skip canonical relay.
func (a *app) allowLoopbackDesktopDashboardAuth(r *http.Request) bool {
	return adminAuthEnabled() && envBool("HACKME_DESKTOP_MODE", false) && requestFromLoopback(r)
}

func (a *app) handleTransferSend(w http.ResponseWriter, r *http.Request) {
	if !a.allowRate("tx_send:"+clientIP(r), 20) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "rate limited", "code": "rate_limited"})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "method not allowed", "code": "method_not_allowed"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid body", "code": "invalid_body"})
		return
	}
	var tx chain.TransferTx
	if err := json.Unmarshal(raw, &tx); err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid json", "code": "invalid_json"})
		return
	}
	simpleSign := strings.TrimSpace(tx.PubKeyEd25519) == "" && strings.TrimSpace(tx.SigEd25519) == ""
	// Pre-signed wire txs are public mempool submit (exchange integrators). Simple/node signing needs admin.
	if simpleSign && !a.allowLoopbackAdminTxSend(r) && !a.allowLoopbackDesktopDashboardAuth(r) && !requireAdminAuth(w, r) {
		return
	}
	a.pruneDesktopStaleLocalTransfers(r.Context())
	if simpleSign {
		if strings.TrimSpace(tx.From) == "" && a.signer != nil {
			tx.From = strings.TrimSpace(a.signer.Address())
		}
		if tx.TimestampUnix <= 0 {
			tx.TimestampUnix = time.Now().Unix()
		}
		if code, msg := chain.ValidateTransferShape(tx); code != "" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]any{"error": msg, "code": code})
			return
		}
	}
	canonicalBase := ""
	// Loopback admin tx (settle_worker_payouts.sh) must use local SQLite nonce + mempool on the chain host.
	// Desktop wallet sends also use loopback + admin token — must still relay to canonical, not local fork.
	loopbackAdminSettle := a.allowLoopbackAdminTxSend(r) && !a.desktopCanonicalTransfersRequired()
	// Match handleWalletEarnings: in network/follower mode keep submitting to canonical even if a stale
	// local miner flag is set; otherwise POST falls through to empty SQLite and returns insufficient_balance.
	if a.shouldUseCanonicalChainAPI() && !loopbackAdminSettle {
		if base := strings.TrimRight(strings.TrimSpace(a.canonicalChainBaseURL()), "/"); base != "" && !canonicalBaseWouldLoopbackProxy(r, base) {
			canonicalBase = base
		}
	}
	if canonicalBase != "" && strings.TrimSpace(tx.PubKeyEd25519) == "" && strings.TrimSpace(tx.SigEd25519) == "" {
		from := strings.TrimSpace(tx.From)
		if from == "" && a.signer != nil {
			from = strings.TrimSpace(a.signer.Address())
			tx.From = from
		}
		if from != "" {
			nonceOK := false
			var canonNonce uint64
			if _, _, cachedNonce, ok := a.readCanonicalWalletCache(from); ok {
				tx.Nonce = cachedNonce
				nonceOK = true
			} else {
				nonceCtx, nonceCancel := context.WithTimeout(context.Background(), 8*time.Second)
				_, canonNonce, nonceOK = a.fetchCanonicalAddressState(nonceCtx, from)
				nonceCancel()
				if nonceOK {
					tx.Nonce = canonNonce
				}
			}
			if !nonceOK {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusBadGateway)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":  "could not read canonical next_nonce; refresh wallet and retry",
					"code":   "canonical_nonce_unavailable",
					"source": canonicalBase,
				})
				return
			}
		}
	}
	if strings.TrimSpace(tx.PubKeyEd25519) == "" && strings.TrimSpace(tx.SigEd25519) == "" {
		if a.signer == nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]any{
				"error": "node signer unavailable",
				"code":  "signer_unavailable",
			})
			return
		}
		from := strings.TrimSpace(tx.From)
		signerAddr := strings.TrimSpace(a.signer.Address())
		if from == "" {
			tx.From = signerAddr
		} else if !strings.EqualFold(from, signerAddr) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]any{
				"error": "simple signing allowed only for node wallet address",
				"code":  "address_pubkey_mismatch",
			})
			return
		}
		tx.PubKeyEd25519 = strings.TrimSpace(a.signer.PublicKeyHex())
		canon, err := tx.CanonicalBytes()
		if err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]any{
				"error": "invalid tx canonical payload",
				"code":  "invalid_tx_encoding",
			})
			return
		}
		tx.SigEd25519 = strings.TrimSpace(a.signer.SignHex(canon))
	}
	submitRaw, err := json.Marshal(tx)
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{
			"error": "invalid tx canonical payload",
			"code":  "invalid_tx_encoding",
		})
		return
	}
	// In follower/network mode, submit transfers to canonical node to avoid local-only
	// stale pending nonce conflicts and keep wallet UX aligned with public chain state.
	if canonicalBase != "" {
		var canonStatus int
		var canonBody []byte
		txTimeout := 8 * time.Second
		curlSec := 12
		if envBool("HACKME_DESKTOP_MODE", false) {
			txTimeout = 12 * time.Second
			curlSec = 14
		}
		forwardCtx, forwardCancel := context.WithTimeout(context.Background(), txTimeout+time.Duration(curlSec+2)*time.Second)
		defer forwardCancel()
		req, err := http.NewRequestWithContext(forwardCtx, http.MethodPost, canonicalBase+"/api/tx/send", bytes.NewReader(submitRaw))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			if relayTok := canonicalRelayAdminToken(r); relayTok != "" {
				req.Header.Set("X-Hackme-Admin-Token", relayTok)
			}
			client := &http.Client{Timeout: txTimeout}
			if resp, err := client.Do(req); err == nil && resp != nil {
				defer resp.Body.Close()
				canonBody, _ = io.ReadAll(resp.Body)
				canonStatus = resp.StatusCode
			} else if !coordinatorURLIsLoopback(canonicalBase) {
				curlCtx, cancelCurl := context.WithTimeout(context.Background(), time.Duration(curlSec+2)*time.Second)
				curlHdr := map[string]string{"Content-Type": "application/json"}
				if relayTok := canonicalRelayAdminToken(r); relayTok != "" {
					curlHdr["X-Hackme-Admin-Token"] = relayTok
				}
				st, bod, cerr := postJSONViaCurl(curlCtx, canonicalBase+"/api/tx/send", submitRaw, curlHdr)
				cancelCurl()
				if cerr == nil && st > 0 {
					canonStatus = st
					canonBody = bod
				}
			}
		}
		if canonStatus > 0 {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(canonStatus)
			_, _ = w.Write(canonBody)
			return
		}
		if a.networkModeActive() || a.desktopCanonicalTransfersRequired() {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":  "canonical chain unreachable; transfer not submitted to local fork",
				"code":   "canonical_unreachable",
				"source": canonicalBase,
			})
			return
		}
	}
	if a.desktopCanonicalTransfersRequired() {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "desktop mode requires canonical chain for transfers",
			"code":  "canonical_required",
		})
		return
	}
	a.pruneDesktopStaleLocalTransfers(r.Context())
	txHash, status, err := a.chain.SubmitTransferTx(r.Context(), tx)
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{
			"error": err.Error(),
			"code":  status,
		})
		return
	}
	if a.p2p != nil && a.p2p.Enabled() {
		a.p2p.RelayTx(r.Context(), submitRaw)
	}
	writeJSON(w, map[string]any{
		"ok":      true,
		"tx_hash": txHash,
		"status":  status,
	})
}

func (a *app) handleTransferPool(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Prefer canonical pool view when canonical base is known and local PoH is idle.
	if a.shouldUseCanonicalChainAPI() {
		if base := strings.TrimRight(strings.TrimSpace(a.canonicalChainBaseURL()), "/"); base != "" && walletCanonicalBaseUsable(base) {
			poolCtx, poolCancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer poolCancel()
			req, err := http.NewRequestWithContext(poolCtx, http.MethodGet, base+"/api/tx/pool", nil)
			if err == nil {
				if resp, err := coordinatorHTTPClient().Do(req); err == nil && resp != nil {
					defer resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						var remote any
						if err := json.NewDecoder(resp.Body).Decode(&remote); err == nil {
							writeJSON(w, remote)
							return
						}
					}
				}
			}
			curlCtx, curlCancel := context.WithTimeout(context.Background(), 6*time.Second)
			parsed, curlErr := fetchJSONViaCurl(curlCtx, base+"/api/tx/pool", nil)
			curlCancel()
			if curlErr == nil && parsed != nil {
				writeJSON(w, parsed)
				return
			}
		}
	}
	rows, err := a.chain.TransferPool(r.Context(), 500)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"txs": rows})
}

func (a *app) handleTransferByHash(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/tx/"))
	if h == "" {
		http.Error(w, "tx hash required", http.StatusBadRequest)
		return
	}
	if a.shouldUseCanonicalChainAPI() {
		if base := strings.TrimRight(strings.TrimSpace(a.canonicalChainBaseURL()), "/"); base != "" && walletCanonicalBaseUsable(base) {
			txCtx, txCancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer txCancel()
			req, err := http.NewRequestWithContext(txCtx, http.MethodGet, base+"/api/tx/"+url.PathEscape(h), nil)
			if err == nil {
				if resp, err := coordinatorHTTPClient().Do(req); err == nil && resp != nil {
					defer resp.Body.Close()
					body, _ := io.ReadAll(resp.Body)
					if resp.StatusCode == http.StatusNotFound {
						http.NotFound(w, r)
						return
					}
					if resp.StatusCode == http.StatusOK {
						w.Header().Set("Content-Type", "application/json; charset=utf-8")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write(body)
						return
					}
				}
			}
			curlCtx, curlCancel := context.WithTimeout(context.Background(), 6*time.Second)
			parsed, curlErr := fetchJSONViaCurl(curlCtx, base+"/api/tx/"+url.PathEscape(h), nil)
			curlCancel()
			if curlErr == nil && parsed != nil {
				writeJSON(w, parsed)
				return
			}
		}
	}
	row, ok, err := a.chain.TransferTxByHash(r.Context(), h)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, row)
}

func (a *app) handleTransferAddressState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	addr := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/address/"))
	if addr == "" {
		http.Error(w, "address required", http.StatusBadRequest)
		return
	}
	if a.shouldUseCanonicalChainAPI() {
		if base := strings.TrimRight(strings.TrimSpace(a.canonicalChainBaseURL()), "/"); base != "" && walletCanonicalBaseUsable(base) {
			if hmc, units, nonce, ok := a.readCanonicalWalletCache(addr); ok {
				writeJSON(w, map[string]any{
					"address":       addr,
					"balance_units": units,
					"next_nonce":    nonce,
					"balance_hmc":   hmc,
					"source":        "canonical_peer_cache",
				})
				return
			}
			peerTimeout := 6 * time.Second
			if envBool("HACKME_DESKTOP_MODE", false) {
				peerTimeout = 8 * time.Second
			}
			peerCtx, cancel := context.WithTimeout(r.Context(), peerTimeout)
			units, nonce, ok := a.fetchCanonicalAddressState(peerCtx, addr)
			cancel()
			if ok {
				hmc := float64(units) / 100_000_000.0
				a.cacheCanonicalWallet(addr, hmc, units, nonce)
				writeJSON(w, map[string]any{
					"address":       addr,
					"balance_units": units,
					"next_nonce":    nonce,
					"balance_hmc":   hmc,
					"source":        "canonical_peer",
				})
				return
			}
		}
	}
	row, err := a.chain.TransferAddressState(r.Context(), addr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, row)
}

func (a *app) handleP2PHandshake(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !a.allowP2PIngressIP(ip) {
		http.Error(w, "p2p ingress denied", http.StatusForbidden)
		return
	}
	if !a.allowRate("p2p_handshake:"+ip, 30) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	if !a.allowRate("p2p_handshake_global", 300) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.requireP2PToken(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, maxP2PHandshakeBodyBytes)
		var incoming p2p.TipSnapshot
		if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
			if err != io.EOF {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
		}
		localPolicyHash := a.chain.PolicyHash()
		if strings.TrimSpace(incoming.PolicyHash) == "" || !strings.EqualFold(strings.TrimSpace(incoming.PolicyHash), localPolicyHash) {
			http.Error(w, "p2p policy mismatch", http.StatusForbidden)
			return
		}
		// Learn discovered peers only when P2P token is configured.
		// This keeps transitive discovery closed by default in tokenless mode.
		if a.p2p != nil && p2pTokenConfigured() && a.allowDiscoveredPeerURL(incoming.AnnounceURL) {
			a.p2p.LearnDiscoveredPeer(incoming.AnnounceURL)
		}
	}
	h, tip, _ := a.chain.Tip(r.Context())
	writeJSON(w, p2p.TipSnapshot{
		NodeID:      a.nodeID,
		Height:      h,
		Tip:         tip,
		SeenAt:      time.Now().Unix(),
		AnnounceURL: strings.TrimSpace(os.Getenv("HACKME_P2P_ADVERTISE_URL")),
		PolicyHash:  a.chain.PolicyHash(),
	})
}

func (a *app) handleP2PTx(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !a.allowP2PIngressIP(ip) {
		http.Error(w, "p2p ingress denied", http.StatusForbidden)
		return
	}
	if !a.allowRate("p2p_tx:"+ip, 100) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	if !a.allowRate("p2p_tx_global", 1000) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.requireP2PToken(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxP2PTxBodyBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	var tx chain.TransferTx
	if err := json.Unmarshal(raw, &tx); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	txHash, status, err := a.chain.SubmitTransferTx(r.Context(), tx)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "code": status, "error": err.Error()})
		return
	}
	if a.p2p != nil && a.p2p.Enabled() {
		a.p2p.RelayTx(r.Context(), raw)
	}
	writeJSON(w, map[string]any{"ok": true, "tx_hash": txHash})
}

func clientIP(r *http.Request) string {
	if envBool("HACKME_TRUST_X_FORWARDED_FOR", false) {
		xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
		if xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				p := strings.TrimSpace(parts[0])
				if p != "" {
					return p
				}
			}
		}
	}
	ra := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(ra); err == nil {
		return strings.TrimSpace(host)
	}
	return ra
}

func parseIP(addr string) (netip.Addr, bool) {
	ip, err := netip.ParseAddr(strings.TrimSpace(addr))
	if err != nil {
		return netip.Addr{}, false
	}
	if ip.Is4In6() {
		ip = ip.Unmap()
	}
	return ip, true
}

func ipPrefix24String(ip netip.Addr) string {
	if !ip.IsValid() {
		return ""
	}
	if ip.Is4() {
		p := netip.PrefixFrom(ip, 24).Masked()
		return p.String()
	}
	p := netip.PrefixFrom(ip, 64).Masked()
	return p.String()
}

func ipInAnyPrefix(ip netip.Addr, list []netip.Prefix) bool {
	for _, p := range list {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func extractP2PSecret(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Hackme-P2P-Token"))
}

func (a *app) requireP2PToken(w http.ResponseWriter, r *http.Request) bool {
	tok := strings.TrimSpace(os.Getenv("HACKME_P2P_TOKEN"))
	if tok == "" {
		return true
	}
	ip := clientIP(r)
	now := time.Now().Unix()
	banKey := "p2p_token_ban:" + ip
	a.rlMu.Lock()
	if until, banned := a.rlBan[banKey]; banned && until > now {
		a.rlMu.Unlock()
		http.Error(w, "p2p token temporarily blocked", http.StatusUnauthorized)
		return false
	}
	a.rlMu.Unlock()
	got := extractP2PSecret(r)
	if !secretsEqualConstantTime(got, tok) {
		a.rlMu.Lock()
		failKey := "p2p_token_fail:" + ip
		a.p2pTokenFail[failKey]++
		failN := a.p2pTokenFail[failKey]
		if failN >= 3 {
			banDur := a.p2pIngress.tokenBanSec
			if banDur < 30 {
				banDur = 30
			}
			a.rlBan[banKey] = now + banDur
			a.p2pTokenFail[failKey] = 0
		}
		a.rlMu.Unlock()
		http.Error(w, "p2p token required", http.StatusUnauthorized)
		return false
	}
	a.rlMu.Lock()
	delete(a.p2pTokenFail, "p2p_token_fail:"+ip)
	a.rlMu.Unlock()
	return true
}

func p2pTokenConfigured() bool {
	return strings.TrimSpace(os.Getenv("HACKME_P2P_TOKEN")) != ""
}

func (a *app) allowRate(key string, limitPerSec int) bool {
	if limitPerSec <= 0 {
		return true
	}
	now := time.Now().Unix()
	a.rlMu.Lock()
	defer a.rlMu.Unlock()
	if until, banned := a.rlBan[key]; banned {
		if until > now {
			return false
		}
		delete(a.rlBan, key)
	}
	if len(a.rlHits) >= rateSlotsMaxKeys {
		for k, v := range a.rlHits {
			if now-v.sec > 2 {
				delete(a.rlHits, k)
			}
		}
		for k, until := range a.rlBan {
			if until <= now {
				delete(a.rlBan, k)
			}
		}
		if len(a.rlHits) >= rateSlotsMaxKeys {
			return false
		}
	}
	slot := a.rlHits[key]
	if slot.sec != now {
		slot.sec = now
		slot.count = 0
	}
	slot.count++
	a.rlHits[key] = slot
	if slot.count > limitPerSec*3 {
		// Temporary lockout for abusive bursts on the same bucket.
		a.rlBan[key] = now + 15
	}
	return slot.count <= limitPerSec
}

func (a *app) allowP2PIngressIP(ipRaw string) bool {
	ip, ok := parseIP(ipRaw)
	if !ok {
		return false
	}
	p := a.p2pIngress
	if len(p.denyCIDRs) > 0 && ipInAnyPrefix(ip, p.denyCIDRs) {
		return false
	}
	if len(p.allowCIDRs) > 0 && !ipInAnyPrefix(ip, p.allowCIDRs) {
		return false
	}
	return true
}

func (a *app) allowDiscoveredPeerURL(rawURL string) bool {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		return false
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return false
	}
	ip, ok := parseIP(host)
	if !ok {
		// For DNS names rely on allowlist/denylist at handshake ingress;
		// keep discovery conservative only for explicit IP peers.
		return true
	}
	if !a.allowP2PIngressIP(ip.String()) {
		return false
	}
	if a.p2p == nil {
		return true
	}
	pfx := ipPrefix24String(ip)
	if pfx == "" {
		return true
	}
	maxPer := a.p2pIngress.maxPeersPer24
	if maxPer <= 0 {
		maxPer = 16
	}
	count := 0
	for _, ps := range a.p2p.PeerSnapshots() {
		pu, err := url.Parse(strings.TrimSpace(ps.PeerURL))
		if err != nil {
			continue
		}
		pip, ok := parseIP(strings.TrimSpace(pu.Hostname()))
		if !ok {
			continue
		}
		if ipPrefix24String(pip) == pfx {
			count++
		}
	}
	return count < maxPer
}

func (a *app) tryAcquireP2PSyncHeavy() bool {
	if a == nil || a.p2pSyncHeavySem == nil {
		return true
	}
	select {
	case a.p2pSyncHeavySem <- struct{}{}:
		return true
	default:
		return false
	}
}

func (a *app) releaseP2PSyncHeavy() {
	if a == nil || a.p2pSyncHeavySem == nil {
		return
	}
	select {
	case <-a.p2pSyncHeavySem:
	default:
	}
}

// p2pBackgroundSyncIntervalSec returns 0 when background sync is disabled.
func p2pBackgroundSyncIntervalSec() int {
	s := strings.TrimSpace(os.Getenv("HACKME_P2P_BACKGROUND_SYNC_SEC"))
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	if n < 15 {
		n = 15
	}
	return n
}

// startP2PBackgroundSync periodically pulls and applies blocks on followers when state replay is enabled.
// Skipped on chain command nodes (HACKME_CHAIN_LEADER_LOCAL_POH=1) to avoid mutating the producer ledger from peers.
func (a *app) startP2PBackgroundSync(ctx context.Context) {
	interval := p2pBackgroundSyncIntervalSec()
	if interval <= 0 {
		return
	}
	if envBool("HACKME_CHAIN_LEADER_LOCAL_POH", false) {
		log.Printf("P2P background sync disabled: HACKME_CHAIN_LEADER_LOCAL_POH=1 (command node)")
		return
	}
	if !envBool("HACKME_P2P_SYNC_STATE_REPLAY_ENABLED", false) {
		log.Printf("P2P background sync disabled: set HACKME_P2P_SYNC_STATE_REPLAY_ENABLED=1 to allow apply (interval was %ds)", interval)
		return
	}
	log.Printf("P2P background sync enabled every %ds (follower mode)", interval)
	go func() {
		t := time.NewTicker(time.Duration(interval) * time.Second)
		defer t.Stop()
		var lastForkLog int64
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if a.p2p == nil || !a.p2p.Enabled() {
					continue
				}
				if !a.tryAcquireP2PSyncHeavy() {
					continue
				}
				func() {
					defer a.releaseP2PSyncHeavy()
					runCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
					defer cancel()
					out := a.p2pSyncRunExecute(runCtx, 64, 20)
					code, _ := out["code"].(string)
					if code == "fork_detected_no_reorg_v1" {
						now := time.Now().Unix()
						if now-lastForkLog > 300 {
							lastForkLog = now
							log.Printf("P2P background sync: fork detected (no reorg) — stop follower mining and reseed from canonical leader")
						}
						return
					}
					if ar, ok := out["apply"].(syncApplyResult); ok && ar.Applied > 0 {
						log.Printf("P2P background sync: applied %d blocks (to_height=%d)", ar.Applied, ar.ToHeight)
					}
				}()
			}
		}
	}()
}

func (a *app) adaptiveSyncBudgets(reqDepth uint64, reqApply int) (depth uint64, apply int, reason string) {
	depth = reqDepth
	apply = reqApply
	if depth == 0 {
		depth = 64
	}
	if apply <= 0 {
		apply = 20
	}
	s := collector.snapshot()
	cpu := s.CPUPct
	loadPerCPU := s.LoadPerCPU
	high := cpu >= 90 || (loadPerCPU >= 1.4 && loadPerCPU < 1000)
	warn := cpu >= 80 || (loadPerCPU >= 1.0 && loadPerCPU < 1000)
	if high {
		if depth > 16 {
			depth = 16
		}
		if apply > 4 {
			apply = 4
		}
		return depth, apply, "adaptive_budget_high_load"
	}
	if warn {
		if depth > 32 {
			depth = 32
		}
		if apply > 8 {
			apply = 8
		}
		return depth, apply, "adaptive_budget_warn_load"
	}
	if depth > 64 {
		depth = 64
	}
	if apply > 20 {
		apply = 20
	}
	return depth, apply, "adaptive_budget_normal"
}

func (a *app) handleP2PPeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.p2p == nil || !a.p2p.Enabled() {
		writeJSON(w, map[string]any{"enabled": false, "peers": []any{}})
		return
	}
	writeJSON(w, map[string]any{"enabled": true, "peers": a.p2p.PeerSnapshots()})
}

func (a *app) handleP2PSync(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !a.allowP2PIngressIP(ip) {
		http.Error(w, "p2p ingress denied", http.StatusForbidden)
		return
	}
	if !a.allowRate("p2p_sync_get:"+ip, 40) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	if !a.allowRate("p2p_sync_get_global", 400) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	localHeight, localTip, _ := a.chain.Tip(r.Context())
	if a.p2p == nil || !a.p2p.Enabled() {
		writeJSON(w, map[string]any{
			"enabled":      false,
			"local_height": localHeight,
			"local_tip":    localTip,
			"sync_needed":  false,
			"lag_blocks":   0,
		})
		return
	}
	hint := a.p2p.BuildSyncHint(localHeight, localTip)
	depthLimit := uint64(64)
	if v := strings.TrimSpace(r.URL.Query().Get("depth_limit")); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil && n > 0 {
			depthLimit = n
		}
	}
	depthLimit, _, budgetReason := a.adaptiveSyncBudgets(depthLimit, 0)
	if !a.tryAcquireP2PSyncHeavy() {
		http.Error(w, "sync planner busy", http.StatusTooManyRequests)
		return
	}
	defer a.releaseP2PSyncHeavy()
	preview := a.p2p.BuildLinearPullPreview(r.Context(), localHeight, localTip, depthLimit)
	blocked, blockedCode, blockedAction := syncBlockedInfo(hint.SyncNeeded, preview)
	writeJSON(w, map[string]any{
		"enabled":           true,
		"local_height":      hint.LocalHeight,
		"local_tip":         localTip,
		"max_peer_height":   hint.MaxPeerHeight,
		"lag_blocks":        hint.LagBlocks,
		"sync_needed":       hint.SyncNeeded,
		"best_peer_url":     hint.BestPeerURL,
		"best_peer_node":    hint.BestPeerNode,
		"pull_preview":      preview,
		"sync_blocked":      blocked,
		"sync_blocked_code": blockedCode,
		"sync_action":       blockedAction,
		"budget_reason":     budgetReason,
	})
}

func (a *app) handleP2PSyncPull(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !a.allowP2PIngressIP(ip) {
		http.Error(w, "p2p ingress denied", http.StatusForbidden)
		return
	}
	if !a.allowRate("p2p_sync_pull:"+ip, 10) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireAdminAuth(w, r) {
		return
	}
	if !a.tryAcquireP2PSyncHeavy() {
		writeJSON(w, map[string]any{"ok": false, "code": "sync_busy"})
		return
	}
	defer a.releaseP2PSyncHeavy()
	if a.p2p == nil || !a.p2p.Enabled() {
		writeJSON(w, map[string]any{"ok": false, "code": "p2p_disabled"})
		return
	}
	localHeight, localTip, _ := a.chain.Tip(r.Context())
	depthLimit := uint64(64)
	if v := strings.TrimSpace(r.URL.Query().Get("depth_limit")); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil && n > 0 {
			depthLimit = n
		}
	}
	depthLimit, _, budgetReason := a.adaptiveSyncBudgets(depthLimit, 0)
	preview := a.p2p.BuildLinearPullPreview(r.Context(), localHeight, localTip, depthLimit)
	if !preview.PlanReady || preview.PeerURL == "" || len(preview.Hashes) == 0 {
		writeJSON(w, map[string]any{
			"ok":      false,
			"code":    "plan_not_ready",
			"preview": preview,
		})
		return
	}
	rawBlocks, err := a.p2p.FetchBlocksByHashes(r.Context(), preview.PeerURL, preview.Hashes)
	if err != nil {
		writeJSON(w, map[string]any{
			"ok":      false,
			"code":    "pull_failed",
			"error":   "failed to fetch blocks from peer",
			"preview": preview,
		})
		return
	}
	staged := 0
	now := time.Now().Unix()
	for _, raw := range rawBlocks {
		var b block.Block
		if err := json.Unmarshal(raw, &b); err != nil {
			continue
		}
		if b.Hash == "" || b.HeaderHashHex() != b.Hash {
			continue
		}
		if err := verifySyncBlockSignature(&b); err != nil {
			continue
		}
		if _, err := a.db.ExecContext(r.Context(),
			`INSERT INTO p2p_sync_stage (block_hash, block_index, prev_hash, peer_url, block_json, fetched_at)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(block_hash) DO UPDATE SET
			   block_index=excluded.block_index,
			   prev_hash=excluded.prev_hash,
			   peer_url=excluded.peer_url,
			   block_json=excluded.block_json,
			   fetched_at=excluded.fetched_at`,
			b.Hash, b.Index, b.PrevHash, preview.PeerURL, string(raw), now); err == nil {
			staged++
		}
	}
	writeJSON(w, map[string]any{
		"ok":             true,
		"mode":           "staging_only",
		"staged_blocks":  staged,
		"requested":      len(preview.Hashes),
		"depth_limit":    preview.DepthLimit,
		"local_height":   localHeight,
		"local_tip":      localTip,
		"peer_url":       preview.PeerURL,
		"planned_to":     preview.PlannedTo,
		"planned_reason": preview.Reason,
		"budget_reason":  budgetReason,
	})
}

func (a *app) handleP2PSyncStage(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !a.allowP2PIngressIP(ip) {
		http.Error(w, "p2p ingress denied", http.StatusForbidden)
		return
	}
	if !a.allowRate("p2p_sync_stage:"+ip, 30) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := 100
	if s := strings.TrimSpace(r.URL.Query().Get("limit")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT block_index, block_hash, prev_hash, peer_url, fetched_at
		   FROM p2p_sync_stage
		  ORDER BY block_index ASC
		  LIMIT ?`, limit)
	if err != nil {
		http.Error(w, "stage query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type item struct {
		Index     uint64 `json:"index"`
		Hash      string `json:"hash"`
		PrevHash  string `json:"prev_hash"`
		PeerURL   string `json:"peer_url"`
		FetchedAt int64  `json:"fetched_at"`
	}
	out := make([]item, 0, limit)
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.Index, &it.Hash, &it.PrevHash, &it.PeerURL, &it.FetchedAt); err != nil {
			http.Error(w, "stage decode failed", http.StatusInternalServerError)
			return
		}
		out = append(out, it)
	}
	writeJSON(w, map[string]any{"staged_blocks": out, "count": len(out)})
}

func (a *app) handleP2PSyncApply(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !a.allowP2PIngressIP(ip) {
		http.Error(w, "p2p ingress denied", http.StatusForbidden)
		return
	}
	if !a.allowRate("p2p_sync_apply:"+ip, 10) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireAdminAuth(w, r) {
		return
	}
	if !a.tryAcquireP2PSyncHeavy() {
		writeJSON(w, map[string]any{"ok": false, "code": "sync_busy"})
		return
	}
	defer a.releaseP2PSyncHeavy()
	maxApply := 20
	if s := strings.TrimSpace(r.URL.Query().Get("max_apply")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 200 {
			maxApply = n
		}
	}
	_, maxApply, budgetReason := a.adaptiveSyncBudgets(0, maxApply)
	if !envBool("HACKME_P2P_SYNC_STATE_REPLAY_ENABLED", false) {
		writeJSON(w, map[string]any{
			"ok":            false,
			"code":          "sync_apply_disabled_no_state_replay_v1",
			"reason":        "sync apply is disabled by default to prevent chain/state drift",
			"action":        "enable HACKME_P2P_SYNC_STATE_REPLAY_ENABLED=1 only in controlled environments",
			"max_apply":     maxApply,
			"budget_reason": budgetReason,
		})
		return
	}
	res, err := a.applyStagedLinearTail(r.Context(), maxApply)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "code": "apply_failed", "error": "failed to apply staged blocks"})
		return
	}
	writeJSON(w, map[string]any{
		"ok":             res.OK,
		"mode":           "strict_linear_apply_v1",
		"applied":        res.Applied,
		"max_apply":      maxApply,
		"from_height":    res.FromHeight,
		"to_height":      res.ToHeight,
		"new_tip":        res.NewTip,
		"reason":         res.Reason,
		"local_height":   res.LocalHeight,
		"local_tip":      res.LocalTip,
		"candidate_idx":  res.CandidateIdx,
		"candidate_prev": res.CandidatePrev,
		"budget_reason":  budgetReason,
	})
}

// p2pSyncRunExecute pulls contiguous blocks from the best peer, stages them, and applies up to maxApply.
// Caller must hold the P2P sync heavy semaphore when contention matters.
func (a *app) p2pSyncRunExecute(ctx context.Context, depthLimit uint64, maxApply int) map[string]any {
	if a.p2p == nil || !a.p2p.Enabled() {
		return map[string]any{"ok": false, "code": "p2p_disabled"}
	}
	depthLimit, maxApply, budgetReason := a.adaptiveSyncBudgets(depthLimit, maxApply)
	if !envBool("HACKME_P2P_SYNC_STATE_REPLAY_ENABLED", false) {
		return map[string]any{
			"ok":            false,
			"code":          "sync_apply_disabled_no_state_replay_v1",
			"reason":        "sync run apply step is disabled by default to prevent chain/state drift",
			"action":        "enable HACKME_P2P_SYNC_STATE_REPLAY_ENABLED=1 only in controlled environments",
			"max_apply":     maxApply,
			"budget_reason": budgetReason,
		}
	}
	localHeight, localTip, _ := a.chain.Tip(ctx)
	preview := a.p2p.BuildLinearPullPreview(ctx, localHeight, localTip, depthLimit)
	if preview.Reason == "no_direct_tail_match" {
		return map[string]any{
			"ok":      false,
			"code":    "fork_detected_no_reorg_v1",
			"action":  "stop_mining_on_follower_and_reseed_db_from_leader",
			"preview": preview,
		}
	}
	if preview.PlanReady && preview.PeerURL != "" && len(preview.Hashes) > 0 {
		rawBlocks, err := a.p2p.FetchBlocksByHashes(ctx, preview.PeerURL, preview.Hashes)
		if err != nil {
			return map[string]any{"ok": false, "code": "pull_failed", "error": "failed to fetch blocks from peer", "preview": preview}
		}
		now := time.Now().Unix()
		for _, raw := range rawBlocks {
			var b block.Block
			if err := json.Unmarshal(raw, &b); err != nil {
				continue
			}
			if b.Hash == "" || b.HeaderHashHex() != b.Hash {
				continue
			}
			if err := verifySyncBlockSignature(&b); err != nil {
				continue
			}
			_, _ = a.db.ExecContext(ctx,
				`INSERT INTO p2p_sync_stage (block_hash, block_index, prev_hash, peer_url, block_json, fetched_at)
				 VALUES (?, ?, ?, ?, ?, ?)
				 ON CONFLICT(block_hash) DO UPDATE SET
				   block_index=excluded.block_index,
				   prev_hash=excluded.prev_hash,
				   peer_url=excluded.peer_url,
				   block_json=excluded.block_json,
				   fetched_at=excluded.fetched_at`,
				b.Hash, b.Index, b.PrevHash, preview.PeerURL, string(raw), now)
		}
	}
	res, err := a.applyStagedLinearTail(ctx, maxApply)
	if err != nil {
		return map[string]any{"ok": false, "code": "apply_failed", "error": "failed to apply staged blocks", "preview": preview}
	}
	return map[string]any{
		"ok":            res.OK,
		"mode":          "sync_run_v1",
		"preview":       preview,
		"apply":         res,
		"max_apply":     maxApply,
		"budget_reason": budgetReason,
	}
}

func (a *app) handleP2PSyncRun(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !a.allowP2PIngressIP(ip) {
		http.Error(w, "p2p ingress denied", http.StatusForbidden)
		return
	}
	if !a.allowRate("p2p_sync_run:"+ip, 10) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireAdminAuth(w, r) {
		return
	}
	if !a.tryAcquireP2PSyncHeavy() {
		writeJSON(w, map[string]any{"ok": false, "code": "sync_busy"})
		return
	}
	defer a.releaseP2PSyncHeavy()
	if a.p2p == nil || !a.p2p.Enabled() {
		writeJSON(w, map[string]any{"ok": false, "code": "p2p_disabled"})
		return
	}
	depthLimit := uint64(64)
	if v := strings.TrimSpace(r.URL.Query().Get("depth_limit")); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil && n > 0 {
			depthLimit = n
		}
	}
	maxApply := 20
	if s := strings.TrimSpace(r.URL.Query().Get("max_apply")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 200 {
			maxApply = n
		}
	}
	writeJSON(w, a.p2pSyncRunExecute(r.Context(), depthLimit, maxApply))
}

func syncBlockedInfo(syncNeeded bool, preview p2p.SyncPullPreview) (blocked bool, code string, action string) {
	if !syncNeeded {
		return false, "", ""
	}
	switch strings.TrimSpace(preview.Reason) {
	case "no_direct_tail_match":
		return true, "fork_detected_no_reorg_v1", "reseed_follower_db_from_leader"
	case "no_lag_or_no_healthy_peer":
		// Soft-wait mode: follower should keep waiting for a healthy peer snapshot
		// instead of treating it as a hard blocked state.
		return false, "sync_waiting_peer_freshness", "wait_and_retry_sync"
	default:
		return false, "", ""
	}
}

// attachWalletEarningsSyncMeta adds diagnostics when earnings come from local SQLite while the UI expects canonical overlay.
func (a *app) attachWalletEarningsSyncMeta(ctx context.Context, out map[string]any, canonAttempted, canonOK bool) {
	if out == nil {
		return
	}
	if canonAttempted && !canonOK {
		if walletEarningsDataTxCount(out) > 0 {
			// Command node ledger (or caught-up follower): local tx_history is authoritative even when
			// HTTPS self-fetch to HACKME_CANONICAL_CHAIN_URL failed.
			out["source"] = "canonical_ledger"
		} else {
			out["canonical_earnings_unavailable"] = true
		}
	}
	if a == nil || a.p2p == nil || !a.p2p.Enabled() {
		return
	}
	localHeight, localTip, err := a.chain.Tip(ctx)
	if err != nil {
		return
	}
	hint := a.p2p.BuildSyncHint(localHeight, localTip)
	preview := a.p2p.BuildLinearPullPreview(ctx, localHeight, localTip, 64)
	blocked, code, action := syncBlockedInfo(hint.SyncNeeded, preview)
	out["fork_hint"] = map[string]any{
		"sync_needed":       hint.SyncNeeded,
		"sync_blocked":      blocked,
		"sync_blocked_code": code,
		"sync_action":       action,
		"lag_blocks":        hint.LagBlocks,
		"local_height":      hint.LocalHeight,
		"max_peer_height":   hint.MaxPeerHeight,
		"best_peer_url":     hint.BestPeerURL,
		"pull_preview_hint": preview.Reason,
	}
}

type syncApplyResult struct {
	OK            bool   `json:"ok"`
	Applied       int    `json:"applied"`
	FromHeight    uint64 `json:"from_height"`
	ToHeight      uint64 `json:"to_height"`
	NewTip        string `json:"new_tip,omitempty"`
	Reason        string `json:"reason,omitempty"`
	LocalHeight   uint64 `json:"local_height"`
	LocalTip      string `json:"local_tip,omitempty"`
	CandidateIdx  uint64 `json:"candidate_idx,omitempty"`
	CandidatePrev string `json:"candidate_prev,omitempty"`
}

func (a *app) applyStagedLinearTail(ctx context.Context, maxApply int) (syncApplyResult, error) {
	localHeight, localTip, _ := a.chain.Tip(ctx)
	res := syncApplyResult{OK: true, Applied: 0, LocalHeight: localHeight, LocalTip: localTip}
	rows, err := a.db.QueryContext(ctx,
		`SELECT block_index, block_hash, prev_hash, block_json
		   FROM p2p_sync_stage
		  ORDER BY block_index ASC
		  LIMIT ?`, maxApply)
	if err != nil {
		return res, err
	}
	defer rows.Close()
	type staged struct {
		Index uint64
		Hash  string
		Prev  string
		Raw   string
	}
	candidates := make([]staged, 0, maxApply)
	for rows.Next() {
		var s staged
		if err := rows.Scan(&s.Index, &s.Hash, &s.Prev, &s.Raw); err != nil {
			return res, err
		}
		candidates = append(candidates, s)
	}
	// Drop stale staged rows that are already behind/equal to current local height.
	// They can appear after follower reseed and otherwise trigger false continuity_break.
	if len(candidates) > 0 {
		for _, c := range candidates {
			if c.Index <= localHeight {
				_, _ = a.db.ExecContext(ctx, `DELETE FROM p2p_sync_stage WHERE block_hash = ?`, c.Hash)
			}
		}
		fresh := make([]staged, 0, len(candidates))
		for _, c := range candidates {
			if c.Index > localHeight {
				fresh = append(fresh, c)
			}
		}
		candidates = fresh
	}
	if len(candidates) == 0 {
		res.Reason = "empty_stage"
		return res, nil
	}
	// If stage contains mixed tails from older peers, skip to the first candidate
	// that exactly continues current local tip.
	if !(candidates[0].Index == localHeight+1 && strings.TrimSpace(candidates[0].Prev) == strings.TrimSpace(localTip)) {
		start := -1
		for i, c := range candidates {
			if c.Index == localHeight+1 && strings.TrimSpace(c.Prev) == strings.TrimSpace(localTip) {
				start = i
				break
			}
		}
		if start > 0 {
			for _, s := range candidates[:start] {
				_, _ = a.db.ExecContext(ctx, `DELETE FROM p2p_sync_stage WHERE block_hash = ?`, s.Hash)
			}
			candidates = candidates[start:]
		}
	}
	applyList := make([]staged, 0, len(candidates))
	curTip := localTip
	curHeight := localHeight
	for i, c := range candidates {
		// Bootstrap case for followers with empty DB: allow applying genesis
		// block #0 as the first staged block.
		if curHeight == 0 && strings.TrimSpace(curTip) == "" && c.Index == 0 {
			if strings.TrimSpace(c.Prev) != block.ZeroPrevHash {
				res.OK = false
				res.Reason = "invalid_genesis_prev_hash"
				res.CandidateIdx = c.Index
				res.CandidatePrev = c.Prev
				break
			}
			var b block.Block
			if err := json.Unmarshal([]byte(c.Raw), &b); err != nil {
				res.OK = false
				res.Reason = "invalid_staged_json"
				return res, nil
			}
			if b.Hash == "" || b.HeaderHashHex() != b.Hash || b.Hash != c.Hash {
				res.OK = false
				res.Reason = "invalid_staged_block_hash"
				return res, nil
			}
			if err := verifySyncBlockSignature(&b); err != nil {
				res.OK = false
				res.Reason = "invalid_staged_block_signature"
				return res, nil
			}
			applyList = append(applyList, c)
			curTip = c.Hash
			curHeight = c.Index
			continue
		}
		if c.Index != curHeight+1 || strings.TrimSpace(c.Prev) != strings.TrimSpace(curTip) {
			if i == 0 {
				res.OK = false
				res.Reason = "continuity_break"
				res.CandidateIdx = c.Index
				res.CandidatePrev = c.Prev
			}
			break
		}
		var b block.Block
		if err := json.Unmarshal([]byte(c.Raw), &b); err != nil {
			res.OK = false
			res.Reason = "invalid_staged_json"
			return res, nil
		}
		if b.Hash == "" || b.HeaderHashHex() != b.Hash || b.Hash != c.Hash {
			res.OK = false
			res.Reason = "invalid_staged_block_hash"
			return res, nil
		}
		if err := verifySyncBlockSignature(&b); err != nil {
			res.OK = false
			res.Reason = "invalid_staged_block_signature"
			return res, nil
		}
		applyList = append(applyList, c)
		curTip = c.Hash
		curHeight = c.Index
	}
	if len(applyList) == 0 {
		if res.Reason == "" {
			res.Reason = "no_contiguous_tail"
		}
		return res, nil
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer func() { _ = tx.Rollback() }()
	for _, c := range applyList {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO blocks (block_index, hash, prev_hash, json) VALUES (?,?,?,?)`,
			c.Index, c.Hash, c.Prev, c.Raw); err != nil {
			return res, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM p2p_sync_stage WHERE block_hash = ?`, c.Hash); err != nil {
			return res, err
		}
	}
	lastHash := applyList[len(applyList)-1].Hash
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES ('tip_hash', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		lastHash); err != nil {
		return res, err
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	res.Applied = len(applyList)
	res.FromHeight = localHeight + 1
	res.ToHeight = applyList[len(applyList)-1].Index
	res.NewTip = lastHash
	res.Reason = "ok"
	return res, nil
}

func verifySyncBlockSignature(b *block.Block) error {
	if b == nil {
		return errors.New("nil block")
	}
	// Allow legacy unsigned blocks; but if signature fields are present,
	// they must be complete and cryptographically valid.
	if strings.TrimSpace(b.MinerPubKey) == "" && strings.TrimSpace(b.MinerSig) == "" {
		return nil
	}
	alg := strings.TrimSpace(strings.ToLower(b.MinerSigAlg))
	if alg == "" {
		alg = chain.TransferSigAlgEd25519
	}
	if alg != chain.TransferSigAlgEd25519 {
		return errors.New("unsupported miner_sig_alg")
	}
	if strings.TrimSpace(b.MinerPubKey) == "" || strings.TrimSpace(b.MinerSig) == "" {
		return errors.New("incomplete signature fields")
	}
	pub, err := hex.DecodeString(strings.TrimSpace(b.MinerPubKey))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("invalid miner pubkey")
	}
	sig, err := hex.DecodeString(strings.TrimSpace(b.MinerSig))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("invalid miner signature")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(b.Hash), sig) {
		return errors.New("signature verify failed")
	}
	return nil
}

func (a *app) handleMiningDevices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s := collector.snapshot()
		ms := a.miner.Stats()
		devs := make([]map[string]any, 0, len(ms.GPUPoHDevices))
		profile := a.loadMiningProfile(r.Context())
		activeGPU := 0
		nvRows := queryNVIDIAMulti()
		nvNameByIndex := make(map[int]string, len(nvRows))
		for _, nv := range nvRows {
			if n := strings.TrimSpace(nv.Name); n != "" {
				nvNameByIndex[nv.Index] = n
			}
		}
		amdRows := queryAMDGPUMulti()
		amdNameByIndex := make(map[int]string, len(amdRows))
		for _, am := range amdRows {
			if n := strings.TrimSpace(am.Name); n != "" {
				amdNameByIndex[am.Index] = n
			}
		}
		seen := make(map[string]struct{}, len(ms.GPUPoHDevices))
		for _, d := range ms.GPUPoHDevices {
			alias := a.loadGPUAlias(r.Context(), d.Backend, d.Index)
			enabled := a.loadGPUEnabled(r.Context(), d.Backend, d.Index)
			priority := a.loadGPUPriority(r.Context(), d.Backend, d.Index)
			if enabled {
				activeGPU++
			}
			name := strings.TrimSpace(d.Name)
			if name == "" {
				if n, ok := nvNameByIndex[d.Index]; ok {
					name = n
				}
			}
			if name == "" {
				if n, ok := amdNameByIndex[d.Index]; ok {
					name = n
				}
			}
			key := strings.TrimSpace(d.Backend) + ":" + strconv.Itoa(d.Index)
			seen[key] = struct{}{}
			devs = append(devs, map[string]any{
				"index":         d.Index,
				"backend":       d.Backend,
				"name":          name,
				"default_label": d.Label,
				"alias":         alias,
				"enabled":       enabled,
				"priority":      priority,
				"temp_c":        d.TempC,
				"display_name": func() string {
					if alias != "" {
						return alias
					}
					if name != "" {
						return name
					}
					return d.Label
				}(),
			})
		}
		// Fallback: when GPU mining is idle/unavailable, still expose detected NVIDIA cards
		// so UI can show real GPU model names in Mining devices.
		for _, nv := range nvRows {
			key := "cuda:" + strconv.Itoa(nv.Index)
			if _, ok := seen[key]; ok {
				continue
			}
			name := strings.TrimSpace(nv.Name)
			if name == "" {
				name = "NVIDIA GPU"
			}
			alias := a.loadGPUAlias(r.Context(), "cuda", nv.Index)
			enabled := a.loadGPUEnabled(r.Context(), "cuda", nv.Index)
			priority := a.loadGPUPriority(r.Context(), "cuda", nv.Index)
			if enabled {
				activeGPU++
			}
			devs = append(devs, map[string]any{
				"index":         nv.Index,
				"backend":       "cuda",
				"name":          name,
				"default_label": fmt.Sprintf("GPU #%d [CUDA]", nv.Index),
				"alias":         alias,
				"enabled":       enabled,
				"priority":      priority,
				"temp_c":        nv.TempC,
				"display_name": func() string {
					if alias != "" {
						return alias
					}
					return name
				}(),
			})
		}
		// Fallback: amdgpu sysfs (OpenCL mining) when idle or for UI inventory.
		for _, am := range amdRows {
			key := "opencl:" + strconv.Itoa(am.Index)
			if _, ok := seen[key]; ok {
				continue
			}
			name := strings.TrimSpace(am.Name)
			if name == "" {
				name = "AMD GPU"
			}
			alias := a.loadGPUAlias(r.Context(), "opencl", am.Index)
			enabled := a.loadGPUEnabled(r.Context(), "opencl", am.Index)
			priority := a.loadGPUPriority(r.Context(), "opencl", am.Index)
			if enabled {
				activeGPU++
			}
			devs = append(devs, map[string]any{
				"index":         am.Index,
				"backend":       "opencl",
				"name":          name,
				"default_label": fmt.Sprintf("GPU #%d [OpenCL]", am.Index),
				"alias":         alias,
				"enabled":       enabled,
				"priority":      priority,
				"temp_c":        am.TempC,
				"display_name": func() string {
					if alias != "" {
						return alias
					}
					return name
				}(),
			})
		}
		var poolWorkerRunning bool
		var poolWorkerSessionSec float64
		a.workerMu.Lock()
		if a.workerCmd != nil && a.workerCmd.Process != nil && a.workerCmd.ProcessState == nil {
			poolWorkerRunning = true
			if a.workerStartedAt > 0 {
				poolWorkerSessionSec = float64(time.Now().Unix() - a.workerStartedAt)
				if poolWorkerSessionSec < 0 {
					poolWorkerSessionSec = 0
				}
			}
		}
		a.workerMu.Unlock()
		writeJSON(w, map[string]any{
			"cpu_model":       s.CPUModel,
			"cpu_alias":       a.loadGPUAlias(r.Context(), "cpu", 0),
			"cpu_enabled":     a.loadGPUEnabled(r.Context(), "cpu", 0),
			"cpu_priority":    a.loadGPUPriority(r.Context(), "cpu", 0),
			"session_seconds": ms.SessionSeconds,
			// Local WASM PoH session is often 0 when follower uses coordinator worker; expose pool child uptime for UI.
			"pool_worker_running":         poolWorkerRunning,
			"pool_worker_session_seconds": math.Round(poolWorkerSessionSec*100) / 100,
			"gpu_count":                   len(devs),
			"gpu_active":                  activeGPU,
			"profile_mode":                profile,
			"gpu_devices":                 devs,
		})
	case http.MethodPost:
		if !requireAdminAuth(w, r) {
			return
		}
		logAdminAction(r, "mining_devices_post")
		var body struct {
			Backend  string `json:"backend"`
			Index    int    `json:"index"`
			Alias    string `json:"alias"`
			Profile  string `json:"profile_mode"`
			Enabled  *bool  `json:"enabled"`
			Priority *int   `json:"priority"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.Profile) != "" {
			if err := a.upsertMiningProfile(r.Context(), body.Profile); err != nil {
				http.Error(w, "invalid profile_mode", http.StatusBadRequest)
				return
			}
		}
		if strings.TrimSpace(body.Backend) != "" && body.Index >= 0 {
			if len(strings.TrimSpace(body.Alias)) > 80 {
				http.Error(w, "alias too long", http.StatusBadRequest)
				return
			}
			if err := a.upsertGPUAlias(r.Context(), body.Backend, body.Index, body.Alias); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if body.Enabled != nil {
				if err := a.upsertGPUEnabled(r.Context(), body.Backend, body.Index, *body.Enabled); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			if body.Priority != nil {
				if err := a.upsertGPUPriority(r.Context(), body.Backend, body.Index, *body.Priority); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
		} else if strings.TrimSpace(body.Profile) == "" {
			http.Error(w, "backend/index or profile_mode required", http.StatusBadRequest)
			return
		}
		a.refreshMinerDevicePolicy(r.Context())
		writeJSON(w, map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}
