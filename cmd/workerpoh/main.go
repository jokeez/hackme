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
	"hackme/internal/worksubmit"
)

// Simple PoH worker:
// - claims nonce ranges from coordinator
// - searches for a valid nonce in the range (CPU baseline)
// - submits found hit with hybrid signature (optional but recommended / often required)
//
// Build:
//   go build -o workerpoh ./cmd/workerpoh
//
// Run:
//   COORD_URL=https://hackme.tech/pool/coordinator COORD_TOKEN=... WORKER_ID=rig-01 \
//   HACKME_MINER_ED25519_SEED_HEX=... ./workerpoh
//
// Or place coordinator admin in .secrets/hackme_coordinator_admin_token (one line) and set only COORD_URL + miner seed.
//
// Note: coordinator validates PoH hit as eval_v1(n)=7n+13; eval(n)%target_mod==0.

type claimResp struct {
	OK        bool   `json:"ok"`
	Reason    string `json:"reason,omitempty"`
	WorkerID  string `json:"worker_id,omitempty"`
	BaseNonce uint64 `json:"base_nonce"`
	BatchSize uint64 `json:"batch_size"`
	WorkID    string `json:"work_id,omitempty"`
	TargetMod uint64 `json:"target_mod,omitempty"`
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

	MinerPubKey string `json:"miner_pubkey_ed25519,omitempty"`
	MinerSig    string `json:"miner_sig_ed25519,omitempty"`
	MinerSigAlg string `json:"miner_sig_alg,omitempty"`
	SubmitNonce uint64 `json:"submit_nonce,omitempty"`
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
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(path, []byte("1"), 0o644); err != nil {
				return 0, err
			}
			return 1, nil
		}
		return 0, err
	}
	cur, _ := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	next := cur + 1
	if next == 0 {
		next = 1
	}
	if err := os.WriteFile(path, []byte(strconv.FormatUint(next, 10)), 0o644); err != nil {
		return 0, err
	}
	return next, nil
}

func findHitCPU(base, batch, mod uint64) (bool, uint64) {
	end := base + batch
	if end < base {
		end = ^uint64(0)
	}
	for n := base; n < end; n++ {
		if chain.PohEval(n)%mod == 0 {
			return true, n
		}
	}
	return false, 0
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

func coordinatorSecretPaths() []string {
	var out []string
	if root := strings.TrimSpace(os.Getenv("HACKME_REPO_ROOT")); root != "" {
		out = append(out, filepath.Join(root, ".secrets", "hackme_coordinator_admin_token"))
	}
	out = append(out, filepath.Join(".secrets", "hackme_coordinator_admin_token"))
	return out
}

// readCoordinatorTokenFromSecrets matches scripts/ops/worker_loop.sh (one line, no quotes).
func readCoordinatorTokenFromSecrets() string {
	for _, p := range coordinatorSecretPaths() {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		line := strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
		if line != "" {
			return line
		}
	}
	return ""
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

// loadHybridSigningMaterial returns (priv, pubHex, true) when HACKME_MINER_ED25519_SEED_HEX is set and valid.
func loadHybridSigningMaterial() (ed25519.PrivateKey, string, bool, error) {
	seedHex := strings.TrimSpace(os.Getenv("HACKME_MINER_ED25519_SEED_HEX"))
	if seedHex == "" {
		return nil, "", false, nil
	}
	seed, err := hex.DecodeString(seedHex)
	if err != nil || len(seed) != 32 {
		return nil, "", false, errors.New("HACKME_MINER_ED25519_SEED_HEX must be 32-byte hex (64 chars)")
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	return priv, hex.EncodeToString(pub), true, nil
}

type searcher interface {
	Label() string
	Search(ctxTimeout time.Duration, base, count, mod uint64) (found bool, nonce uint64, elapsedSec float64, err error)
}

type cpuSearcher struct{}

func (cpuSearcher) Label() string { return "cpu" }
func (cpuSearcher) Search(ctxTimeout time.Duration, base, count, mod uint64) (bool, uint64, float64, error) {
	t0 := time.Now()
	found, nonce := findHitCPU(base, count, mod)
	return found, nonce, time.Since(t0).Seconds(), nil
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
	fmt.Fprintf(os.Stderr, "%s: backing off %s (coordinator/network)\n", kind, wait.Round(time.Millisecond))
	time.Sleep(wait)
	next := wait * 2
	if next > 45*time.Second {
		next = 45 * time.Second
	}
	*backoff = next
}

func (g gpuSearcher) Search(ctxTimeout time.Duration, base, count, mod uint64) (bool, uint64, float64, error) {
	t0 := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
	defer cancel()
	found, nonce, err := g.acc.Search(ctx, base, count, mod)
	return found, nonce, time.Since(t0).Seconds(), err
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
			// Default selection for mixed rigs: prefer OpenCL (better AMD coverage),
			// then first available accelerator.
			for _, a := range accs {
				if strings.EqualFold(strings.TrimSpace(a.Backend()), "opencl") {
					pick = a
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
	return cpuSearcher{}, func() {}, "cpu"
}

func main() {
	var (
		coordURL        = flag.String("coord", strings.TrimSpace(os.Getenv("COORD_URL")), "coordinator base URL")
		token           = flag.String("token", strings.TrimSpace(os.Getenv("COORD_TOKEN")), "coordinator admin token")
		workerID        = flag.String("worker", strings.TrimSpace(os.Getenv("WORKER_ID")), "worker id")
		batch           = flag.Uint64("batch", 1<<22, "claim batch size")
		gpuChunk        = flag.Uint64("gpu-chunk", 1<<22, "GPU chunk size per Search() call")
		searchTimeoutMS = flag.Int("search-timeout-ms", 2500, "Search() timeout per GPU chunk (ms)")
		gpuBackend      = flag.String("gpu-backend", strings.TrimSpace(os.Getenv("HACKME_GPU_BACKEND")), "preferred GPU backend: auto|opencl|cuda")
		gpuDevice       = flag.Int("gpu-device", -1, "preferred accelerator device index (-1 = auto)")
		gpuDisable      = flag.Bool("gpu-disable", isTruthy(os.Getenv("HACKME_GPU_DISABLE")), "disable GPU and force CPU mode")
	)
	flag.Parse()

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
			hn = "worker"
		}
		*workerID = "worker-" + hn
	}

	priv, pubHex, signHybrid, err := loadHybridSigningMaterial()
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

	claimCL := newWorkerHTTPClient(workerHTTPDuration("HACKME_WORKER_CLAIM_TIMEOUT", 35*time.Second))
	submitCL := newWorkerHTTPClient(workerHTTPDuration("HACKME_WORKER_SUBMIT_TIMEOUT", 90*time.Second))
	pushCL := newWorkerHTTPClient(workerHTTPDuration("HACKME_WORKER_PUSH_TIMEOUT", 20*time.Second))
	var netBackoff time.Duration = 2 * time.Second
	preferredBackend := strings.ToLower(strings.TrimSpace(*gpuBackend))
	if preferredBackend == "auto" {
		preferredBackend = ""
	}
	srch, cleanup, mode := pickSearcher(preferredBackend, *gpuDevice, *gpuDisable)
	defer cleanup()
	fmt.Fprintf(os.Stderr, "workerpoh: searcher=%s mode=%s hybrid_sign=%v\n", srch.Label(), mode, signHybrid)
	var okSubmits int64
	for {
		// claim
		claimBody := map[string]any{"worker_id": *workerID, "batch_size": *batch}
		cb, _ := json.Marshal(claimBody)
		req, _ := http.NewRequest(http.MethodPost, strings.TrimRight(*coordURL, "/")+"/api/work/claim", bytes.NewReader(cb))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hackme-Admin-Token", *token)
		res, err := claimCL.Do(req)
		if err != nil {
			fmt.Fprintln(os.Stderr, "claim error:", err)
			sleepWorkerBackoff("claim", &netBackoff)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		_ = res.Body.Close()
		var cr claimResp
		_ = json.Unmarshal(body, &cr)
		if res.StatusCode != 200 || !cr.OK {
			fmt.Fprintln(os.Stderr, "claim rejected:", res.StatusCode, cr.Reason)
			reason := strings.ToLower(strings.TrimSpace(cr.Reason))
			if strings.Contains(reason, "banned") || strings.Contains(reason, "rate") {
				sleepWorkerBackoff("claim", &netBackoff)
			} else {
				time.Sleep(2 * time.Second)
			}
			continue
		}
		netBackoff = 2 * time.Second

		var found bool
		var foundNonce uint64
		elapsed := 0.0
		// Chunked search: for GPU we keep each call bounded; for CPU this is one pass.
		if mode == "gpu" {
			remain := cr.BatchSize
			cur := cr.BaseNonce
			chunk := *gpuChunk
			if chunk < 1024 {
				chunk = 1024
			}
			for remain > 0 {
				n := chunk
				if n > remain {
					n = remain
				}
				f, nonce, sec, err := srch.Search(time.Duration(*searchTimeoutMS)*time.Millisecond, cur, n, cr.TargetMod)
				elapsed += sec
				if err != nil {
					// On transient GPU errors, fallback to CPU for this claim.
					f2, nonce2 := findHitCPU(cur, n, cr.TargetMod)
					if f2 {
						found, foundNonce = true, nonce2
						break
					}
				}
				if f {
					found, foundNonce = true, nonce
					break
				}
				cur += n
				remain -= n
			}
		} else {
			var err error
			found, foundNonce, elapsed, err = srch.Search(0, cr.BaseNonce, cr.BatchSize, cr.TargetMod)
			_ = err
		}
		ghs := 0.0
		if elapsed > 0.01 {
			ghs = float64(cr.BatchSize) / elapsed / 1e9
		}
		if ghs > 500 {
			ghs = 500
		} else if ghs > 0 && ghs < 1e-6 {
			ghs = 0
		}
		out := submitReq{
			WorkerID:    *workerID,
			BaseNonce:   cr.BaseNonce,
			BatchSize:   cr.BatchSize,
			WorkID:      cr.WorkID,
			Attempts:    cr.BatchSize,
			Found:       found,
			FoundNonce:  foundNonce,
			HashrateGHS: ghs,
		}
		if found {
			sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", *workerID, foundNonce)))
			out.ResultHash = hex.EncodeToString(sum[:])
		}
		if signHybrid {
			submitNonce, err := loadAndBumpSubmitNonce(nonceFile)
			if err != nil {
				fmt.Fprintln(os.Stderr, "nonce file error:", err)
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
			fmt.Fprintln(os.Stderr, "submit error:", err)
			sleepWorkerBackoff("submit", &netBackoff)
			continue
		}
		sbody, _ := io.ReadAll(io.LimitReader(sres.Body, 1<<20))
		_ = sres.Body.Close()
		if sres.StatusCode != 200 {
			fmt.Fprintln(os.Stderr, "submit http:", sres.StatusCode, string(sbody))
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
		fmt.Printf("submit ok found=%v batch=%d mod=%d ghs=%.6f\n", found, cr.BatchSize, cr.TargetMod, ghs)
		if ms := envIntMs("HACKME_WORKER_CLAIM_COOLDOWN_MS", 0); ms > 0 {
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}
	}
}
