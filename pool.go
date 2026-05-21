package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"hackme/internal/chain"
	"hackme/internal/lanpool"
	"hackme/internal/store"
)

// Default public command authority (canonical + inferred pool coordinator + P2P peer).
// HTTPS without port → coordinator at /pool/coordinator (see inferCoordinatorURLFromCommandBase).
const (
	defaultPublicStagingCommandBase     = "https://hackme.tech"
	defaultPublicStagingCoordinatorBase = "https://hackme.tech/pool/coordinator"
)

// PublicStagingJoinEnvExport returns one-line shell exports to attach this node to the public pool + canonical + P2P.
// Uses HACKME_PUBLIC_AUTHORITY_BASE so canonical and coordinator URLs are inferred consistently at runtime.
func PublicStagingJoinEnvExport() string {
	base := strings.TrimRight(defaultPublicStagingCommandBase, "/")
	return fmt.Sprintf(
		"export HACKME_PUBLIC_AUTHORITY_BASE=%s HACKME_P2P_PEERS=%s",
		base,
		base,
	)
}

// PublicAuthorityEnvOneLiner returns a minimal export when command node and coordinator follow the standard VPS layout:
// explicit port → coordinator on port+1; HTTPS without port → coordinator at /pool/coordinator on same host.
func PublicAuthorityEnvOneLiner(commandBase string) string {
	commandBase = strings.TrimSpace(commandBase)
	if commandBase == "" {
		return ""
	}
	return "export HACKME_PUBLIC_AUTHORITY_BASE=" + strings.TrimRight(commandBase, "/")
}

// applyPublicAuthorityBaseEnv sets HACKME_CANONICAL_CHAIN_URL and HACKME_POOL_COORDINATOR_URL from
// HACKME_PUBLIC_AUTHORITY_BASE when unset — single-VPS is the source of truth for height, blocks, pool API.
// Optional HACKME_PUBLIC_COORDINATOR_URL overrides coordinator when POOL_COORDINATOR is unset.
func applyPublicAuthorityBaseEnv() {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_PUBLIC_AUTHORITY_BASE")), "/")
	if base == "" {
		return
	}
	if strings.TrimSpace(os.Getenv("HACKME_CANONICAL_CHAIN_URL")) == "" {
		_ = os.Setenv("HACKME_CANONICAL_CHAIN_URL", base)
		log.Printf("HACKME_PUBLIC_AUTHORITY_BASE: canonical command node = %s (HACKME_CANONICAL_CHAIN_URL)", base)
	}
	if strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_URL")) != "" {
		return
	}
	if co := strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_PUBLIC_COORDINATOR_URL")), "/"); co != "" {
		_ = os.Setenv("HACKME_POOL_COORDINATOR_URL", co)
		log.Printf("HACKME_PUBLIC_AUTHORITY_BASE: coordinator = %s (HACKME_PUBLIC_COORDINATOR_URL)", co)
		return
	}
	if inf := inferCoordinatorURLFromCommandBase(base); inf != "" {
		_ = os.Setenv("HACKME_POOL_COORDINATOR_URL", inf)
		log.Printf("HACKME_PUBLIC_AUTHORITY_BASE: coordinator = %s (inferred)", inf)
	} else {
		log.Printf("HACKME_PUBLIC_AUTHORITY_BASE: set HACKME_POOL_COORDINATOR_URL or HACKME_PUBLIC_COORDINATOR_URL (could not infer from %s)", base)
	}
}

// inferCoordinatorURLFromCommandBase maps command-node API base to coordinator URL:
// - host:port with numeric port → port+1 (e.g. :18080 → :18081);
// - scheme://host without port → {scheme}://{host}/pool/coordinator (typical TLS reverse-proxy).
func inferCoordinatorURLFromCommandBase(cmdBase string) string {
	cmdBase = strings.TrimRight(strings.TrimSpace(cmdBase), "/")
	if cmdBase == "" {
		return ""
	}
	u, err := neturl.Parse(cmdBase)
	if err != nil || strings.TrimSpace(u.Hostname()) == "" {
		return ""
	}
	portStr := strings.TrimSpace(u.Port())
	if portStr != "" {
		pn, err := strconv.Atoi(portStr)
		if err == nil && pn > 0 && pn < 65534 {
			u2 := *u
			u2.Host = net.JoinHostPort(strings.TrimSpace(u.Hostname()), strconv.Itoa(pn+1))
			return strings.TrimRight(u2.String(), "/")
		}
	}
	origin := u.Scheme + "://" + u.Host
	return strings.TrimRight(origin, "/") + "/pool/coordinator"
}

func asUint64(v any) uint64 {
	switch n := v.(type) {
	case uint64:
		return n
	case int64:
		if n < 0 {
			return 0
		}
		return uint64(n)
	case int:
		if n < 0 {
			return 0
		}
		return uint64(n)
	case float64:
		if n < 0 {
			return 0
		}
		return uint64(n)
	default:
		return 0
	}
}

func asFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case uint64:
		return float64(n)
	case int64:
		return float64(n)
	case int:
		return float64(n)
	default:
		return 0
	}
}

func asInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case uint64:
		return int64(n)
	default:
		return 0
	}
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

// MiningRigMetrics is JSON for /api/metrics (alias to lan pool row type).
type MiningRigMetrics = lanpool.MetricsRow

type pushWorkBody = lanpool.PushWorkBody

const (
	coordinatorForwardHeader = "X-Hackme-Pool-Forwarded"
	staleRigPruneAge         = 30 * time.Minute
	staleRigPruneEvery       = 60 * time.Second
)

var (
	workStatsCacheMu sync.RWMutex
	workStatsCache   map[string]any
	workStatsCacheTS int64
	coordHTTPOnce    sync.Once
	coordHTTPClient  *http.Client
)

func coordinatorHTTPClient() *http.Client {
	coordHTTPOnce.Do(func() {
		tr := &http.Transport{
			// Direct egress: local dev environments often have stale proxy env vars
			// that break coordinator calls and keep work stats permanently stale.
			Proxy: nil,
			DialContext: (&net.Dialer{
				Timeout:   3 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			// HTTP/2 to same-host coordinator has caused stall storms under load; HTTP/1.1 is enough on loopback.
			ForceAttemptHTTP2:     false,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   4 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
		coordHTTPClient = &http.Client{
			Timeout:   30 * time.Second,
			Transport: tr,
		}
	})
	return coordHTTPClient
}

// coordinatorURLIsLoopback reports whether the pool coordinator is on this machine.
// Curl subprocess fallback is disabled for loopback: under public traffic it can fork-bomb the VPS.
func coordinatorURLIsLoopback(base string) bool {
	u, err := neturl.Parse(strings.TrimSpace(base))
	if err != nil {
		return false
	}
	h := strings.ToLower(strings.TrimSpace(u.Hostname()))
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

var coordCurlFallbackSem = make(chan struct{}, 3)

func fetchJSONViaCurl(ctx context.Context, u string, headers map[string]string) (map[string]any, error) {
	return fetchJSONViaCurlMax(ctx, u, headers, 8)
}

func fetchJSONViaCurlMax(ctx context.Context, u string, headers map[string]string, maxTimeSec int) (map[string]any, error) {
	if maxTimeSec <= 0 {
		maxTimeSec = 8
	}
	select {
	case coordCurlFallbackSem <- struct{}{}:
		defer func() { <-coordCurlFallbackSem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, fmt.Errorf("curl fallback busy (too many concurrent coordinator fallbacks)")
	}
	args := []string{"--max-time", strconv.Itoa(maxTimeSec), "-sS"}
	for k, v := range headers {
		if strings.TrimSpace(k) == "" {
			continue
		}
		args = append(args, "-H", k+": "+v)
	}
	args = append(args, u)
	cmd := exec.CommandContext(ctx, "curl", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

// fetchJSONViaCurlDesktop is a wallet-only curl path that bypasses coordCurlFallbackSem saturation.
func fetchJSONViaCurlDesktop(u string, maxTimeSec int) (map[string]any, error) {
	if maxTimeSec <= 0 {
		maxTimeSec = 12
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(maxTimeSec+2)*time.Second)
	defer cancel()
	args := []string{"--max-time", strconv.Itoa(maxTimeSec), "-sS", u}
	cmd := exec.CommandContext(ctx, "curl", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

// postJSONViaCurl POSTs JSON to URL via curl (TLS subprocess fallback when net/http to public canonical fails).
func postJSONViaCurl(ctx context.Context, targetURL string, jsonBody []byte) (statusCode int, respBody []byte, err error) {
	if coordinatorURLIsLoopback(targetURL) {
		return 0, nil, fmt.Errorf("curl post fallback disabled on loopback")
	}
	select {
	case coordCurlFallbackSem <- struct{}{}:
		defer func() { <-coordCurlFallbackSem }()
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	default:
		return 0, nil, fmt.Errorf("curl fallback busy (too many concurrent coordinator fallbacks)")
	}
	tmp, err := os.CreateTemp("", "hackme-txsend-*.json")
	if err != nil {
		return 0, nil, err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(jsonBody); err != nil {
		_ = tmp.Close()
		return 0, nil, err
	}
	if err := tmp.Close(); err != nil {
		return 0, nil, err
	}
	cmd := exec.CommandContext(ctx, "curl", "--max-time", "12", "-sS", "-X", "POST",
		"-H", "Content-Type: application/json",
		"--data-binary", "@"+tmpPath,
		"-w", "\n%{http_code}",
		targetURL,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0, nil, err
	}
	idx := bytes.LastIndexByte(out, '\n')
	if idx < 0 || idx >= len(out)-1 {
		return 0, nil, fmt.Errorf("curl: unexpected output shape")
	}
	codeStr := strings.TrimSpace(string(out[idx+1:]))
	sc, err := strconv.Atoi(codeStr)
	if err != nil {
		return 0, nil, fmt.Errorf("curl: bad http code %q", codeStr)
	}
	return sc, out[:idx], nil
}

func buildMiningRigsForMetrics(nodeID string, miningRunning bool, attemptsPerSec float64, reg *lanpool.Registry, gpuRigs []MiningRigMetrics) []MiningRigMetrics {
	var out []MiningRigMetrics
	if len(gpuRigs) > 0 {
		out = append(out, gpuRigs...)
	} else if miningRunning {
		gh := attemptsPerSec / 1e6
		if gh < 0.01 {
			gh = attemptsPerSec / 1e3
		}
		out = append(out, MiningRigMetrics{
			WorkerID:     nodeID,
			Name:         "LOCAL_COMMAND_NODE",
			HashrateGHS:  gh,
			LastSeenUnix: 0,
			IP:           "127.0.0.1",
			Online:       true,
			Source:       "local",
		})
	}
	out = append(out, reg.ListOnline()...)
	return out
}

func maybePruneStaleRigs(a *app) {
	if a == nil || a.rigs == nil {
		return
	}
	now := time.Now().Unix()
	a.rlMu.Lock()
	last := a.rigPruneLastUnix
	if now-last < int64(staleRigPruneEvery/time.Second) {
		a.rlMu.Unlock()
		return
	}
	a.rigPruneLastUnix = now
	a.rlMu.Unlock()

	removed := a.rigs.PruneOlderThan(staleRigPruneAge)
	if len(removed) == 0 {
		return
	}
	cutoff := time.Now().Add(-staleRigPruneAge).Unix()
	if n, err := store.DeleteLANPeerRigsOlderThan(context.Background(), a.db, cutoff); err == nil {
		log.Printf("lan rigs prune: removed memory=%d db=%d cutoff_unix=%d", len(removed), n, cutoff)
	} else {
		log.Printf("lan rigs prune: removed memory=%d db_error=%v", len(removed), err)
	}
}

func loadLANPeersIntoRegistry(db *sql.DB, reg *lanpool.Registry) error {
	if db == nil {
		return nil
	}
	ctx := context.Background()
	rows, err := store.LoadLANPeerRigs(ctx, db)
	if err != nil {
		return err
	}
	for _, r := range rows {
		reg.SeedFromDBRow(r.WorkerID, r.Name, r.HashrateGHS, r.LastSeenUnix, r.IP, r.SharesAccepted)
	}
	return nil
}

func persistLANPeer(ctx context.Context, db *sql.DB, workerID string, reg *lanpool.Registry) {
	if db == nil {
		return
	}
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

func networkStatsForApp(a *app) lanpool.NetworkStatsResponse {
	maybePruneStaleRigs(a)
	useMock := os.Getenv("HACKME_NETWORK_MOCK") == "1" || strings.EqualFold(os.Getenv("HACKME_NETWORK_MOCK"), "true")
	if useMock {
		return lanpool.MockNetworkStats(a.rigs, a.nodeID)
	}
	ms := a.miner.Stats()
	var gpuSum float64
	for _, g := range ms.GPUPoHDevices {
		gpuSum += g.HashrateGHS
	}
	local := lanpool.LocalMining{
		Running:        ms.Running,
		AttemptsPerSec: ms.AttemptsPerSec,
		GPUTotalGHS:    gpuSum,
	}
	return lanpool.RealNetworkStats(a.rigs, a.nodeID, local)
}

func coordinatorBaseURLFromEnvAndPeers() string {
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_URL")), "/"); v != "" {
		return v
	}
	return inferCoordinatorURLFromPeers()
}

// coordinatorBaseURL resolves pool coordinator HTTP base for proxy/stats:
// env HACKME_POOL_COORDINATOR_URL, P2P infer, or URL from dashboard-started worker subprocess.
func (a *app) coordinatorBaseURL() string {
	if v := coordinatorBaseURLFromEnvAndPeers(); v != "" {
		return v
	}
	if a != nil {
		a.workerMu.Lock()
		w := strings.TrimRight(strings.TrimSpace(a.workerCoordURL), "/")
		a.workerMu.Unlock()
		if w != "" {
			return w
		}
	}
	return ""
}

// networkModeActive reports follower/pool intent: P2P peers, coordinator env,
// explicit canonical chain URL, or a dashboard-started worker (runtime coordinator URL).
func (a *app) networkModeActive() bool {
	if strings.TrimSpace(os.Getenv("HACKME_CANONICAL_CHAIN_URL")) != "" {
		return true
	}
	if strings.TrimSpace(os.Getenv("HACKME_P2P_PEERS")) != "" {
		return true
	}
	if strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_URL")) != "" {
		return true
	}
	if envBool("HACKME_DESKTOP_MODE", false) && strings.TrimSpace(os.Getenv("HACKME_PUBLIC_AUTHORITY_BASE")) != "" {
		return true
	}
	if a == nil {
		return false
	}
	return strings.TrimSpace(a.coordinatorBaseURL()) != ""
}

func (a *app) workerProcessRunning() bool {
	if a == nil {
		return false
	}
	a.workerMu.Lock()
	subprocess := a.workerCmd != nil && a.workerCmd.Process != nil && a.workerCmd.ProcessState == nil
	dataDir := strings.TrimSpace(a.dataDir)
	a.workerMu.Unlock()
	if subprocess {
		return true
	}
	logRoot := filepath.Join(resolveWorkerRepoRoot(dataDir), "logs")
	return workerActiveFromLog(logRoot, 120)
}

func canonicalMiningObservedSec(remote map[string]any) float64 {
	v, ok := remote["mining_observed_block_sec"]
	if !ok || remote == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		if t > 0 && !math.IsNaN(t) {
			return t
		}
	case json.Number:
		if x, err := t.Float64(); err == nil && x > 0 && !math.IsNaN(x) {
			return x
		}
	}
	return 0
}

func inferCoordinatorURLFromPeers() string {
	raw := strings.TrimSpace(os.Getenv("HACKME_P2P_PEERS"))
	if raw == "" {
		return ""
	}
	first := strings.TrimSpace(strings.Split(raw, ",")[0])
	if first == "" {
		return ""
	}
	u, err := neturl.Parse(first)
	if err != nil || strings.TrimSpace(u.Scheme) == "" || strings.TrimSpace(u.Hostname()) == "" {
		return ""
	}
	coordPort := "18081"
	if p := strings.TrimSpace(u.Port()); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 && n < 65535 {
			coordPort = strconv.Itoa(n + 1)
		}
	}
	hostPort := net.JoinHostPort(strings.TrimSpace(u.Hostname()), coordPort)
	return strings.TrimRight(u.Scheme+"://"+hostPort, "/")
}

func canonicalPeerBaseURLFromEnv() string {
	raw := strings.TrimSpace(os.Getenv("HACKME_P2P_PEERS"))
	if raw == "" {
		return ""
	}
	first := strings.TrimSpace(strings.Split(raw, ",")[0])
	if first == "" {
		return ""
	}
	return strings.TrimRight(first, "/")
}

// inferCommandNodeBaseFromCoordinatorURL maps coordinator base URL to the command-node HTTP base on the same host
// using port−1 (e.g. :18081→:18080, :8081→:8080). Override with HACKME_CANONICAL_CHAIN_URL when ports differ.
func inferCommandNodeBaseFromCoordinatorURL(coordBase string) string {
	base := strings.TrimRight(strings.TrimSpace(coordBase), "/")
	if base == "" {
		return ""
	}
	u, err := neturl.Parse(base)
	if err != nil || strings.TrimSpace(u.Hostname()) == "" {
		return ""
	}
	host := strings.TrimSpace(u.Hostname())
	portStr := strings.TrimSpace(u.Port())
	if portStr == "" {
		return ""
	}
	pn, err := strconv.Atoi(portStr)
	if err != nil || pn <= 1 {
		return ""
	}
	nodePort := pn - 1
	u2 := *u
	u2.Host = net.JoinHostPort(host, strconv.Itoa(nodePort))
	return strings.TrimRight(u2.String(), "/")
}

func inferCanonicalChainBaseFromCoordinatorURL() string {
	return inferCommandNodeBaseFromCoordinatorURL(os.Getenv("HACKME_POOL_COORDINATOR_URL"))
}

func (a *app) canonicalChainBaseURL() string {
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_CANONICAL_CHAIN_URL")), "/"); v != "" {
		return v
	}
	if v := canonicalPeerBaseURLFromEnv(); v != "" {
		return v
	}
	if v := inferCanonicalChainBaseFromCoordinatorURL(); v != "" {
		return v
	}
	if a != nil {
		if v := inferCommandNodeBaseFromCoordinatorURL(a.coordinatorBaseURL()); v != "" {
			return v
		}
	}
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_PUBLIC_AUTHORITY_BASE")), "/"); v != "" {
		return v
	}
	return ""
}

// canonicalBaseWouldLoopbackProxy reports when proxying /api/tx/send to base would hit this same HTTP listener (causes rate-limit loops on VPS :18080).
func canonicalBaseWouldLoopbackProxy(r *http.Request, base string) bool {
	if r == nil {
		return false
	}
	u, err := neturl.Parse(strings.TrimRight(strings.TrimSpace(base), "/"))
	if err != nil {
		return false
	}
	canonHost := strings.ToLower(strings.TrimSpace(u.Host))
	reqHost := strings.ToLower(strings.TrimSpace(r.Host))
	if canonHost == "" || reqHost == "" {
		return false
	}
	if canonHost == reqHost {
		return true
	}
	if strings.HasPrefix(canonHost, "127.0.0.1:") && strings.HasPrefix(reqHost, "localhost:") {
		return canonHost[len("127.0.0.1:"):] == reqHost[len("localhost:"):]
	}
	if strings.HasPrefix(canonHost, "localhost:") && strings.HasPrefix(reqHost, "127.0.0.1:") {
		return canonHost[len("localhost:"):] == reqHost[len("127.0.0.1:"):]
	}
	return false
}

// shouldUseCanonicalChainAPI is true for pool followers / desktop public mode: read and submit transfers against canonical chain, not empty local SQLite.
func (a *app) shouldUseCanonicalChainAPI() bool {
	if a == nil {
		return false
	}
	if a.networkModeActive() {
		return true
	}
	return !a.miner.Running()
}

func walletCanonicalBaseUsable(base string) bool {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return false
	}
	u, err := neturl.Parse(base)
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(u.Host))
	return host != "127.0.0.1:8080" && host != "localhost:8080"
}

// fetchCanonicalAddressState loads balance_units + next_nonce from the public command node.
// Uses curl fallback when Go's HTTPS client fails (common on some desktop VPN setups).
func (a *app) fetchCanonicalAddressState(ctx context.Context, addr string) (balanceUnits, nextNonce uint64, ok bool) {
	addr = strings.TrimSpace(addr)
	if addr == "" || a == nil {
		return 0, 0, false
	}
	base := strings.TrimRight(strings.TrimSpace(a.canonicalChainBaseURL()), "/")
	if !walletCanonicalBaseUsable(base) {
		return 0, 0, false
	}
	u := base + "/api/address/" + neturl.PathEscape(addr)
	httpSec := 6
	curlSec := 8
	tryCurlFirst := false
	if envBool("HACKME_DESKTOP_MODE", false) {
		// Desktop: prefer net/http first (stable); longer curl fallback when VPN breaks Go TLS.
		httpSec = 12
		curlSec = 15
		tryCurlFirst = false
	} else if !coordinatorURLIsLoopback(base) {
		tryCurlFirst = true
	}
	parseAddr := func(parsed map[string]any) (uint64, uint64, bool) {
		if parsed == nil {
			return 0, 0, false
		}
		if strings.TrimSpace(asString(parsed["address"])) == "" && parsed["balance_units"] == nil {
			return 0, 0, false
		}
		return asUint64(parsed["balance_units"]), asUint64(parsed["next_nonce"]), true
	}
	fetchHTTP := func() (uint64, uint64, bool) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return 0, 0, false
		}
		client := &http.Client{Timeout: time.Duration(httpSec) * time.Second}
		resp, err := client.Do(req)
		if err != nil || resp == nil {
			return 0, 0, false
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return 0, 0, false
		}
		var st struct {
			BalanceUnits uint64 `json:"balance_units"`
			NextNonce    uint64 `json:"next_nonce"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
			return 0, 0, false
		}
		return st.BalanceUnits, st.NextNonce, true
	}
	fetchCurl := func() (uint64, uint64, bool) {
		if coordinatorURLIsLoopback(base) {
			return 0, 0, false
		}
		curlCtx, cancel := context.WithTimeout(context.Background(), time.Duration(curlSec+2)*time.Second)
		defer cancel()
		parsed, curlErr := fetchJSONViaCurlMax(curlCtx, u, nil, curlSec)
		if curlErr != nil && envBool("HACKME_DESKTOP_MODE", false) {
			// Wallet refresh must not fail when coordinator curl pool is saturated.
			parsed, curlErr = fetchJSONViaCurlDesktop(u, curlSec)
		}
		if curlErr != nil {
			return 0, 0, false
		}
		return parseAddr(parsed)
	}
	if tryCurlFirst {
		if units, nonce, ok := fetchCurl(); ok {
			return units, nonce, true
		}
		return fetchHTTP()
	}
	if units, nonce, ok := fetchHTTP(); ok {
		return units, nonce, true
	}
	return fetchCurl()
}

func (a *app) fetchCanonicalStatusTip(ctx context.Context) (hasGenesis bool, tipHeight uint64, tipHash string, ok bool) {
	if a == nil || a.miner.Running() {
		return false, 0, "", false
	}
	// Resolve intent via canonicalChainBaseURL (env, P2P peer, coordinator env,
	// or coordinator URL from a running dashboard-started worker). No overlay for pure local-solo.
	base := strings.TrimRight(strings.TrimSpace(a.canonicalChainBaseURL()), "/")
	if base == "" {
		return false, 0, "", false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/api/status", nil)
	if err != nil {
		return false, 0, "", false
	}
	u := strings.TrimRight(base, "/") + "/api/status"
	resp, err := coordinatorHTTPClient().Do(req)
	if err == nil && resp != nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			var remote struct {
				HasGenesis bool   `json:"has_genesis"`
				TipHeight  uint64 `json:"tip_height"`
				TipHash    string `json:"tip_hash"`
			}
			if decErr := json.NewDecoder(resp.Body).Decode(&remote); decErr == nil && strings.TrimSpace(remote.TipHash) != "" {
				return remote.HasGenesis, remote.TipHeight, remote.TipHash, true
			}
		}
	}
	// On some VPN/mobile setups Go's HTTPS path can flap while curl still succeeds.
	curlCtx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	parsed, curlErr := fetchJSONViaCurl(curlCtx, u, nil)
	if curlErr != nil {
		return false, 0, "", false
	}
	tip := strings.TrimSpace(asString(parsed["tip_hash"]))
	if tip == "" {
		return false, 0, "", false
	}
	hasGen := false
	if b, ok := parsed["has_genesis"].(bool); ok {
		hasGen = b
	}
	return hasGen, asUint64(parsed["tip_height"]), tip, true
}

// applyCanonicalChainTipToMap adds canonical_peer tip fields for /api/global/metrics without replacing local SQLite tip_height.
func (a *app) applyCanonicalChainTipToMap(ctx context.Context, dst map[string]any) {
	if dst == nil {
		return
	}
	hasGen, height, tipHash, ok := a.fetchCanonicalStatusTip(ctx)
	if !ok {
		return
	}
	dst["canonical_tip_height"] = height
	dst["canonical_tip_hash"] = tipHash
	dst["canonical_tip_has_genesis"] = hasGen
	dst["canonical_tip_ok"] = true
	dst["canonical_chain_tip_source"] = "canonical_peer"
}

// applyCanonicalChainTipToStatusMap adds canonical tip hints on flat /api/status JSON without replacing local ledger tip_height/tip_hash.
func (a *app) applyCanonicalChainTipToStatusMap(ctx context.Context, dst map[string]any) {
	if dst == nil {
		return
	}
	hasGen, height, tipHash, ok := a.fetchCanonicalStatusTip(ctx)
	if !ok {
		return
	}
	dst["canonical_tip_height"] = height
	dst["canonical_tip_hash"] = tipHash
	dst["canonical_tip_has_genesis"] = hasGen
	dst["canonical_tip_ok"] = true
	dst["canonical_tip_sync_source"] = "canonical_peer"
}

func coordinatorToken() string {
	return strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_TOKEN"))
}

func fetchCoordinatorStats(ctx context.Context, base string) (lanpool.NetworkStatsResponse, error) {
	var out lanpool.NetworkStatsResponse
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return out, fmt.Errorf("coordinator url is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/network/stats", nil)
	if err != nil {
		return out, err
	}
	req.Header.Set(coordinatorForwardHeader, "1")
	res, err := coordinatorHTTPClient().Do(req)
	if err != nil {
		// Fallback: read canonical aggregate /api/global/metrics from same host.
		if gm, gmErr := fetchGlobalMetricsFromCoordinatorHost(ctx, base); gmErr == nil {
			netPart := mapFromAny(gm["network"])
			out.TotalMiners = int(asUint64(netPart["total_miners"]))
			out.GlobalHashrateTHS = asFloat64(netPart["global_hashrate_th_s"])
			out.PeerConnections = int(asUint64(netPart["peer_connections"]))
			out.TopMiners = anySliceToStringSlice(anySlice(netPart["top_miners"]))
			out.Note = asString(netPart["note"])
			return out, nil
		}
		return out, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return out, fmt.Errorf("coordinator stats status=%d body=%s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}

func fetchCoordinatorWorkStats(ctx context.Context, base string, includeDetails bool) (map[string]any, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return nil, fmt.Errorf("coordinator url is empty")
	}
	u := base + "/api/work/stats?details=0"
	if includeDetails {
		u = base + "/api/work/stats?details=1"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(coordinatorForwardHeader, "1")
	if includeDetails {
		if tok := coordinatorToken(); tok != "" {
			req.Header.Set("X-Hackme-Admin-Token", tok)
		}
	}
	res, err := coordinatorHTTPClient().Do(req)
	if err != nil {
		if coordinatorURLIsLoopback(base) {
			// Skip curl: same-box coordinator should use Go HTTP only (curl fork storm under public poll load).
		} else {
			curlCtx, curlCancel := context.WithTimeout(context.Background(), 6*time.Second)
			curlHdr := map[string]string{coordinatorForwardHeader: "1"}
			if includeDetails {
				if tok := coordinatorToken(); tok != "" {
					curlHdr["X-Hackme-Admin-Token"] = tok
				}
			}
			if parsed, curlErr := fetchJSONViaCurl(curlCtx, u, curlHdr); curlErr == nil && len(parsed) > 0 {
				curlCancel()
				return parsed, nil
			}
			curlCancel()
		}
		// Fallback: read work section from canonical aggregate /api/global/metrics.
		if gm, gmErr := fetchGlobalMetricsFromCoordinatorHost(ctx, base); gmErr == nil {
			workPart := mapFromAny(gm["work"])
			if len(workPart) > 0 {
				return workPart, nil
			}
		}
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		body := strings.TrimSpace(string(b))
		// Miner releases ship worker token only; details=1 needs admin. Fall back to public summary.
		if includeDetails && res.StatusCode == http.StatusUnauthorized {
			if basic, err2 := fetchCoordinatorWorkStats(ctx, base, false); err2 == nil {
				return basic, nil
			}
		}
		return nil, fmt.Errorf("coordinator work stats status=%d body=%s", res.StatusCode, body)
	}
	var out map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func fetchGlobalMetricsFromCoordinatorHost(ctx context.Context, coordBase string) (map[string]any, error) {
	u, err := neturl.Parse(strings.TrimSpace(coordBase))
	if err != nil || strings.TrimSpace(u.Scheme) == "" || strings.TrimSpace(u.Host) == "" {
		return nil, fmt.Errorf("invalid coordinator base")
	}
	hostBase := strings.TrimRight(u.Scheme+"://"+u.Host, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hostBase+"/api/global/metrics", nil)
	if err != nil {
		return nil, err
	}
	res, err := coordinatorHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return nil, fmt.Errorf("global metrics status=%d body=%s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	var out map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// attachPoolLaneToStatus adds pool/coordinator and canonical-metric fields so read-only
// UIs (explorer, same-origin dashboard) see VPS-aligned difficulty without cross-origin fetches.
func (a *app) attachPoolLaneToStatus(ctx context.Context, statusBody map[string]any) {
	coord := strings.TrimRight(strings.TrimSpace(a.coordinatorBaseURL()), "/")
	var ws map[string]any
	var wsErr error
	var netStats lanpool.NetworkStatsResponse
	var netErr error
	if coord != "" {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
			defer cancel()
			m, err := fetchCoordinatorWorkStats(cctx, coord, false)
			ws = m
			wsErr = err
		}()
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
			defer cancel()
			s, err := fetchCoordinatorStats(cctx, coord)
			netStats = s
			netErr = err
		}()
		wg.Wait()
	}
	if coord != "" && wsErr == nil && ws != nil {
		if tm := asUint64(ws["target_mod"]); tm > 0 {
			statusBody["pool_target_mod"] = tm
			statusBody["pool_target_mod_source"] = "coordinator"
		}
		if hint := asUint64(ws["target_mod_load_hint"]); hint > 0 {
			statusBody["pool_target_mod_load_hint"] = hint
		}
		if _, ok := ws["target_mod_load_capped"]; ok {
			if b, ok := ws["target_mod_load_capped"].(bool); ok {
				statusBody["pool_target_mod_load_capped"] = b
			}
		}
		if min := asUint64(ws["target_mod_min"]); min > 0 {
			statusBody["pool_target_mod_min"] = min
		}
		if max := asUint64(ws["target_mod_max"]); max > 0 {
			statusBody["pool_target_mod_max"] = max
		}
		statusBody["pool_workers_count"] = asUint64(ws["workers_count"])
	} else if coord != "" && wsErr != nil {
		statusBody["pool_work_stats_error"] = wsErr.Error()
	}
	if coord != "" && netErr == nil {
		statusBody["pool_global_hashrate_th_s"] = netStats.GlobalHashrateTHS
		statusBody["pool_total_miners"] = netStats.TotalMiners
	} else if coord != "" && netErr != nil {
		statusBody["pool_network_stats_error"] = netErr.Error()
	}
	if asUint64(statusBody["pool_target_mod"]) == 0 {
		peer := strings.TrimRight(strings.TrimSpace(a.canonicalChainBaseURL()), "/")
		if peer == "" {
			return
		}
		mctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		req, err := http.NewRequestWithContext(mctx, http.MethodGet, peer+"/api/metrics", nil)
		if err != nil {
			cancel()
			return
		}
		cl := &http.Client{Timeout: 1500 * time.Millisecond}
		resp, err2 := cl.Do(req)
		if err2 != nil || resp == nil {
			cancel()
			return
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return
			}
			var remote map[string]any
			if json.NewDecoder(resp.Body).Decode(&remote) != nil {
				return
			}
			if tm := asUint64(remote["mining_target_mod"]); tm > 0 {
				statusBody["pool_target_mod"] = tm
				statusBody["pool_target_mod_source"] = "canonical_metrics"
			}
		}()
		cancel()
	}
}

func anySlice(v any) []any {
	if arr, ok := v.([]any); ok {
		return arr
	}
	return nil
}

func anySliceToStringSlice(src []any) []string {
	if len(src) == 0 {
		return nil
	}
	out := make([]string, 0, len(src))
	for _, v := range src {
		if s := strings.TrimSpace(fmt.Sprintf("%v", v)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func forwardPushWorkToCoordinator(ctx context.Context, base string, body lanpool.PushWorkBody) error {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return nil
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/push_work", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(coordinatorForwardHeader, "1")
	if t := coordinatorToken(); t != "" {
		req.Header.Set("X-Hackme-Admin-Token", t)
	}
	cl := &http.Client{Timeout: 4 * time.Second}
	res, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return fmt.Errorf("coordinator push status=%d body=%s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func (a *app) handleNetworkStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if base := a.coordinatorBaseURL(); base != "" && r.Header.Get(coordinatorForwardHeader) != "1" {
		if s, err := fetchCoordinatorStats(r.Context(), base); err == nil {
			writeJSON(w, s)
			return
		}
		log.Printf("network/stats coordinator fallback -> local aggregate")
	}
	resp := networkStatsForApp(a)
	writeJSON(w, resp)
}

func globalMetricsQueryLite(r *http.Request) bool {
	q := strings.TrimSpace(r.URL.Query().Get("lite"))
	return q == "1" || strings.EqualFold(q, "true")
}

func (a *app) handleGlobalMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if globalMetricsQueryLite(r) {
		now := time.Now().Unix()
		chainPart := map[string]any{
			"has_genesis":  false,
			"tip_height":   uint64(0),
			"tip_hash":     "",
			"total_blocks": uint64(0),
		}
		if a != nil {
			tipCtx, tipCancel := context.WithTimeout(r.Context(), 200*time.Millisecond)
			h, tip, err := a.chain.Tip(tipCtx)
			tipCancel()
			if err == nil && strings.TrimSpace(tip) != "" {
				chainPart["has_genesis"] = true
				chainPart["tip_height"] = h
				chainPart["tip_hash"] = tip
				chainPart["total_blocks"] = h + 1
			}
			overlayCtx, overlayCancel := context.WithTimeout(r.Context(), 800*time.Millisecond)
			a.applyCanonicalChainTipToMap(overlayCtx, chainPart)
			overlayCancel()
		}
		writeJSON(w, map[string]any{
			"ok":            true,
			"global_source": "lite",
			"sample_ts":     now,
			"chain":         chainPart,
			"network":       map[string]any{"ok": false, "global_source": "lite_skipped"},
			"work":          map[string]any{"ok": false, "global_source": "lite_skipped"},
			"note":          "lite=1: coordinator/network aggregate skipped for fast dashboard paint",
		})
		return
	}
	now := time.Now().Unix()
	var (
		height     uint64
		tip        string
		err        error
		hasGenesis bool
	)
	// Always include local SQLite tip_height — canonical / pool tip is exposed separately (canonical_tip_*).
	tipCtx, tipCancel := context.WithTimeout(r.Context(), 2*time.Second)
	height, tip, err = a.chain.Tip(tipCtx)
	tipCancel()
	hasGenesis = err == nil && strings.TrimSpace(tip) != ""
	chainPart := map[string]any{
		"has_genesis": hasGenesis,
		"tip_height":  height,
		"tip_hash":    tip,
		"total_blocks": func() uint64 {
			if hasGenesis {
				return height + 1
			}
			return 0
		}(),
	}
	if err != nil {
		chainPart["error"] = err.Error()
	}
	overlayCtx, overlayCancel := context.WithTimeout(r.Context(), 3*time.Second)
	a.applyCanonicalChainTipToMap(overlayCtx, chainPart)
	overlayCancel()

	networkPart := map[string]any{
		"ok":            false,
		"global_source": "local_fallback",
		"sample_ts":     now,
		"stale_sec":     int64(0),
	}
	workPart := map[string]any{
		"ok":            false,
		"global_source": "coordinator",
		"sample_ts":     now,
		"stale_sec":     int64(0),
	}
	baseCoord := a.coordinatorBaseURL()
	type coordStatsRes struct {
		stats lanpool.NetworkStatsResponse
		err   error
	}
	type coordWorkRes struct {
		work map[string]any
		err  error
	}
	var (
		statsCh chan coordStatsRes
		workCh  chan coordWorkRes
	)
	if baseCoord != "" {
		statsCh = make(chan coordStatsRes, 1)
		workCh = make(chan coordWorkRes, 1)
		go func() {
			coordCtx, coordCancel := context.WithTimeout(r.Context(), 25*time.Second)
			defer coordCancel()
			s, err := fetchCoordinatorStats(coordCtx, baseCoord)
			statsCh <- coordStatsRes{stats: s, err: err}
		}()
		go func() {
			workCtx, workCancel := context.WithTimeout(r.Context(), 25*time.Second)
			defer workCancel()
			ws, err := fetchCoordinatorWorkStats(workCtx, baseCoord, false)
			workCh <- coordWorkRes{work: ws, err: err}
		}()
	}
	if statsCh != nil {
		statsRes := <-statsCh
		if statsRes.err == nil {
			networkPart["ok"] = true
			networkPart["global_source"] = "coordinator"
			networkPart["total_miners"] = statsRes.stats.TotalMiners
			networkPart["global_hashrate_th_s"] = statsRes.stats.GlobalHashrateTHS
			networkPart["peer_connections"] = statsRes.stats.PeerConnections
			networkPart["active_rigs"] = statsRes.stats.ActiveRigs
			networkPart["top_miners"] = statsRes.stats.TopMiners
			networkPart["global_mock"] = statsRes.stats.GlobalMock
			networkPart["note"] = statsRes.stats.Note
		} else {
			networkPart["error"] = statsRes.err.Error()
		}
	}
	if ok, _ := networkPart["ok"].(bool); !ok {
		s := networkStatsForApp(a)
		networkPart["ok"] = true
		networkPart["global_source"] = "local_fallback"
		networkPart["total_miners"] = s.TotalMiners
		networkPart["global_hashrate_th_s"] = s.GlobalHashrateTHS
		networkPart["peer_connections"] = s.PeerConnections
		networkPart["active_rigs"] = s.ActiveRigs
		networkPart["top_miners"] = s.TopMiners
		networkPart["global_mock"] = s.GlobalMock
		networkPart["note"] = s.Note
	}

	if workCh != nil {
		workRes := <-workCh
		if workRes.err == nil {
			ws := workRes.work
			workPart["ok"] = true
			workPart["issued_ranges"] = asUint64(ws["issued_ranges"])
			workPart["submitted_items"] = asUint64(ws["submitted_items"])
			workPart["accepted_attempts"] = asUint64(ws["accepted_attempts"])
			workPart["target_mod"] = asUint64(ws["target_mod"])
			workPart["total_payout_hmc"] = ws["total_payout_hmc"]
			workPart["pool_hashrate_gh_s"] = ws["pool_hashrate_gh_s"]
			workPart["workers"] = ws["workers"]
			workPart["miners"] = ws["miners"]
			workPart["active_rigs"] = ws["active_rigs"]
			wc := asUint64(ws["workers_count"])
			if wc == 0 {
				wc = uint64(len(mapFromAny(ws["workers"])))
			}
			workPart["workers_count"] = wc
		} else {
			workPart["reason"] = "coordinator_unavailable"
			workPart["error"] = workRes.err.Error()
		}
	} else {
		workPart["reason"] = "coordinator_not_configured"
	}
	applyPoolHashrateToNetwork(networkPart, workPart)
	mergeWorkActiveRigsIntoNetwork(networkPart, workPart)
	// If coordinator target_mod is unavailable, pull canonical target_mod so
	// follower dashboards keep difficulty fresh while idle.
	if asUint64(workPart["target_mod"]) == 0 {
		if peer := strings.TrimRight(a.canonicalChainBaseURL(), "/"); peer != "" {
			metricsCtx, metricsCancel := context.WithTimeout(r.Context(), 1*time.Second)
			req, _ := http.NewRequestWithContext(metricsCtx, http.MethodGet, peer+"/api/metrics", nil)
			cl := &http.Client{Timeout: 1 * time.Second}
			if resp, err := cl.Do(req); err == nil && resp != nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var remote map[string]any
					if decErr := json.NewDecoder(resp.Body).Decode(&remote); decErr == nil {
						if tm := asUint64(remote["mining_target_mod"]); tm > 0 {
							workPart["target_mod"] = tm
							if _, ok := workPart["reason"]; !ok {
								workPart["reason"] = "target_from_canonical_metrics"
							}
						}
					}
				}
			}
			metricsCancel()
		}
	}

	writeJSON(w, map[string]any{
		"ok":            true,
		"global_source": "vps_node+coordinator",
		"sample_ts":     now,
		"stale_sec":     int64(0),
		"chain":         chainPart,
		"network":       networkPart,
		"work":          workPart,
	})
}

func (a *app) handleWorkStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	base := a.coordinatorBaseURL()
	if base == "" {
		writeJSON(w, map[string]any{
			"ok":      false,
			"reason":  "coordinator_not_configured",
			"message": "set HACKME_POOL_COORDINATOR_URL to proxy /api/work/stats from coordinator",
		})
		return
	}
	includeDetails := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("details")))
	details := includeDetails == "1" || includeDetails == "true" || includeDetails == "yes"
	// Keep this longer than browser poll cadence to allow at least some successful
	// refreshes on slow links (VPN/mobile), while still bounded for UI responsiveness.
	coordCtx, cancel := context.WithTimeout(context.Background(), 9*time.Second)
	defer cancel()
	ws, err := fetchCoordinatorWorkStats(coordCtx, base, details)
	if err != nil {
		log.Printf("work/stats coordinator fallback: %v", err)
		workStatsCacheMu.RLock()
		cached := workStatsCache
		cachedTS := workStatsCacheTS
		workStatsCacheMu.RUnlock()
		if cached != nil {
			out := map[string]any{}
			for k, v := range cached {
				out[k] = v
			}
			out["source"] = base
			out["stale"] = true
			out["stale_reason"] = err.Error()
			if cachedTS > 0 {
				out["stale_sec"] = time.Now().Unix() - cachedTS
			}
			writeJSON(w, out)
			return
		}
		writeJSON(w, map[string]any{
			"ok":      false,
			"reason":  "coordinator_unavailable",
			"message": err.Error(),
			"source":  base,
		})
		return
	}
	ensureCoordinatorWorkersMap(ws)
	workersMap := mapFromAny(ws["workers"])
	workersCount := asUint64(ws["workers_count"])
	if workersCount == 0 && len(workersMap) > 0 {
		ws["workers_count"] = uint64(len(workersMap))
	}
	ws["ok"] = true
	ws["source"] = base
	ws["stale"] = false
	ws["stale_sec"] = int64(0)
	workStatsCacheMu.Lock()
	workStatsCache = ws
	workStatsCacheTS = time.Now().Unix()
	workStatsCacheMu.Unlock()
	writeJSON(w, ws)
}

func mapFromAny(v any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	switch t := v.(type) {
	case map[string]any:
		return t
	default:
		out := map[string]any{}
		_ = json.Unmarshal([]byte(asString(v)), &out)
		return out
	}
}

// repairWorkerSettlementState clamps local settled_hmc when coordinator restarted and accrued dropped.
func repairWorkerSettlementState(state *workerSettlementState, workers map[string]any) bool {
	if state == nil || len(workers) == 0 {
		return false
	}
	if state.Workers == nil {
		state.Workers = map[string]workerSettlementStateEntry{}
	}
	changed := false
	for wid, v := range workers {
		row := mapFromAny(v)
		accrued := parseAnyFloat(row["payout_hmc"])
		if accrued < 0 {
			accrued = 0
		}
		ent := state.Workers[wid]
		if ent.SettledHMC > accrued+1e-12 {
			ent.SettledHMC = accrued
			state.Workers[wid] = ent
			changed = true
		}
	}
	return changed
}

// mergeWorkActiveRigsIntoNetwork merges coordinator submit hashrate rigs into network active_rigs.
func mergeWorkActiveRigsIntoNetwork(networkPart, workPart map[string]any) {
	if networkPart == nil {
		return
	}
	byID := map[string]map[string]any{}
	for _, row := range anySlice(networkPart["active_rigs"]) {
		m := mapFromAny(row)
		if wid := strings.TrimSpace(asString(m["worker_id"])); wid != "" {
			byID[wid] = m
		}
	}
	if workPart != nil {
		for _, row := range anySlice(workPart["active_rigs"]) {
			m := mapFromAny(row)
			wid := strings.TrimSpace(asString(m["worker_id"]))
			if wid == "" {
				continue
			}
			if cur, ok := byID[wid]; ok {
				if parseAnyFloat(m["hashrate_gh_s"]) > parseAnyFloat(cur["hashrate_gh_s"]) {
					byID[wid] = m
				}
			} else {
				byID[wid] = m
			}
		}
	}
	if len(byID) == 0 {
		return
	}
	out := make([]any, 0, len(byID))
	for _, r := range byID {
		out = append(out, r)
	}
	networkPart["active_rigs"] = out
}

// applyPoolHashrateToNetwork sets global_hashrate_th_s from work pool_hashrate_gh_s and active rigs (best of both).
func applyPoolHashrateToNetwork(networkPart, workPart map[string]any) {
	if networkPart == nil {
		return
	}
	var sumRigsGH float64
	for _, row := range anySlice(networkPart["active_rigs"]) {
		m := mapFromAny(row)
		sumRigsGH += parseAnyFloat(m["hashrate_gh_s"])
	}
	poolGH := float64(0)
	if workPart != nil {
		poolGH = parseAnyFloat(workPart["pool_hashrate_gh_s"])
	}
	totalGH := sumRigsGH
	if poolGH > totalGH {
		totalGH = poolGH
	}
	if totalGH > 0 {
		networkPart["global_hashrate_th_s"] = totalGH / 1000.0
		networkPart["pool_hashrate_gh_s"] = totalGH
	}
}

// coordinatorWorkersMap returns per-worker rows when present (not a bare online count).
func coordinatorWorkersMap(ws map[string]any) map[string]any {
	if ws == nil {
		return map[string]any{}
	}
	if m, ok := ws["workers"].(map[string]any); ok {
		return m
	}
	return mapFromAny(ws["workers"])
}

// ensureCoordinatorWorkersMap fills ws["workers"] when public coordinator stats omit per-worker rows.
func ensureCoordinatorWorkersMap(ws map[string]any) {
	if ws == nil {
		return
	}
	if _, ok := ws["workers"].(float64); ok {
		return
	}
	if _, ok := ws["workers"].(int); ok {
		return
	}
	if len(coordinatorWorkersMap(ws)) > 0 {
		return
	}
	workersCount := asUint64(ws["workers_count"])
	if workersCount == 0 {
		return
	}
	ws["workers"] = map[string]any{
		"worker-active": map[string]any{
			"accepted_ranges":   asUint64(ws["submitted_items"]),
			"accepted_hits":     asUint64(ws["found_hits"]),
			"accepted_attempts": asUint64(ws["accepted_attempts"]),
			"payout_hmc":        parseAnyFloat(ws["total_payout_hmc"]),
		},
	}
}

// walletWorkerRowMatches returns true when coordinator worker row accrual belongs to nodeAddress wallet.
func walletWorkerRowMatches(nodeAddress, workerID string, row map[string]any, ws map[string]any, payoutMap map[string]string) bool {
	displayAddr := settlementDisplayWalletAddress(nodeAddress, payoutMap)
	nodeAddr := displayAddr
	if nodeAddr == "" {
		nodeAddr = strings.TrimSpace(nodeAddress)
	}
	if nodeAddr == "" {
		return true
	}
	if payoutMap != nil {
		if target := strings.TrimSpace(payoutMap[workerID]); target != "" && strings.EqualFold(target, nodeAddr) {
			return true
		}
	}
	if payoutAddr := strings.TrimSpace(asString(row["payout_address"])); payoutAddr != "" {
		return strings.EqualFold(payoutAddr, nodeAddr)
	}
	lastSigner := strings.TrimSpace(asString(ws["last_signed_miner_address"]))
	wid := strings.ToLower(strings.TrimSpace(workerID))
	if wid == "worker-active" {
		return false
	}
	if lastSigner != "" && strings.EqualFold(lastSigner, nodeAddr) {
		if wid == "" || strings.Contains(wid, "kapa") || strings.Contains(wid, "desktop") || strings.HasSuffix(wid, "-pc") {
			return true
		}
	}
	return false
}

// settlementDisplayWalletAddress prefers WORKER_PAYOUT_MAP targets over the node signer id for accrual UI.
func settlementDisplayWalletAddress(nodeAddress string, payoutMap map[string]string) string {
	for _, target := range payoutMap {
		t := strings.TrimSpace(target)
		if strings.HasPrefix(t, "HMC-") {
			return t
		}
	}
	return strings.TrimSpace(nodeAddress)
}

// walletAccrualFromCoordinator derives accrued/settled/unpaid HMC for the desktop wallet from coordinator stats.
// accrualSource describes how unpaid was attributed (for dashboard transparency).
func walletAccrualFromCoordinator(ws map[string]any, stateWorkers map[string]workerSettlementStateEntry, nodeAddress, desktopWorkerID string, payoutMap map[string]string, localWorkerRunning bool) (accrued, settled, unpaid float64, accrualSource string) {
	if ws == nil {
		return 0, 0, 0, "unavailable"
	}
	accrualSource = "none"
	workers := coordinatorWorkersMap(ws)
	coordHasPerWorker := len(workers) > 0
	if !coordHasPerWorker {
		ensureCoordinatorWorkersMap(ws)
		workers = coordinatorWorkersMap(ws)
	}
	nodeAddr := settlementDisplayWalletAddress(nodeAddress, payoutMap)
	wid := strings.TrimSpace(desktopWorkerID)
	if wid == "" {
		wid = "worker-kapa-pc"
	}
	var sumAccrued, sumSettled float64
	for workerID, v := range workers {
		if !walletWorkerRowMatches(nodeAddr, workerID, mapFromAny(v), ws, payoutMap) {
			continue
		}
		row := mapFromAny(v)
		rowAccrued := parseAnyFloat(row["payout_hmc"])
		if rowAccrued < 0 {
			rowAccrued = 0
		}
		rowSettled := stateWorkers[workerID].SettledHMC
		if rowSettled < 0 {
			rowSettled = 0
		}
		if rowSettled > rowAccrued {
			rowSettled = rowAccrued
		}
		sumAccrued += rowAccrued
		sumSettled += rowSettled
	}
	if sumAccrued > 0 {
		if coordHasPerWorker {
			accrualSource = "per_worker"
		} else {
			accrualSource = "fleet_aggregate"
		}
	}
	// Do not attribute fleet total_payout_hmc to a mapped desktop worker when per-worker rows are missing
	// (public nginx used to drop ?details=1; pool total is mostly other workers e.g. vps-canary).
	if sumAccrued <= 0 && nodeAddr != "" && !coordHasPerWorker && asUint64(ws["workers_count"]) > 0 {
		if accrualSource == "none" {
			accrualSource = "workers_omitted"
		}
	}
	unpaid = sumAccrued - sumSettled
	if unpaid < 0 {
		unpaid = 0
	}
	accrued = sumAccrued
	settled = sumSettled
	return
}

func canonicalFetchBudget(ctx context.Context, defaultDur time.Duration) time.Duration {
	if ctx == nil {
		return defaultDur
	}
	if dl, ok := ctx.Deadline(); ok {
		left := time.Until(dl)
		if left <= 0 {
			return 50 * time.Millisecond
		}
		if left < defaultDur {
			return left
		}
	}
	return defaultDur
}

// fetchCanonicalJSON GETs JSON from canonical with a bounded timeout.
func fetchCanonicalJSON(ctx context.Context, url string, timeout time.Duration) (map[string]any, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false
	}
	cl := &http.Client{Timeout: timeout}
	resp, err := cl.Do(req)
	if err != nil || resp == nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var remote map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&remote); err != nil {
		return nil, false
	}
	return remote, true
}

func applyCanonicalBlockEconomics(s *MetricsSnapshot, bh uint64) bool {
	if s == nil || bh == 0 {
		return false
	}
	s.BlockHeight = bh
	s.EconNextHalvingBlock = chain.NextHalvingBlock(bh)
	s.EconExpectedEmptyHmcHour = chain.ExpectedEmptyMiningHMCPerHour(bh)
	s.EconBaseRewardNowHMC = chain.BaseRewardForBlockIndex(bh + 1)
	s.EconRewardTailFloorHMC = chain.RewardTailFloorHMC
	if strings.TrimSpace(s.MiningTaskSource) != chain.TaskSourceOrder {
		s.MiningRewardHMC = s.EconBaseRewardNowHMC
	}
	return true
}

// fillMetricsFleetHashrate sets hashrate_th_s from live GPU/worker/pool data (never synthetic).
func (a *app) fillMetricsFleetHashrate(ctx context.Context, s *MetricsSnapshot) {
	if a == nil || s == nil {
		return
	}
	if s.MiningGPUTotalGHS > 0 {
		s.HashrateTHs = s.MiningGPUTotalGHS / 1000.0
		return
	}
	base := strings.TrimRight(a.coordinatorBaseURL(), "/")
	if base == "" {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	ws, err := fetchCoordinatorWorkStats(cctx, base, false)
	if err != nil || ws == nil {
		return
	}
	gh := parseAnyFloat(ws["pool_hashrate_gh_s"])
	if gh <= 0 {
		for _, row := range anySlice(ws["active_rigs"]) {
			gh += parseAnyFloat(mapFromAny(row)["hashrate_gh_s"])
		}
	}
	if gh <= 0 {
		for _, row := range coordinatorWorkersMap(ws) {
			gh += parseAnyFloat(mapFromAny(row)["hashrate_gh_s"])
		}
	}
	if gh > 0 {
		if gh > 5000 {
			gh = 5000
		}
		s.HashrateTHs = gh / 1000.0
	}
}

// overlayCanonicalMiningIntoSnapshot aligns PoH modulus / block-height-derived economics with the canonical
// command node when local PoH search is idle (dashboard worker subprocess or coordinator follower env).
func (a *app) overlayCanonicalMiningIntoSnapshot(ctx context.Context, s *MetricsSnapshot) bool {
	if a == nil || s == nil {
		return false
	}
	if a.miner.Running() {
		return false
	}
	wantOverlay := a.workerProcessRunning() ||
		coordinatorBaseURLFromEnvAndPeers() != "" ||
		strings.TrimSpace(os.Getenv("HACKME_CANONICAL_CHAIN_URL")) != "" ||
		canonicalPeerBaseURLFromEnv() != ""
	if !wantOverlay {
		return false
	}
	base := strings.TrimRight(a.canonicalChainBaseURL(), "/")
	if base == "" {
		return false
	}
	out := false
	if status, ok := fetchCanonicalJSON(ctx, base+"/api/status", canonicalFetchBudget(ctx, 4*time.Second)); ok {
		if bh := asUint64(status["tip_height"]); bh > 0 {
			if applyCanonicalBlockEconomics(s, bh) {
				out = true
			}
			if tm := asUint64(status["mining_target_mod"]); tm > 0 {
				s.MiningTargetMod = tm
				s.MiningTargetModAtCap = chain.IsPoHTargetModAtCap(tm)
				out = true
			}
			if obs := canonicalMiningObservedSec(status); obs > 0 {
				s.MiningObservedBlockSec = obs
				out = true
			}
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/metrics", nil)
	if err != nil {
		return out
	}
	metricsTimeout := canonicalFetchBudget(ctx, 3*time.Second)
	if out {
		metricsTimeout = canonicalFetchBudget(ctx, 2*time.Second)
	}
	cl := &http.Client{Timeout: metricsTimeout}
	resp, err := cl.Do(req)
	if err != nil || resp == nil {
		return out
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out
	}
	var remote map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&remote); err != nil {
		return out
	}
	sched := false
	if tm := asUint64(remote["mining_target_mod"]); tm > 0 {
		s.MiningTargetMod = tm
		s.MiningTargetModAtCap = chain.IsPoHTargetModAtCap(tm)
		sched = true
		out = true
	}
	if bh := asUint64(remote["block_height"]); bh > 0 {
		s.BlockHeight = bh
		sched = true
		out = true
	}
	if obs := canonicalMiningObservedSec(remote); obs > 0 {
		s.MiningObservedBlockSec = obs
		out = true
	}
	if sched {
		s.EconNextHalvingBlock = chain.NextHalvingBlock(s.BlockHeight)
		s.EconExpectedEmptyHmcHour = chain.ExpectedEmptyMiningHMCPerHour(s.BlockHeight)
		s.EconBaseRewardNowHMC = chain.BaseRewardForBlockIndex(s.BlockHeight + 1)
		if strings.TrimSpace(s.MiningTaskSource) != chain.TaskSourceOrder {
			s.MiningRewardHMC = s.EconBaseRewardNowHMC
		}
	}
	if v := asFloat64(remote["econ_window_total_hmc"]); v > 0 {
		s.EconWindowTotalHMC = v
		out = true
	}
	if v := asFloat64(remote["econ_window_base_hmc"]); v > 0 {
		s.EconWindowBaseHMC = v
		out = true
	}
	if v := asFloat64(remote["econ_window_order_hmc"]); v > 0 {
		s.EconWindowOrderHMC = v
		out = true
	}
	if v := asInt64(remote["econ_window_blocks"]); v > 0 {
		s.EconWindowBlocks = int(v)
		s.MiningPohBlocksLast1h = int(v)
		out = true
	}
	if v := asInt64(remote["econ_window_base_blocks"]); v > 0 {
		s.EconWindowBaseBlocks = int(v)
		out = true
	}
	if v := asInt64(remote["econ_window_order_blocks"]); v > 0 {
		s.EconWindowOrderBlocks = int(v)
		out = true
	}
	if v := asFloat64(remote["mining_hmc_last_hour_approx"]); v > 0 {
		s.MiningHmcLastHourApprox = v
		out = true
	} else if s.EconWindowTotalHMC > 0 {
		s.MiningHmcLastHourApprox = s.EconWindowTotalHMC
		out = true
	}
	if v := asInt64(remote["mining_poh_blocks_last_1h"]); v > 0 {
		s.MiningPohBlocksLast1h = int(v)
		out = true
	}
	if v := asFloat64(remote["econ_window_order_share_pct"]); v > 0 {
		s.EconWindowOrderShare = v
		out = true
	} else if s.EconWindowTotalHMC > 1e-12 {
		s.EconWindowOrderShare = 100 * s.EconWindowOrderHMC / s.EconWindowTotalHMC
	}
	if s.EconWindowSec <= 0 {
		s.EconWindowSec = 3600
	}
	return out
}

func (a *app) handlePushWork(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireAdminAuth(w, r) {
		return
	}
	if !a.allowRate("push_work:"+clientIP(r), 30) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	logAdminAction(r, "push_work")
	if r.Body == nil {
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	var body lanpool.PushWorkBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	remote := r.RemoteAddr
	if err := a.rigs.Upsert(remote, body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(body.WorkerID)
	persistLANPeer(r.Context(), a.db, id, a.rigs)
	if base := a.coordinatorBaseURL(); base != "" && r.Header.Get(coordinatorForwardHeader) != "1" {
		if err := forwardPushWorkToCoordinator(r.Context(), base, body); err != nil {
			log.Printf("push_work: coordinator forward failed: %v", err)
		}
	}
	writeJSON(w, map[string]any{"ok": true, "worker_id": id})
}
