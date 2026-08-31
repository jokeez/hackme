// Package workerfuzzloop runs coordinator fuzz claim/submit digs.
// Used by cmd/workerfuzz and by workerpoh when HACKME_WORKER_HYBRID_FUZZ=1.
package workerfuzzloop

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"hackme/internal/chain"
	"hackme/internal/fuzzengine"
	"hackme/internal/poolfuzz"
	"hackme/internal/sandbox"
)

// ClaimResp is one leased fuzz work item from the coordinator.
type ClaimResp struct {
	OK                   bool             `json:"ok"`
	Reason               string           `json:"reason,omitempty"`
	WorkID               string           `json:"work_id,omitempty"`
	CampaignID           string           `json:"campaign_id,omitempty"`
	ItemID               int64            `json:"item_id,omitempty"`
	InputN               uint64           `json:"input_n,omitempty"`
	ActualInput          uint64           `json:"actual_input,omitempty"`
	InputMode            string           `json:"input_mode,omitempty"`
	InputBytesHex        string           `json:"input_bytes_hex,omitempty"`
	DepthTier            string           `json:"depth_tier,omitempty"`
	PerRunHMC            float64          `json:"per_run_hmc,omitempty"`
	ExecPerUnit          int              `json:"exec_per_unit,omitempty"`
	MaxInputBytes        int              `json:"max_input_bytes,omitempty"`
	CoverageKind         string           `json:"coverage_kind,omitempty"`
	WasmCheckHex         string           `json:"wasm_check_hex,omitempty"`
	CheckSemantics       string           `json:"check_semantics,omitempty"`
	CorpusSeeds          []map[string]any `json:"corpus_seeds,omitempty"`
	CorpusSnapshotSHA256 string           `json:"corpus_snapshot_sha256,omitempty"`
	TaskClass            string           `json:"task_class,omitempty"`
	WorkKind             string           `json:"work_kind,omitempty"`
	HarnessHash          string           `json:"harness_hash,omitempty"`
	UpstreamTargetID     string           `json:"upstream_target_id,omitempty"`
	HuntSource           string           `json:"hunt_source,omitempty"`
	HuntPinPath          string           `json:"hunt_pin_path,omitempty"`
	HuntSourceRel        string           `json:"hunt_source_rel,omitempty"`
	HarnessFetchURL      string           `json:"harness_fetch_url,omitempty"`
	HuntDetectLeaks      bool             `json:"hunt_detect_leaks,omitempty"`
}

// Config drives a supervised fuzz dig loop.
type Config struct {
	CoordURL   string
	Token      string
	WorkerID   string
	MinerAddr  string
	TimeoutMS  int
	HTTPClient *http.Client

	// Priv/PubHex enable hybrid Ed25519 submits (required for production pools).
	Priv   ed25519.PrivateKey
	PubHex string
	Hybrid bool

	// Concurrency is max in-flight claim→run→submit cycles (default 1).
	Concurrency int
	// MinClaimGap is floor sleep between successful claim starts (default 50ms).
	MinClaimGap time.Duration
	LogPrefix   string

	// PohGHSMilli stores milli-GH/s (ghs*1000) for lock-free reads; optional backpressure.
	PohGHSMilli *atomic.Int64
	// CalibGHSMilli is calibrated/peak PoH milli-GH/s for backpressure baseline.
	CalibGHSMilli *atomic.Int64
	// BackpressureFloorPct pauses fuzz when PoH GH/s < floor% of calib (default 35).
	BackpressureFloorPct int
}

// Stats are best-effort counters for diagnostics.
type Stats struct {
	ClaimsOK   atomic.Int64
	SubmitsOK  atomic.Int64
	Findings   atomic.Int64
	PausedBack atomic.Int64
	Panics     atomic.Int64
}

// Truthy reports common truthy env strings.
func Truthy(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// HybridFuzzEnabled is true unless HACKME_WORKER_HYBRID_FUZZ is explicitly off.
// Fleet default is ON (inline dig under the same worker_id). Escape hatch: =0|false|no|off.
func HybridFuzzEnabled() bool {
	v := strings.TrimSpace(os.Getenv("HACKME_WORKER_HYBRID_FUZZ"))
	if v == "" {
		return true
	}
	return !Falsy(v)
}

// Falsy is true for common off spellings (0, false, no, off).
func Falsy(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	return v == "0" || v == "false" || v == "no" || v == "off"
}

// HybridFuzzMode returns "inline" (default) or "process".
func HybridFuzzMode() string {
	m := strings.ToLower(strings.TrimSpace(os.Getenv("HACKME_WORKER_HYBRID_FUZZ_MODE")))
	if m == "process" || m == "subprocess" || m == "child" {
		return "process"
	}
	return "inline"
}

// HTTPTimeoutFromEnv mirrors workerfuzz timeout selection.
func HTTPTimeoutFromEnv() time.Duration {
	if v := strings.TrimSpace(os.Getenv("WORKERFUZZ_HTTP_TIMEOUT_SEC")); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			return time.Duration(sec) * time.Second
		}
	}
	if strings.Contains(strings.ToLower(os.Getenv("COORD_URL")), "hackme.tech") {
		return 120 * time.Second
	}
	return 45 * time.Second
}

// LoadHybridKey loads HACKME_MINER_ED25519_SEED_HEX; refuses treasury/dev-fee payout.
// Fallback: HACKME_MINER_SEED_FILE, then desktop node seed (logs/desktop/data/node_ed25519.seed).
func LoadHybridKey() (ed25519.PrivateKey, string, string, bool, error) {
	seedHex := strings.TrimSpace(os.Getenv("HACKME_MINER_ED25519_SEED_HEX"))
	if seedHex == "" {
		if raw, err := loadMinerSeedBytes(); err == nil {
			seedHex = hex.EncodeToString(raw)
		}
	}
	if seedHex == "" {
		return nil, "", "", false, errors.New("HACKME_MINER_ED25519_SEED_HEX required (or HACKME_MINER_SEED_FILE / desktop node seed)")
	}
	seed, err := hex.DecodeString(seedHex)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, "", "", false, errors.New("invalid HACKME_MINER_ED25519_SEED_HEX")
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	sum := sha256.Sum256(pub)
	addr := "HMC-" + hex.EncodeToString(sum[:])[:16]
	if PayoutIsTreasury(addr) {
		return nil, "", "", false, fmt.Errorf("payout address %s is treasury/dev-fee — generate a dedicated worker key (minersign -gen-seed)", addr)
	}
	return priv, hex.EncodeToString(pub), addr, true, nil
}

func loadMinerSeedBytes() ([]byte, error) {
	candidates := []string{}
	if p := strings.TrimSpace(os.Getenv("HACKME_MINER_SEED_FILE")); p != "" {
		candidates = append(candidates, p)
	}
	candidates = append(candidates,
		filepath.Join("logs", "desktop", "data", "node_ed25519.seed"),
		filepath.Join("data", "node_ed25519.seed"),
	)
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "..", "logs", "desktop", "data", "node_ed25519.seed"),
			filepath.Join(dir, "..", "data", "node_ed25519.seed"),
		)
	}
	var last error
	for _, p := range candidates {
		b, err := os.ReadFile(p)
		if err != nil {
			last = err
			continue
		}
		b = bytes.TrimSpace(b)
		if len(b) == ed25519.SeedSize {
			return b, nil
		}
		// ASCII hex seed file
		if raw, err := hex.DecodeString(string(b)); err == nil && len(raw) == ed25519.SeedSize {
			return raw, nil
		}
		last = fmt.Errorf("seed file %s: want 32 bytes or 64 hex chars", p)
	}
	if last == nil {
		last = errors.New("miner seed file not found")
	}
	return nil, last
}

// PayoutIsTreasury reports whether addr is the chain treasury/dev-fee sink.
func PayoutIsTreasury(addr string) bool {
	return strings.EqualFold(strings.TrimSpace(addr), chain.DevFeeAddress)
}

// Run loops until ctx is cancelled. Each claim cycle is panic-isolated.
func Run(ctx context.Context, cfg Config, st *Stats) error {
	if st == nil {
		st = &Stats{}
	}
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 1
	}
	if cfg.TimeoutMS <= 0 {
		cfg.TimeoutMS = 500
	}
	if cfg.MinClaimGap <= 0 {
		cfg.MinClaimGap = 50 * time.Millisecond
	}
	// BackpressureFloorPct: 0 disables; negative/unset → default 35 in callers that omit it.
	// Hybrid dig profile passes an explicit floor (often 10).
	if cfg.BackpressureFloorPct < 0 {
		cfg.BackpressureFloorPct = 35
	}
	if cfg.LogPrefix == "" {
		cfg.LogPrefix = "workerfuzz"
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: HTTPTimeoutFromEnv()}
	}
	base := strings.TrimRight(cfg.CoordURL, "/")
	if base == "" || cfg.Token == "" || cfg.WorkerID == "" {
		return errors.New("coord URL, token, and worker_id required")
	}
	if cfg.Hybrid && cfg.Priv == nil {
		return errors.New("hybrid signer required but no key loaded")
	}

	sem := make(chan struct{}, cfg.Concurrency)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if paused := backpressurePause(cfg, st); paused > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(paused):
			}
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case sem <- struct{}{}:
		}
		go func() {
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					st.Panics.Add(1)
					fmt.Fprintf(os.Stderr, "%s: recovered panic in fuzz cycle: %v\n", cfg.LogPrefix, r)
				}
			}()
			runOne(ctx, cfg, base, st)
		}()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cfg.MinClaimGap):
		}
	}
}

func backpressurePause(cfg Config, st *Stats) time.Duration {
	if cfg.BackpressureFloorPct <= 0 || cfg.PohGHSMilli == nil || cfg.CalibGHSMilli == nil {
		return 0
	}
	calib := cfg.CalibGHSMilli.Load()
	cur := cfg.PohGHSMilli.Load()
	if calib < 1000 || cur <= 0 {
		return 0
	}
	floor := calib * int64(cfg.BackpressureFloorPct) / 100
	if floor < 1 {
		floor = 1
	}
	if cur >= floor {
		return 0
	}
	st.PausedBack.Add(1)
	fmt.Fprintf(os.Stderr, "%s: backpressure — PoH %.2f GH/s < %d%% of calib %.2f; pausing fuzz 5s\n",
		cfg.LogPrefix, float64(cur)/1000.0, cfg.BackpressureFloorPct, float64(calib)/1000.0)
	return 5 * time.Second
}

func runOne(ctx context.Context, cfg Config, base string, st *Stats) {
	cr, err := Claim(ctx, cfg.HTTPClient, base, cfg.Token, cfg.WorkerID)
	if err != nil {
		sleep := backoffForErr(err)
		fmt.Fprintf(os.Stderr, "%s: claim: %v (sleep %s)\n", cfg.LogPrefix, err, sleep)
		select {
		case <-ctx.Done():
		case <-time.After(sleep):
		}
		return
	}
	if !cr.OK {
		sleep := backoffForReason(cr.Reason)
		select {
		case <-ctx.Done():
		case <-time.After(sleep):
		}
		return
	}
	st.ClaimsOK.Add(1)
	var checkRet int32
	var durMS int
	var trap string
	var execDone int
	if IsHuntClaim(cr) {
		if err := HuntClaimMissingFields(cr); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", cfg.LogPrefix, err)
			return
		}
		checkRet, durMS, trap, execDone = RunHuntShard(ctx, cr, cfg.TimeoutMS)
	} else {
		checkRet, durMS, trap, execDone = RunSegmentCheck(ctx, cr, cfg.TimeoutMS)
	}
	nonce := uint64(time.Now().UnixNano())
	if err := Submit(ctx, cfg.HTTPClient, base, cfg.Token, cfg.WorkerID, cfg.MinerAddr, cfg.Priv, cfg.PubHex, cfg.Hybrid, nonce, cr, checkRet, durMS, trap, execDone); err != nil {
		fmt.Fprintf(os.Stderr, "%s: submit: %v\n", cfg.LogPrefix, err)
		return
	}
	st.SubmitsOK.Add(1)
	checkSem := fuzzengine.ParseCheckSemantics(map[string]any{"check_semantics": cr.CheckSemantics})
	pass, finding := fuzzengine.EvalCheck(checkSem, checkRet, nil)
	if finding && trap == "" && !IsHuntClaim(cr) {
		st.Findings.Add(1)
		fmt.Fprintf(os.Stderr, "%s: FINDING campaign=%s input=0x%x semantics=%s\n", cfg.LogPrefix, cr.CampaignID, cr.ActualInput, checkSem)
	} else if pass || IsHuntClaim(cr) {
		fmt.Fprintf(os.Stderr, "%s: ok campaign=%s input=0x%x\n", cfg.LogPrefix, cr.CampaignID, cr.ActualInput)
	}
}

func backoffForErr(err error) time.Duration {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "429") || strings.Contains(msg, "banned") || strings.Contains(msg, "no_fuzz_work") ||
		strings.Contains(msg, "rate") || strings.Contains(msg, "too_many") {
		return 30 * time.Second
	}
	// nginx/proxy briefly 502/503/504 while coordinator restarts (SQLite boot).
	if strings.Contains(msg, "502") || strings.Contains(msg, "503") || strings.Contains(msg, "504") ||
		strings.Contains(msg, "bad gateway") || strings.Contains(msg, "gateway timeout") {
		return 5 * time.Second
	}
	return 2 * time.Second
}

// shortHTTPBody keeps claim/submit errors one-line and never dumps nginx HTML into worker logs.
func shortHTTPBody(status int, raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return http.StatusText(status)
	}
	low := strings.ToLower(s)
	if strings.Contains(low, "<html") || strings.Contains(low, "<!doctype") || strings.Contains(low, "<head>") {
		title := ""
		if i := strings.Index(low, "<title>"); i >= 0 {
			j := strings.Index(low[i+7:], "</title>")
			if j > 0 {
				title = strings.TrimSpace(s[i+7 : i+7+j])
			}
		}
		if title == "" {
			if strings.Contains(low, "502") {
				title = "Bad Gateway"
			} else if strings.Contains(low, "503") {
				title = "Service Unavailable"
			} else if strings.Contains(low, "504") {
				title = "Gateway Timeout"
			} else {
				title = "HTML error page"
			}
		}
		return title
	}
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	if len(s) > 160 {
		return s[:157] + "..."
	}
	return s
}

func backoffForReason(reason string) time.Duration {
	rl := strings.ToLower(reason)
	if strings.Contains(rl, "banned") || strings.Contains(rl, "no_fuzz_work") || strings.Contains(rl, "rate") {
		return 30 * time.Second
	}
	return 2 * time.Second
}

// Claim leases one fuzz work item.
func Claim(ctx context.Context, cl *http.Client, base, token, workerID string) (ClaimResp, error) {
	var out ClaimResp
	body, _ := json.Marshal(map[string]any{"worker_id": workerID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/fuzz/work/claim", bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", token)
	res, err := cl.Do(req)
	if err != nil {
		return out, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	_ = json.Unmarshal(b, &out)
	if res.StatusCode != 200 {
		return out, fmt.Errorf("HTTP %d %s", res.StatusCode, shortHTTPBody(res.StatusCode, b))
	}
	return out, nil
}

// RunCheck executes the leased WASM check in-process (wazero) — single exec fallback.
func RunCheck(ctx context.Context, cr ClaimResp, timeoutMS int) (checkResult int32, durationMS int, trap string) {
	checkResult, durationMS, trap, _ = RunSegmentCheck(ctx, cr, timeoutMS)
	return checkResult, durationMS, trap
}

// RunSegmentCheck runs exec_per_unit deterministic execs for one work unit.
func RunSegmentCheck(ctx context.Context, cr ClaimResp, timeoutMS int) (checkResult int32, durationMS int, trap string, execDone int) {
	start := time.Now()
	wasm, err := hex.DecodeString(strings.TrimSpace(cr.WasmCheckHex))
	if err != nil || len(wasm) == 0 {
		return 0, 0, "missing wasm", 0
	}
	execPer := cr.ExecPerUnit
	if execPer < 1 {
		execPer = 1
	}
	cfg := map[string]any{
		"input_mode":      strings.TrimSpace(cr.InputMode),
		"depth_tier":      strings.TrimSpace(cr.DepthTier),
		"check_semantics": strings.TrimSpace(cr.CheckSemantics),
		"exec_per_unit":   execPer,
	}
	if cr.MaxInputBytes > 0 {
		cfg["max_input_bytes"] = cr.MaxInputBytes
	}
	if ck := strings.TrimSpace(cr.CoverageKind); ck != "" {
		cfg["coverage_kind"] = ck
	}
	if strings.EqualFold(cr.InputMode, "bytes") {
		cfg["input_mode"] = "bytes"
	}
	seeds, _ := fuzzengine.CorpusSeedsFromClaimMaps(cr.CorpusSeeds)
	if len(seeds) > 0 {
		cfg["guided_scheduling"] = true
	}
	sem := fuzzengine.ParseCheckSemantics(cfg)
	if fuzzengine.GuidedSchedulingEnabled(cfg) && len(seeds) == 0 {
		// Legacy claim without snapshot — segment verify may diverge; single-exec only.
		if execPer > 1 {
			execPer = 1
		}
		cfg["exec_per_unit"] = execPer
	}
	runOne := func(runCtx context.Context, inU uint64, inputB []byte) (int32, string, error, []byte) {
		cctx, cancel := context.WithTimeout(runCtx, time.Duration(timeoutMS)*time.Millisecond)
		defer cancel()
		if len(inputB) > 0 {
			out, execErr := sandbox.InvokeCheckOutcomeInput(cctx, wasm, inputB)
			if execErr != nil {
				return 0, execErr.Error(), execErr, nil
			}
			if out.OK {
				return 1, "", nil, out.EdgeBitmap
			}
			return 0, "", nil, out.EdgeBitmap
		}
		if inU != 0 {
			out, execErr := sandbox.InvokeCheckOutcome(cctx, wasm, inU)
			if execErr != nil {
				return 0, execErr.Error(), execErr, nil
			}
			if out.OK {
				return 1, "", nil, out.EdgeBitmap
			}
			return 0, "", nil, out.EdgeBitmap
		}
		out, execErr := sandbox.InvokeCheckOutcomeInput(cctx, wasm, InputForCheck(cr))
		if execErr != nil {
			return 0, execErr.Error(), execErr, nil
		}
		if out.OK {
			return 1, "", nil, out.EdgeBitmap
		}
		return 0, "", nil, out.EdgeBitmap
	}
	if execPer <= 1 {
		cctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
		defer cancel()
		out, execErr := sandbox.InvokeCheckOutcomeInput(cctx, wasm, InputForCheck(cr))
		durationMS = int(time.Since(start).Milliseconds())
		if execErr != nil {
			return 0, durationMS, execErr.Error(), 1
		}
		if out.OK {
			return 1, durationMS, "", 1
		}
		return 0, durationMS, "", 1
	}
	segCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS*execPer)*time.Millisecond)
	defer cancel()
	seg := fuzzengine.EvalSegment(segCtx, cr.InputN, cfg, seeds, sem, runOne)
	durationMS = int(time.Since(start).Milliseconds())
	return seg.CheckResult, durationMS, seg.Trap, seg.ExecDone
}

// InputForCheck returns bytes for check_bytes / packed check(i64).
func InputForCheck(cr ClaimResp) []byte {
	if h := strings.TrimSpace(cr.InputBytesHex); h != "" {
		if b, err := hex.DecodeString(h); err == nil && len(b) > 0 {
			return b
		}
	}
	return nil
}

// Submit posts a fuzz work result (optional hybrid signature).
func Submit(ctx context.Context, cl *http.Client, base, token, workerID, minerAddress string, priv ed25519.PrivateKey, pubHex string, hybrid bool, nonce uint64, cr ClaimResp, checkResult int32, durationMS int, trap string, segmentExecDone int) error {
	payload := map[string]any{
		"worker_id":         workerID,
		"work_id":           cr.WorkID,
		"campaign_id":       cr.CampaignID,
		"item_id":           cr.ItemID,
		"input_n":           cr.InputN,
		"actual_input":      cr.ActualInput,
		"check_result":      checkResult,
		"duration_ms":       durationMS,
		"trap":              trap,
		"submit_nonce":      nonce,
		"segment_exec_done": segmentExecDone,
	}
	if hybrid {
		if priv == nil {
			return errors.New("hybrid signer required but no key loaded")
		}
		signPayload := poolfuzz.SubmitSignPayload{
			WorkerID: workerID, CampaignID: cr.CampaignID, ItemID: cr.ItemID,
			InputN: cr.InputN, ActualInput: cr.ActualInput, CheckResult: checkResult, SubmitNonce: nonce,
			SegmentExecDone: segmentExecDone,
		}
		if h := strings.TrimSpace(cr.InputBytesHex); h != "" {
			signPayload.InputBytesHex = h
		}
		sig := ed25519.Sign(priv, poolfuzz.CanonicalSubmitBytes(signPayload))
		payload["miner_pubkey"] = pubHex
		payload["miner_sig"] = hex.EncodeToString(sig)
		payload["miner_sig_alg"] = "ed25519"
		payload["miner_address"] = minerAddress
	} else if strings.TrimSpace(minerAddress) != "" {
		payload["miner_address"] = strings.TrimSpace(minerAddress)
	}
	if strings.TrimSpace(cr.InputBytesHex) != "" {
		payload["input_bytes_hex"] = strings.TrimSpace(cr.InputBytesHex)
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/fuzz/work/submit", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", token)
	res, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if res.StatusCode != 200 {
		return fmt.Errorf("HTTP %d %s", res.StatusCode, shortHTTPBody(res.StatusCode, b))
	}
	return nil
}

// EnvInt reads a positive int env or returns fallback.
func EnvInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	x, err := strconv.Atoi(v)
	if err != nil || x < 0 {
		return fallback
	}
	return x
}

// EnvDurationMS reads milliseconds from env.
func EnvDurationMS(key string, fallbackMS int) time.Duration {
	ms := EnvInt(key, fallbackMS)
	if ms < 0 {
		ms = fallbackMS
	}
	return time.Duration(ms) * time.Millisecond
}
