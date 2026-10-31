package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hackme/internal/chain"
	"hackme/internal/gpupoh"
	"hackme/internal/gputune"
	"hackme/internal/hotlog"
	"hackme/internal/operator"
	"hackme/internal/sandbox"
	"hackme/internal/workerfuzzloop"
	"hackme/internal/workerid"
	"hackme/internal/workerlock"
	"hackme/internal/worksubmit"
)

// Simple PoH worker:
// - claims nonce ranges from coordinator
// - searches for a valid nonce in the range (CPU baseline)
// - submits found hit with hybrid signature (optional but recommended / often required)
// - digs pool fuzz under the same worker_id by default (HACKME_WORKER_HYBRID_FUZZ=0 to disable)
//
// Build:
//   go build -o workerpoh ./cmd/workerpoh
//
// Run:
//   COORD_URL=https://hackme.tech/pool/coordinator COORD_TOKEN=... WORKER_ID=rig-01 \
//   HACKME_MINER_ED25519_SEED_HEX=... ./workerpoh
//
// Hybrid (PoH+fuzz, same worker_id; default ON, inline mode):
//   ./workerpoh
//   HACKME_WORKER_HYBRID_FUZZ=0 ./workerpoh              # escape hatch
//   HACKME_WORKER_HYBRID_FUZZ_MODE=process ./workerpoh   # OS-isolated workerfuzz child
//
// Or place coordinator admin in .secrets/hackme_coordinator_admin_token (one line) and set only COORD_URL + miner seed.
//
// Note: pre-v3 coordinator validates eval_v1(n)=7n+13; post-v3 Blake3(prev||miner||nonce_le).

type claimResp struct {
	OK                  bool    `json:"ok"`
	Reason              string  `json:"reason,omitempty"`
	WorkerID            string  `json:"worker_id,omitempty"`
	BaseNonce           uint64  `json:"base_nonce"`
	BatchSize           uint64  `json:"batch_size"`
	WorkID              string  `json:"work_id,omitempty"`
	TargetMod           uint64  `json:"target_mod,omitempty"`
	OrderTaskID         string  `json:"order_task_id,omitempty"`
	WasmCheckHex        string  `json:"wasm_check_hex,omitempty"`
	OrderRewardHMC      float64 `json:"order_reward_hmc,omitempty"`
	PrevHash            string  `json:"prev_hash,omitempty"`
	MinerAddress        string  `json:"miner_address,omitempty"`
	PoHFormula          string  `json:"poh_formula,omitempty"`
	PoHFormulaVersion   int     `json:"poh_formula_version,omitempty"`
	PoHActivationHeight uint64  `json:"poh_activation_height,omitempty"`
	ShareDivisor        uint64  `json:"share_divisor,omitempty"`
	ShareMod            uint64  `json:"share_mod,omitempty"`
	ShareStrict         bool    `json:"share_strict,omitempty"`
	ShareScanMax        uint64  `json:"share_scan_max,omitempty"`
	LeaseExpiresAt      int64   `json:"lease_expires_at,omitempty"`
}

type submitReq struct {
	WorkerID    string  `json:"worker_id"`
	BaseNonce   uint64  `json:"base_nonce"`
	BatchSize   uint64  `json:"batch_size"`
	WorkID      string  `json:"work_id,omitempty"`
	Attempts    uint64  `json:"attempts,omitempty"`
	Found       bool    `json:"found,omitempty"`
	FoundNonce  uint64  `json:"found_nonce,omitempty"`
	ResultHash  string  `json:"result_hash,omitempty"`
	HashrateGHS float64 `json:"hashrate_gh_s,omitempty"`

	MinerPubKey  string   `json:"miner_pubkey_ed25519,omitempty"`
	MinerSig     string   `json:"miner_sig_ed25519,omitempty"`
	MinerSigAlg  string   `json:"miner_sig_alg,omitempty"`
	SubmitNonce  uint64   `json:"submit_nonce,omitempty"`
	OrderTaskID  string   `json:"order_task_id,omitempty"`
	WasmGatePass bool     `json:"wasm_gate_pass,omitempty"`
	ShareNonces  []uint64 `json:"share_nonces,omitempty"`
}

func canonicalSubmitBytes(req submitReq) []byte {
	rh, ph := worksubmit.NormalizeHashes(req.ResultHash, "")
	p := worksubmit.SignPayload{
		WorkerID:    strings.TrimSpace(req.WorkerID),
		BaseNonce:   req.BaseNonce,
		BatchSize:   req.BatchSize,
		WorkID:      strings.TrimSpace(req.WorkID),
		Attempts:    req.Attempts,
		Found:       req.Found,
		FoundNonce:  req.FoundNonce,
		ResultHash:  rh,
		ProofHash:   ph,
		SubmitNonce: req.SubmitNonce,
	}
	return p.CanonicalJSON()
}

func loadAndBumpSubmitNonce(path string) (uint64, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = filepath.Join("logs", "miner_submit_nonce.seq")
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	// Floor to unix-ms so a cold/low seq file cannot sit behind a fleet that shared the same miner key.
	floor := uint64(time.Now().UnixMilli())
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(path, []byte(strconv.FormatUint(floor, 10)), 0o644); err != nil {
				return 0, err
			}
			return floor, nil
		}
		return 0, err
	}
	cur, _ := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	next := cur + 1
	if next == 0 {
		next = 1
	}
	if next < floor {
		next = floor
	}
	if err := os.WriteFile(path, []byte(strconv.FormatUint(next, 10)), 0o644); err != nil {
		return 0, err
	}
	return next, nil
}

func findHitCPU(base, batch, mod uint64) (bool, uint64) {
	return findHitCPUPoH(base, batch, mod, 1, "", "")
}

func findHitCPUPoH(base, batch, mod uint64, formulaVersion int, prevHash, minerAddr string) (bool, uint64) {
	end := base + batch
	if end < base {
		end = ^uint64(0)
	}
	for n := base; n < end; n++ {
		var work uint64
		if formulaVersion >= 3 {
			work = chain.PohWorkBlake3(prevHash, minerAddr, n)
		} else {
			work = chain.PohEval(n)
		}
		if mod > 0 && work%mod == 0 {
			return true, n
		}
	}
	return false, 0
}

// collectShareNonces finds up to maxShares nonces in [base, base+batch) where work%shareMod==0.
// Caps hash budget (maxScan from claim share_scan_max). Large leases use stride sampling
// across the full range so PPS proofs are not stuck in the first 256K nonces.
func collectShareNonces(base, batch, shareMod uint64, formulaVersion int, prevHash, minerAddr string, maxShares int, foundNonce uint64, found bool, maxScan uint64) []uint64 {
	if shareMod == 0 || batch == 0 || maxShares <= 0 {
		return nil
	}
	if maxScan == 0 {
		maxScan = uint64(256 * 1024)
	}
	stride := uint64(1)
	if batch > maxScan {
		stride = batch / maxScan
		if stride < 1 {
			stride = 1
		}
	}
	out := make([]uint64, 0, maxShares)
	seen := map[uint64]struct{}{}
	end := base + batch
	if end < base {
		end = ^uint64(0)
	}
	if found && foundNonce >= base && foundNonce < end {
		var fwork uint64
		if formulaVersion >= 3 {
			fwork = chain.PohWorkBlake3(prevHash, minerAddr, foundNonce)
		} else {
			fwork = chain.PohEval(foundNonce)
		}
		if fwork%shareMod == 0 {
			out = append(out, foundNonce)
			seen[foundNonce] = struct{}{}
		}
	}
	scanned := uint64(0)
	for n := base; n < end && len(out) < maxShares && scanned < maxScan; n += stride {
		scanned++
		if _, ok := seen[n]; ok {
			continue
		}
		var work uint64
		if formulaVersion >= 3 {
			work = chain.PohWorkBlake3(prevHash, minerAddr, n)
		} else {
			work = chain.PohEval(n)
		}
		if work%shareMod != 0 {
			continue
		}
		out = append(out, n)
		seen[n] = struct{}{}
	}
	return out
}

// filterVerifiedShares drops GPU/host false positives before submit so the pool
// never sees share_nonces that fail PoH % shareMod (keeps share_rejects near zero).
func filterVerifiedShares(shares []uint64, base, batch, shareMod uint64, formulaVersion int, prevHash, minerAddr string, maxShares int) []uint64 {
	if shareMod == 0 || batch == 0 || maxShares <= 0 || len(shares) == 0 {
		return nil
	}
	end := base + batch
	if end < base {
		end = ^uint64(0)
	}
	out := make([]uint64, 0, maxShares)
	seen := make(map[uint64]struct{}, len(shares))
	for _, n := range shares {
		if len(out) >= maxShares {
			break
		}
		if _, ok := seen[n]; ok {
			continue
		}
		if n < base || n >= end {
			continue
		}
		seen[n] = struct{}{}
		var work uint64
		if formulaVersion >= 3 {
			work = chain.PohWorkBlake3(prevHash, minerAddr, n)
		} else {
			work = chain.PohEval(n)
		}
		if work%shareMod != 0 {
			continue
		}
		out = append(out, n)
	}
	return out
}

func isTruthy(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func coordPushWorkEnabled() bool {
	v := strings.TrimSpace(os.Getenv("COORD_PUSH_WORK"))
	if v == "" {
		return true
	}
	return isTruthy(v)
}

// readCoordinatorTokenFromSecrets matches scripts/ops/worker_loop.sh (one line, no quotes).
func readCoordinatorTokenFromSecrets() string {
	return operator.ReadCoordinatorAdminToken()
}

func pushWorkSnapshot(cl *http.Client, coordURL, token, workerID, workerName string, ghs float64, shareAccepted bool, sharesOK int64) {
	if !coordPushWorkEnabled() {
		return
	}
	body := map[string]any{
		"worker_id":       workerID,
		"name":            workerName,
		"hashrate_gh_s":   ghs,
		"share_accepted":  shareAccepted,
		"shares_accepted": sharesOK,
	}
	jb, err := json.Marshal(body)
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(coordURL, "/")+"/api/push_work", bytes.NewReader(jb))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", token)
	res, err := cl.Do(req)
	if err != nil {
		return
	}
	_ = res.Body.Close()
}

// loadHybridSigningMaterial returns (priv, pubHex, payoutAddr, hybrid) when a miner seed is available
// (HACKME_MINER_ED25519_SEED_HEX, HACKME_MINER_SEED_FILE, or desktop node seed).
func loadHybridSigningMaterial() (ed25519.PrivateKey, string, string, bool, error) {
	priv, pubHex, addr, hybrid, err := workerfuzzloop.LoadHybridKey()
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "required") || strings.Contains(msg, "SEED") {
			return nil, "", "", false, nil
		}
		return nil, "", "", false, err
	}
	return priv, pubHex, strings.TrimSpace(addr), hybrid, nil
}

type searcher interface {
	Label() string
	Search(ctxTimeout time.Duration, base, count, mod uint64) (found bool, nonce uint64, elapsedSec float64, err error)
	SearchPoH(ctxTimeout time.Duration, req gpupoh.SearchRequest) (found bool, nonce uint64, elapsedSec float64, err error)
	SearchPoHCollect(ctxTimeout time.Duration, req gpupoh.SearchRequest) (res gpupoh.SearchResult, elapsedSec float64, err error)
}

type cpuSearcher struct{}

func (cpuSearcher) Label() string { return "cpu" }
func (cpuSearcher) Search(ctxTimeout time.Duration, base, count, mod uint64) (bool, uint64, float64, error) {
	t0 := time.Now()
	found, nonce := findHitCPU(base, count, mod)
	return found, nonce, time.Since(t0).Seconds(), nil
}
func (cpuSearcher) SearchPoH(ctxTimeout time.Duration, req gpupoh.SearchRequest) (bool, uint64, float64, error) {
	res, sec, err := (cpuSearcher{}).SearchPoHCollect(ctxTimeout, req)
	return res.Found, res.Nonce, sec, err
}
func (cpuSearcher) SearchPoHCollect(ctxTimeout time.Duration, req gpupoh.SearchRequest) (gpupoh.SearchResult, float64, error) {
	t0 := time.Now()
	if req.FormulaVersion >= 3 {
		ctx := context.Background()
		if ctxTimeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, ctxTimeout)
			defer cancel()
		}
		res, err := gpupoh.Blake3SearchCollect(ctx, req)
		return res, time.Since(t0).Seconds(), err
	}
	found, nonce := findHitCPU(req.Base, req.Count, req.Mod)
	return gpupoh.SearchResult{Found: found, Nonce: nonce}, time.Since(t0).Seconds(), nil
}

type gpuSearcher struct {
	acc gpupoh.Accelerator
}

func (g gpuSearcher) Label() string {
	return strings.TrimSpace(g.acc.Label() + " " + g.acc.DeviceName())
}
func workerHTTPDuration(envKey string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(envKey))
	if v == "" {
		v = strings.TrimSpace(os.Getenv("HACKME_WORKER_HTTP_TIMEOUT"))
	}
	if v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return fallback
}

// workerClaimCooldownMS returns pause between claim/submit cycles.
// Unset env uses 100ms on GPU (anti hammer); explicit HACKME_WORKER_CLAIM_COOLDOWN_MS=0 means no sleep.
func workerClaimCooldownMS(mode string) int {
	ms := envIntMs("HACKME_WORKER_CLAIM_COOLDOWN_MS", -1)
	if ms < 0 {
		if mode == "gpu" && gpuBackendConfigured() {
			return 100
		}
		return 0
	}
	return ms
}

// effectiveClaimCooldownMS aligns worker pacing with coordinator per-GH rate caps.
// Blake3 LIVE wall samples are often 0.1–0.4 GH/s while the GPU kernel itself is
// multi-GH/s — the old 25s floor on ghs<1 starved SM occupancy and made pool credit
// look like a dead card. Only apply a mild sub-GH floor when kernel timing is also slow
// (or missing) so pre-Blake3 CPU hammers still back off.
func effectiveClaimCooldownMS(mode string, ghs float64) int {
	ms := workerClaimCooldownMS(mode)
	if mode != "gpu" || !(ghs > 0 && ghs < 1.0) {
		return ms
	}
	// Fast Blake3 CUDA/OpenCL kernels: trust the short anti-hammer cooldown only.
	if k := lastGPUKernelSeconds(); k > 0 && k < 0.05 {
		return ms
	}
	if gpuKernelTimingActive() {
		return ms
	}
	subGHMin := envIntMs("HACKME_WORKER_SUBGH_COOLDOWN_MS", 500)
	if subGHMin <= 0 {
		return ms
	}
	if ms < subGHMin {
		return subGHMin
	}
	return ms
}

func envIntMs(envKey string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(envKey))
	if v == "" {
		return fallback
	}
	x, err := strconv.Atoi(v)
	if err != nil || x < 0 {
		return fallback
	}
	return x
}

// envIntPositive is envIntMs for values where zero is invalid (e.g. the GPU
// search timeout: a zero-duration context is already expired, so every GPU
// Search would fail and silently fall back to CPU).
func envIntPositive(envKey string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(envKey))
	if v == "" {
		return fallback
	}
	x, err := strconv.Atoi(v)
	if err != nil || x <= 0 {
		return fallback
	}
	return x
}

func envUint64(envKey string, fallback uint64) uint64 {
	v := strings.TrimSpace(os.Getenv(envKey))
	if v == "" {
		return fallback
	}
	x, err := strconv.ParseUint(v, 10, 64)
	if err != nil || x == 0 {
		return fallback
	}
	return x
}

func newWorkerHTTPClient(timeout time.Duration) *http.Client {
	if timeout < 5*time.Second {
		timeout = 5 * time.Second
	}
	hdrTimeout := timeout - 2*time.Second
	if hdrTimeout < 3*time.Second {
		hdrTimeout = 3 * time.Second
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   12 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   12 * time.Second,
			ResponseHeaderTimeout: hdrTimeout,
			ExpectContinueTimeout: 1 * time.Second,
			MaxIdleConnsPerHost:   4,
			IdleConnTimeout:       45 * time.Second,
		},
	}
}

func sleepWorkerBackoff(kind string, backoff *time.Duration) {
	wait := *backoff
	if wait < 2*time.Second {
		wait = 2 * time.Second
	}
	hotlog.Stderrf("%s: backing off %s (coordinator/network)", kind, wait.Round(time.Millisecond))
	time.Sleep(wait)
	next := wait * 2
	if next > 45*time.Second {
		next = 45 * time.Second
	}
	*backoff = next
}

func (g gpuSearcher) Search(ctxTimeout time.Duration, base, count, mod uint64) (bool, uint64, float64, error) {
	return g.SearchPoH(ctxTimeout, gpupoh.SearchRequest{Base: base, Count: count, Mod: mod, FormulaVersion: 1})
}
func (g gpuSearcher) SearchPoH(ctxTimeout time.Duration, req gpupoh.SearchRequest) (bool, uint64, float64, error) {
	res, sec, err := g.SearchPoHCollect(ctxTimeout, req)
	return res.Found, res.Nonce, sec, err
}
func (g gpuSearcher) SearchPoHCollect(ctxTimeout time.Duration, req gpupoh.SearchRequest) (gpupoh.SearchResult, float64, error) {
	t0 := time.Now()
	ctx := context.Background()
	var cancel context.CancelFunc
	if ctxTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, ctxTimeout)
		defer cancel()
	}
	res, err := gpupoh.SearchCollect(ctx, g.acc, req)
	sec := time.Since(t0).Seconds()
	if req.FormulaVersion < 3 {
		switch strings.ToLower(strings.TrimSpace(g.acc.Backend())) {
		case "cuda":
			if k := gpupoh.LastCUDAKernelSeconds(); k > 0 {
				sec = k
			}
		case "opencl":
			if k := gpupoh.LastOCLKernelSeconds(); k > 0 {
				sec = k
			}
		}
	} else if k := lastGPUKernelSeconds(); k > 0 {
		sec = k
	}
	return res, sec, err
}

func pickSearcher(preferredBackend string, preferredDevice int, disableGPU bool) (searcher, func(), string) {
	if disableGPU {
		return cpuSearcher{}, func() {}, "cpu"
	}
	accs, err := gpupoh.DiscoverAccelerators()
	if err == nil && len(accs) > 0 {
		var pick gpupoh.Accelerator
		for _, a := range accs {
			if preferredDevice >= 0 && a.DeviceIndex() != preferredDevice {
				continue
			}
			if preferredBackend != "" && !strings.EqualFold(strings.TrimSpace(a.Backend()), preferredBackend) {
				continue
			}
			pick = a
			break
		}
		if pick == nil {
			// Default: CUDA on NVIDIA when available, else OpenCL (AMD/Intel), else first device.
			order := []string{"cuda", "opencl"}
			if preferredBackend != "" && preferredBackend != "auto" {
				order = []string{strings.ToLower(preferredBackend)}
			}
			if strings.EqualFold(strings.TrimSpace(os.Getenv("HACKME_GPU_BACKEND")), "opencl") ||
				strings.TrimSpace(os.Getenv("HACKME_FORCE_OPENCL")) == "1" {
				order = []string{"opencl"}
			}
			for _, want := range order {
				for _, a := range accs {
					if strings.EqualFold(strings.TrimSpace(a.Backend()), want) {
						pick = a
						break
					}
				}
				if pick != nil {
					break
				}
			}
		}
		if pick == nil {
			pick = accs[0]
		}
		cleanup := func() {
			for _, a := range accs {
				_ = a.Close()
			}
		}
		return gpuSearcher{acc: pick}, cleanup, "gpu"
	}
	if preferredBackend == "cuda" || preferredBackend == "opencl" {
		fmt.Fprintf(os.Stderr, "workerpoh: WARN no %s accelerator found (%v); falling back to CPU — rebuild with: bash scripts/ops/build_cuda_worker.sh\n",
			preferredBackend, err)
	}
	return cpuSearcher{}, func() {}, "cpu"
}

// workerMeasureHashrateCapGHS is the worker-side report/measure ceiling (500).
// Coordinator clampWorkerHashrateGHS allows 512 so billing/retarget is not clipped
// below real multi-GPU peaks; keep this at 500 for honest per-worker telemetry.
const workerMeasureHashrateCapGHS = 500.0

func rawHashrateGHS(batch uint64, elapsedSec float64) float64 {
	if batch == 0 || elapsedSec <= 0 {
		return 0
	}
	ghs := float64(batch) / elapsedSec / 1e9
	if ghs > workerMeasureHashrateCapGHS {
		return workerMeasureHashrateCapGHS
	}
	return ghs
}

// measureWorkerHashrateGHS estimates GH/s from batch size and elapsed search time.
func measureWorkerHashrateGHS(batch uint64, elapsedSec float64) float64 {
	return measureWorkerHashrateGHSForFormula(batch, elapsedSec, 1)
}

// measureWorkerHashrateGHSForFormula prefers wall time for Blake3 (v3+): host Blake3
// does not refresh LastCUDAKernelSeconds, so stale v1 calib timing would otherwise
// advertise 50–80 GH/s while real Blake3 work is far slower.
func measureWorkerHashrateGHSForFormula(batch uint64, elapsedSec float64, formulaVersion int) float64 {
	if batch == 0 {
		return 0
	}
	wall := elapsedSec
	if formulaVersion >= 3 {
		if wall <= 0 {
			wall = effectiveSearchSeconds(elapsedSec)
		}
		return rawHashrateGHS(batch, wall)
	}
	elapsedSec = effectiveSearchSeconds(elapsedSec)
	return rawHashrateGHS(batch, elapsedSec)
}

func envFloatGHS(keys ...string) float64 {
	for _, k := range keys {
		v := strings.TrimSpace(os.Getenv(k))
		if v == "" {
			continue
		}
		f, err := strconv.ParseFloat(v, 64)
		if err == nil && f > 0 {
			return f
		}
	}
	return 0
}

// gpuHashrateFloorGHS is a minimum submit/calibration floor only — never a hard cap on measured GH/s.
func gpuHashrateFloorGHS() float64 {
	return envFloatGHS("HACKME_GPU_HASHRATE_FLOOR_GHS", "HACKME_CUDA_CALIBRATE_GHS")
}

func lastGPUKernelSeconds() float64 {
	if cudaBackendConfigured() {
		return gpupoh.LastCUDAKernelSeconds()
	}
	if openclBackendConfigured() {
		return gpupoh.LastOCLKernelSeconds()
	}
	return 0
}

func gpuKernelTimingActive() bool {
	return gpuBackendConfigured() && lastGPUKernelSeconds() > 0
}

func effectiveSearchSeconds(searchSec float64) float64 {
	if k := lastGPUKernelSeconds(); k > 0 {
		// Fast CUDA/OpenCL batches can be sub-millisecond; do not clamp to 50ms (that pinned ~0.08 GH/s).
		const kernelFloor = 1e-6
		if k < kernelFloor {
			return kernelFloor
		}
		return k
	}
	sec := searchSec
	const minSec = 0.05
	if sec < minSec {
		sec = minSec
	}
	return sec
}

var (
	submitHashrateEMA float64
	gpuCalibratedGHS  float64
	workerGPUBackend  string
)

func effectiveGPUBackend() string {
	b := strings.ToLower(strings.TrimSpace(workerGPUBackend))
	if b == "cuda" || b == "opencl" {
		return b
	}
	b = strings.ToLower(strings.TrimSpace(os.Getenv("HACKME_GPU_BACKEND")))
	if b == "cuda" || b == "opencl" {
		return b
	}
	return ""
}

func cudaBackendConfigured() bool {
	return effectiveGPUBackend() == "cuda"
}

func openclBackendConfigured() bool {
	return effectiveGPUBackend() == "opencl"
}

func gpuBackendConfigured() bool {
	return effectiveGPUBackend() != ""
}

func syncWorkerGPUBackendFromSearcher(srch searcher, mode string) {
	if mode != "gpu" {
		return
	}
	gs, ok := srch.(gpuSearcher)
	if !ok {
		return
	}
	b := strings.ToLower(strings.TrimSpace(gs.acc.Backend()))
	if b == "cuda" || b == "opencl" {
		workerGPUBackend = b
	}
}

func calibrateGPUHashrateGHS(srch searcher, batch, mod uint64) float64 {
	return calibrateGPUHashrateGHSForFormula(srch, batch, mod, 3)
}

// calibrateGPUHashrateGHSForFormula measures live search GH/s. Prefer formula 3 (Blake3)
// so pool stats after activation are not pinned to legacy 7n+13 GPU rates.
func calibrateGPUHashrateGHSForFormula(srch searcher, batch, mod uint64, formulaVersion int) float64 {
	if batch == 0 {
		return 0
	}
	if formulaVersion <= 0 {
		formulaVersion = 3
	}
	const warmup = 1
	const samples = 4
	var sum float64
	var n int
	calibReq := gpupoh.SearchRequest{
		Base: 1, Count: batch, Mod: mod,
		FormulaVersion: formulaVersion,
		PrevHash:       "00",
		MinerAddress:   "HMC-calib",
	}
	for i := 0; i < warmup+samples; i++ {
		_, _, wallSec, err := srch.SearchPoH(60*time.Second, calibReq)
		if err != nil {
			if os.Getenv("HACKME_CUDA_VERBOSE") == "1" {
				hotlog.Stderrf("workerpoh: calib search err: %v", err)
			}
			continue
		}
		sec := wallSec
		if formulaVersion < 3 {
			if k := gpupoh.LastCUDAKernelSeconds(); k > 0 {
				sec = k
			} else if k := gpupoh.LastOCLKernelSeconds(); k > 0 {
				sec = k
			}
		}
		if sec <= 0 {
			continue
		}
		if i < warmup {
			continue
		}
		g := rawHashrateGHS(batch, sec)
		if g >= 0.01 && g <= workerMeasureHashrateCapGHS {
			sum += g
			n++
		}
	}
	var measured float64
	if n > 0 {
		measured = sum / float64(n)
	}
	floor := gpuHashrateFloorGHS()
	if measured > 0 {
		if floor > 0 && measured < floor*0.5 {
			return floor
		}
		return measured
	}
	return floor
}

// submitHashrateGHS reports GH/s from GPU search time (batch / kernelSec).
// CUDA: dynamic EMA from measured kernel time; calibration is fallback only when timing is missing.
// OpenCL: when timing is bogus (too slow), fall back to calibrated/declared so workers are not under-paid.
// formulaVersion >= 3 (Blake3): never re-inflate from pre-Blake3 calib floors — report live wall rate.
func submitHashrateGHS(batch uint64, searchSec float64, mode string) float64 {
	return submitHashrateGHSForFormula(batch, searchSec, mode, 1)
}

func submitHashrateGHSForFormula(batch uint64, searchSec float64, mode string, formulaVersion int) float64 {
	inst := measureWorkerHashrateGHSForFormula(batch, searchSec, formulaVersion)
	kernelTimed := mode == "gpu" && gpuKernelTimingActive()
	floor := gpuHashrateFloorGHS()
	blake3 := formulaVersion >= 3
	if !blake3 && gpuBackendConfigured() && mode == "gpu" && !kernelTimed {
		if inst < 2 && gpuCalibratedGHS > 0 {
			inst = gpuCalibratedGHS
		} else if inst < gpuCalibratedGHS*0.7 && gpuCalibratedGHS > 0 {
			inst = gpuCalibratedGHS
		}
	} else if !blake3 && gpuBackendConfigured() && mode == "gpu" && kernelTimed && openclBackendConfigured() && !cudaBackendConfigured() {
		// OpenCL-only: reject bogus slow wall/kernel timing; trust calibration/EMA.
		if inst < 2 && gpuCalibratedGHS > 0 {
			inst = gpuCalibratedGHS
		} else if inst < gpuCalibratedGHS*0.7 && gpuCalibratedGHS > 0 {
			inst = gpuCalibratedGHS
		}
	} else if !blake3 && cudaBackendConfigured() && kernelTimed && gpuCalibratedGHS > 0 && inst > gpuCalibratedGHS*2.5 {
		// Cap absurd over-report spikes; trust live measurement otherwise.
		inst = gpuCalibratedGHS * 1.15
	} else if !blake3 && cudaBackendConfigured() && kernelTimed && gpuCalibratedGHS > 10 && inst > 0 && inst < gpuCalibratedGHS*0.25 {
		// Desktop GPU contention (browser/IDE): avoid pinning pool stats and coordinator
		// rate limits to a one-off slow kernel sample.
		contentionFloor := gpuCalibratedGHS * 0.55
		if inst < contentionFloor {
			inst = contentionFloor
		}
	}
	if inst > 0 {
		if submitHashrateEMA <= 0 {
			submitHashrateEMA = inst
		} else if cudaBackendConfigured() && kernelTimed {
			// CUDA: track live kernel timing; decay quickly when performance drops so pool
			// stats are not stuck on an early calibration spike.
			if inst < submitHashrateEMA*0.6 {
				submitHashrateEMA = 0.75*inst + 0.25*submitHashrateEMA
			} else {
				submitHashrateEMA = 0.55*inst + 0.45*submitHashrateEMA
			}
		} else {
			submitHashrateEMA = 0.35*inst + 0.65*submitHashrateEMA
		}
	}
	ghs := inst
	if blake3 && inst > 0 {
		// Blake3: always prefer live wall/device rate (no sticky pre-cliff EMA).
		ghs = inst
	} else if cudaBackendConfigured() && kernelTimed && inst > 0 {
		// Prefer live measurement for pool payout; EMA is diagnostic only.
		ghs = inst
	} else if submitHashrateEMA > ghs {
		ghs = submitHashrateEMA
	}
	declared := envFloatGHS("HASHRATE_GHS", "HACKME_WORKER_HASHRATE_GHS", "HACKME_WORKER_DECLARED_HASHRATE_GHS")
	if gpuBackendConfigured() {
		if !blake3 {
			if ghs <= 0 && gpuCalibratedGHS > 0 {
				ghs = gpuCalibratedGHS
			}
			if floor > 0 && ghs > 0 && ghs < floor {
				ghs = floor
			}
		}
		if ghs > workerMeasureHashrateCapGHS {
			return workerMeasureHashrateCapGHS
		}
		return ghs
	}
	if declared <= 0 {
		if ghs > workerMeasureHashrateCapGHS {
			return workerMeasureHashrateCapGHS
		}
		return ghs
	}
	if ghs > 0 {
		if ghs > workerMeasureHashrateCapGHS {
			return workerMeasureHashrateCapGHS
		}
		return ghs
	}
	if declared > workerMeasureHashrateCapGHS {
		return workerMeasureHashrateCapGHS
	}
	return declared
}

func sanitizeWorkerHostname(host string) string {
	return workerid.SanitizeHostname(host)
}

func main() {
	// -hackme-wasm-validate is handled in sandbox.init before flag.Parse.
	// #1: never let AV/disk stalls on the log fd freeze Search/submit.
	hotlog.Install()

	var (
		coordURL        = flag.String("coord", strings.TrimSpace(os.Getenv("COORD_URL")), "coordinator base URL")
		token           = flag.String("token", strings.TrimSpace(os.Getenv("COORD_TOKEN")), "coordinator admin token")
		workerID        = flag.String("worker", strings.TrimSpace(os.Getenv("WORKER_ID")), "worker id")
		batch           = flag.Uint64("batch", 1<<24, "claim batch size")
		gpuChunk        = flag.Uint64("gpu-chunk", envUint64("GPU_CHUNK", 1<<24), "GPU chunk size per Search() call (env GPU_CHUNK; Blake3 prefer 2^24–2^26)")
		searchTimeoutMS = flag.Int("search-timeout-ms", envIntPositive("SEARCH_TIMEOUT_MS", 2500), "Search() timeout per GPU chunk (ms) (env SEARCH_TIMEOUT_MS)")
		gpuBackend      = flag.String("gpu-backend", strings.TrimSpace(os.Getenv("HACKME_GPU_BACKEND")), "preferred GPU backend: auto|opencl|cuda")
		gpuDevice       = flag.Int("gpu-device", -1, "preferred accelerator device index (-1 = auto)")
		gpuDisable      = flag.Bool("gpu-disable", isTruthy(os.Getenv("HACKME_GPU_DISABLE")), "disable GPU and force CPU mode")
	)
	flag.Parse()
	workerGPUBackend = strings.ToLower(strings.TrimSpace(*gpuBackend))

	if *token == "" {
		*token = strings.TrimSpace(os.Getenv("COORD_ADMIN_TOKEN"))
	}
	if *token == "" {
		*token = strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_ADMIN_TOKEN"))
	}
	if *token == "" {
		*token = readCoordinatorTokenFromSecrets()
	}
	if *coordURL == "" {
		fmt.Fprintln(os.Stderr, "set COORD_URL or -coord")
		os.Exit(2)
	}
	if *token == "" {
		fmt.Fprintln(os.Stderr, "set COORD_TOKEN, COORD_ADMIN_TOKEN, or -token")
		os.Exit(2)
	}
	if *workerID == "" {
		hn, _ := os.Hostname()
		if hn == "" {
			hn = "local"
		}
		*workerID = "worker-" + sanitizeWorkerHostname(hn)
	}
	if !workerfuzzloop.Truthy(os.Getenv("HACKME_WORKER_SKIP_INSTANCE_LOCK")) {
		g, err := workerlock.Acquire("workerpoh", *workerID, strings.TrimSpace(os.Getenv("HACKME_WORKER_LOCK_DIR")))
		if err != nil {
			if errors.Is(err, workerlock.ErrAlreadyRunning) {
				fmt.Fprintf(os.Stderr, "workerpoh: %v\n", err)
				// Non-zero so the node Wait() path clears running state instead of
				// treating a lock collision as a clean exit (orphan DoS after node crash).
				os.Exit(2)
			}
			fmt.Fprintf(os.Stderr, "workerpoh: instance lock: %v\n", err)
			os.Exit(1)
		}
		defer g.Release()
	}

	priv, pubHex, claimMinerAddr, signHybrid, err := loadHybridSigningMaterial()
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad signing material:", err.Error())
		os.Exit(2)
	}
	if !signHybrid {
		fmt.Fprintln(os.Stderr, "workerpoh: unsigned submits (set HACKME_MINER_ED25519_SEED_HEX when the pool requires hybrid signatures)")
	}
	nonceFile := strings.TrimSpace(os.Getenv("HACKME_MINER_NONCE_FILE"))
	if nonceFile == "" {
		safeID := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
				return r
			}
			return '_'
		}, strings.TrimSpace(*workerID))
		if safeID == "" {
			safeID = "default"
		}
		nonceFile = filepath.Join("logs", "miner_submit_nonce."+safeID+".seq")
	}
	workerName := strings.TrimSpace(os.Getenv("WORKER_NAME"))
	if workerName == "" {
		workerName = *workerID
	}

	claimCL := newWorkerHTTPClient(workerHTTPDuration("HACKME_WORKER_CLAIM_TIMEOUT", 60*time.Second))
	submitCL := newWorkerHTTPClient(workerHTTPDuration("HACKME_WORKER_SUBMIT_TIMEOUT", 90*time.Second))
	pushCL := newWorkerHTTPClient(workerHTTPDuration("HACKME_WORKER_PUSH_TIMEOUT", 20*time.Second))
	var netBackoff time.Duration = 2 * time.Second
	preferredBackend := strings.ToLower(strings.TrimSpace(*gpuBackend))
	if preferredBackend == "auto" {
		preferredBackend = ""
	}
	srch, cleanup, mode := pickSearcher(preferredBackend, *gpuDevice, *gpuDisable)
	defer cleanup()
	syncWorkerGPUBackendFromSearcher(srch, mode)
	fmt.Fprintf(os.Stderr, "workerpoh: searcher=%s mode=%s backend=%s hybrid_sign=%v chunk=%d search_timeout_ms=%d\n",
		srch.Label(), mode, effectiveGPUBackend(), signHybrid, *gpuChunk, *searchTimeoutMS)
	if mode == "gpu" && gpuBackendConfigured() {
		calibMod := uint64(19_485_298)
		if v := strings.TrimSpace(os.Getenv("HACKME_GPU_CALIBRATE_MOD")); v != "" {
			if x, err := strconv.ParseUint(v, 10, 64); err == nil && x > 0 {
				calibMod = x
			}
		} else if v := strings.TrimSpace(os.Getenv("HACKME_CUDA_CALIBRATE_MOD")); v != "" {
			if x, err := strconv.ParseUint(v, 10, 64); err == nil && x > 0 {
				calibMod = x
			}
		}
		gpuCalibratedGHS = calibrateGPUHashrateGHS(srch, *batch, calibMod)
		if gpuCalibratedGHS > 0 {
			backend := strings.TrimSpace(workerGPUBackend)
			if backend == "" {
				backend = "gpu"
			}
			fmt.Fprintf(os.Stderr, "workerpoh: %s calibrated %.2f GH/s (batch=%d)\n", strings.ToUpper(backend), gpuCalibratedGHS, *batch)
		}
	}
	hybridFuzz := startHybridFuzzIfEnabled(*coordURL, *token, *workerID)
	if hybridFuzz != nil {
		defer hybridFuzz.cancel()
		hybridFuzz.notePoHGHS(0, gpuCalibratedGHS)
	}
	var okSubmits int64
	for {
		// claim — include hybrid identity so coordinator can reject claim-as-victim
		// (shared pool token + empty identity abuse / temp-ban poisoning).
		claimBody := map[string]any{"worker_id": *workerID, "batch_size": *batch}
		if signHybrid && pubHex != "" {
			claimBody["miner_pubkey_ed25519"] = pubHex
			if claimMinerAddr != "" {
				claimBody["miner_address"] = claimMinerAddr
			}
		}
		cb, _ := json.Marshal(claimBody)
		req, _ := http.NewRequest(http.MethodPost, strings.TrimRight(*coordURL, "/")+"/api/work/claim", bytes.NewReader(cb))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hackme-Admin-Token", *token)
		res, err := claimCL.Do(req)
		if err != nil {
			hotlog.Stderrf("claim error: %v", err)
			sleepWorkerBackoff("claim", &netBackoff)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		_ = res.Body.Close()
		var cr claimResp
		_ = json.Unmarshal(body, &cr)
		if res.StatusCode != 200 || !cr.OK {
			reason := strings.TrimSpace(cr.Reason)
			if res.StatusCode >= 500 || res.StatusCode == 520 || res.StatusCode == 522 {
				// 520/522 are often Cloudflare/proxy origin timeouts — not coordinator JSON reject reasons.
				hotlog.Stderrf("claim transport error: http %d (coordinator/proxy timeout or overload; not a pool ban)", res.StatusCode)
				sleepWorkerBackoff("claim", &netBackoff)
			} else {
				hotlog.Stderrf("claim rejected: http %d reason=%q", res.StatusCode, reason)
				rl := strings.ToLower(reason)
				if strings.Contains(rl, "banned") || strings.Contains(rl, "rate") || res.StatusCode == 429 {
					sleepWorkerBackoff("claim", &netBackoff)
				} else {
					time.Sleep(2 * time.Second)
				}
			}
			continue
		}
		netBackoff = 2 * time.Second

		var found bool
		var foundNonce uint64
		elapsed := 0.0
		var searched uint64
		fv := cr.PoHFormulaVersion
		if fv <= 0 {
			fv = 1
		}
		minerForPoH := strings.TrimSpace(cr.MinerAddress)
		if minerForPoH == "" {
			minerForPoH = claimMinerAddr
		}
		searchReq := func(base, count uint64) gpupoh.SearchRequest {
			return gpupoh.SearchRequest{
				Base: base, Count: count, Mod: cr.TargetMod,
				FormulaVersion: fv, PrevHash: cr.PrevHash, MinerAddress: minerForPoH,
				ShareMod: cr.ShareMod, MaxShares: 128,
			}
		}
		var collectedShares []uint64
		mergeShares := func(more []uint64) {
			if len(more) == 0 {
				return
			}
			seen := map[uint64]struct{}{}
			for _, s := range collectedShares {
				seen[s] = struct{}{}
			}
			for _, s := range more {
				if len(collectedShares) >= 128 {
					return
				}
				if _, ok := seen[s]; ok {
					continue
				}
				seen[s] = struct{}{}
				collectedShares = append(collectedShares, s)
			}
		}
		// Chunked search: for GPU we keep each call bounded; for CPU this is one pass.
		// Shares are harvested during the same hash pass (GPU kernel / host Blake3SearchCollect).
		leaseDeadline := time.Time{}
		if cr.LeaseExpiresAt > 0 {
			// Leave 2s for submit/sign RTT so we don't burn expired leases.
			leaseDeadline = time.Unix(cr.LeaseExpiresAt-2, 0)
		}
		pastLease := func() bool {
			return !leaseDeadline.IsZero() && time.Now().After(leaseDeadline)
		}
		if mode == "gpu" {
			remain := cr.BatchSize
			cur := cr.BaseNonce
			chunk := *gpuChunk
			if chunk < 1024 {
				chunk = 1024
			}
			for remain > 0 {
				if pastLease() {
					hotlog.Stderrf("workerpoh: lease deadline near — submit with %d shares searched=%d", len(collectedShares), searched)
					break
				}
				n := chunk
				if n > remain {
					n = remain
				}
				res, sec, err := srch.SearchPoHCollect(time.Duration(*searchTimeoutMS)*time.Millisecond, searchReq(cur, n))
				elapsed += sec
				searched += n
				mergeShares(res.Shares)
				// Enough shares for PPS — don't burn the rest of a huge lease into expiry.
				if cr.ShareMod > 0 && len(collectedShares) >= 32 {
					if found || !cr.ShareStrict {
						break
					}
					// Still hunting a block hit is optional; prefer timely submit under strict.
					if len(collectedShares) >= 64 {
						break
					}
				}
				if err != nil {
					// On transient GPU errors, fallback to CPU for this claim (preserve session; log for coordinator tail).
					class := gputune.ClassifyGPUFailure(err)
					if gputune.ShouldCPUFallback(class) {
						hotlog.Stderrf("workerpoh: %s", gputune.FormatWorkerGPUEvent(*workerID, workerGPUBackend, class, err))
					}
					fb, errFB := gpupoh.Blake3SearchCollect(context.Background(), searchReq(cur, n))
					if errFB == nil {
						mergeShares(fb.Shares)
						if fb.Found {
							found, foundNonce = true, fb.Nonce
							// Keep scanning remaining chunks when ShareMod set so PPS reflects work.
							if cr.ShareMod == 0 {
								break
							}
						}
					} else {
						f2, nonce2 := findHitCPUPoH(cur, n, cr.TargetMod, fv, cr.PrevHash, minerForPoH)
						if f2 {
							found, foundNonce = true, nonce2
							if cr.ShareMod == 0 {
								break
							}
						}
					}
				} else if res.Found {
					found, foundNonce = true, res.Nonce
					// Soft / no shares: stop early. Strict PPS: finish lease for share proofs.
					if cr.ShareMod == 0 {
						break
					}
				}
				cur += n
				remain -= n
			}
		} else {
			res, sec, err := srch.SearchPoHCollect(0, searchReq(cr.BaseNonce, cr.BatchSize))
			_ = err
			elapsed = sec
			searched = res.Scanned
			if searched == 0 {
				searched = cr.BatchSize
			}
			found, foundNonce = res.Found, res.Nonce
			mergeShares(res.Shares)
		}
		hashBatch := cr.BatchSize
		if searched > 0 && searched < hashBatch {
			hashBatch = searched
		}
		instGHS := measureWorkerHashrateGHSForFormula(hashBatch, elapsed, fv)
		ghs := submitHashrateGHSForFormula(hashBatch, elapsed, mode, fv)
		if os.Getenv("HACKME_CUDA_VERBOSE") == "1" && mode == "gpu" {
			kern := lastGPUKernelSeconds()
			hotlog.VerboseStderrf("workerpoh: search_sec=%.6f kernel_sec=%.6f fv=%d inst_ghs=%.2f submit_ghs=%.2f calib=%.2f",
				elapsed, kern, fv, instGHS, ghs, gpuCalibratedGHS)
		}
		wasmGatePass := false
		if found && strings.TrimSpace(cr.WasmCheckHex) != "" && strings.TrimSpace(cr.OrderTaskID) != "" {
			raw, err := hex.DecodeString(strings.TrimSpace(cr.WasmCheckHex))
			if err == nil && len(raw) > 0 {
				ok, err := sandbox.InvokeCheck(context.Background(), raw, foundNonce)
				if err == nil && ok {
					wasmGatePass = true
				} else if err != nil {
					hotlog.Stderrf("workerpoh: wasm gate err: %v", err)
				} else {
					hotlog.Stderrf("workerpoh: wasm gate rejected nonce=%d (order %s)", foundNonce, cr.OrderTaskID)
					found = false
				}
			}
		}
		out := submitReq{
			WorkerID:    *workerID,
			BaseNonce:   cr.BaseNonce,
			BatchSize:   cr.BatchSize,
			WorkID:      cr.WorkID,
			Attempts:    hashBatch, // honest: only nonces actually hashed (CPU chunk / lease abort)
			Found:       found,
			FoundNonce:  foundNonce,
			HashrateGHS: ghs,
		}
		if cr.ShareMod > 0 {
			// Prefer shares from the main hash pass. Never submit a block-found nonce as a
			// share unless it also satisfies share_mod (otherwise share_rejects inflate).
			// Re-verify on host before submit so GPU false positives never inflate share_rejects.
			if len(collectedShares) > 0 {
				out.ShareNonces = filterVerifiedShares(collectedShares, cr.BaseNonce, cr.BatchSize, cr.ShareMod, fv, cr.PrevHash, minerForPoH, 128)
			}
			// SHARE_STRICT rejects non-found submits with zero valid shares. CPU/OpenCL
			// paths (and early-found leases) can finish with an empty collectedShares
			// slice when share_scan_max was omitted from the claim — always harvest a
			// capped stride proof so standalone workerpoh matches worker_loop+shareharvest.
			if len(out.ShareNonces) == 0 {
				scanMax := cr.ShareScanMax
				if scanMax == 0 {
					scanMax = uint64(256 * 1024)
				}
				out.ShareNonces = collectShareNonces(cr.BaseNonce, cr.BatchSize, cr.ShareMod, fv, cr.PrevHash, minerForPoH, 128, foundNonce, found, scanMax)
			}
		}
		if strings.TrimSpace(cr.OrderTaskID) != "" {
			out.OrderTaskID = strings.TrimSpace(cr.OrderTaskID)
			out.WasmGatePass = wasmGatePass
		}
		if found {
			sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", *workerID, foundNonce)))
			out.ResultHash = hex.EncodeToString(sum[:])
		}
		if signHybrid {
			submitNonce, err := loadAndBumpSubmitNonce(nonceFile)
			if err != nil {
				hotlog.Stderrf("nonce file error: %v", err)
				time.Sleep(1 * time.Second)
				continue
			}
			out.MinerPubKey = pubHex
			out.MinerSigAlg = "ed25519"
			out.SubmitNonce = submitNonce
			sig := ed25519.Sign(priv, canonicalSubmitBytes(out))
			out.MinerSig = hex.EncodeToString(sig)
		}

		sb, _ := json.Marshal(out)
		sreq, _ := http.NewRequest(http.MethodPost, strings.TrimRight(*coordURL, "/")+"/api/work/submit", bytes.NewReader(sb))
		sreq.Header.Set("Content-Type", "application/json")
		sreq.Header.Set("X-Hackme-Admin-Token", *token)
		sres, err := submitCL.Do(sreq)
		if err != nil {
			hotlog.Stderrf("submit error: %v", err)
			sleepWorkerBackoff("submit", &netBackoff)
			continue
		}
		sbody, _ := io.ReadAll(io.LimitReader(sres.Body, 1<<20))
		_ = sres.Body.Close()
		if sres.StatusCode != 200 {
			hotlog.Stderrf("submit http: %d %s", sres.StatusCode, string(sbody))
			if signHybrid && (strings.Contains(string(sbody), `"reason":"replay"`) || strings.Contains(string(sbody), "duplicate_signed_payload")) {
				// Shared miner key across rigs: bump local seq so next submit_nonce exceeds coordinator max.
				_ = os.WriteFile(nonceFile, []byte(strconv.FormatUint(uint64(time.Now().Unix())*1000, 10)), 0o644)
			}
			time.Sleep(1 * time.Second)
			continue
		}
		var subWrap struct {
			OK       bool `json:"ok"`
			Accepted bool `json:"accepted"`
		}
		_ = json.Unmarshal(sbody, &subWrap)
		if subWrap.OK {
			okSubmits++
		}
		pushWorkSnapshot(pushCL, *coordURL, *token, *workerID, workerName, ghs, subWrap.Accepted, okSubmits)
		if hybridFuzz != nil {
			hybridFuzz.notePoHGHS(ghs, gpuCalibratedGHS)
		}
		hotlog.Stdoutf("submit ok found=%v batch=%d mod=%d ghs=%.6f inst_ghs=%.2f t_ms=%d", found, cr.BatchSize, cr.TargetMod, ghs, instGHS, time.Now().UnixMilli())
		if ms := effectiveClaimCooldownMS(mode, ghs); ms > 0 {
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}
	}
}
