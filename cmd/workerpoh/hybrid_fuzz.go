package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"hackme/internal/workerfuzzloop"
	"hackme/internal/workerlock"
)

// hybridFuzzState shares PoH hashrate with the inline fuzz loop for backpressure.
type hybridFuzzState struct {
	pohGHSMilli   atomic.Int64
	calibGHSMilli atomic.Int64
	stats         workerfuzzloop.Stats
	cancel        context.CancelFunc
}

func (h *hybridFuzzState) notePoHGHS(ghs, calib float64) {
	if h == nil {
		return
	}
	if ghs > 0 {
		h.pohGHSMilli.Store(int64(ghs * 1000))
	}
	if calib > 0 {
		h.calibGHSMilli.Store(int64(calib * 1000))
	}
}

// startHybridFuzzIfEnabled launches PoH+fuzz dig under the same worker_id.
// Hybrid is ON by default; set HACKME_WORKER_HYBRID_FUZZ=0 to disable.
// Default mode is inline (one binary); set HACKME_WORKER_HYBRID_FUZZ_MODE=process
// for OS-level crash isolation via a supervised workerfuzz child.
func startHybridFuzzIfEnabled(coordURL, token, workerID string) *hybridFuzzState {
	if !workerfuzzloop.HybridFuzzEnabled() {
		return nil
	}
	st := &hybridFuzzState{}
	ctx, cancel := context.WithCancel(context.Background())
	st.cancel = cancel
	mode := workerfuzzloop.HybridFuzzMode()
	fmt.Fprintf(os.Stderr, "workerpoh: hybrid fuzz ON mode=%s worker_id=%s (same id for PoH+fuzz; no *-fuzz sybil row)\n",
		mode, workerID)
	switch mode {
	case "process":
		go superviseHybridFuzzProcess(ctx, coordURL, token, workerID)
	default:
		go runHybridFuzzInline(ctx, st, coordURL, token, workerID)
	}
	return st
}

func runHybridFuzzInline(ctx context.Context, st *hybridFuzzState, coordURL, token, workerID string) {
	priv, pubHex, addr, hybrid, err := workerfuzzloop.LoadHybridKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "workerpoh: hybrid fuzz disabled: %v\n", err)
		return
	}
	conc := workerfuzzloop.EnvInt("HACKME_WORKER_HYBRID_FUZZ_CONCURRENCY", 1)
	if conc > 2 {
		// Hard cap: keep fuzz from starving GPU PoH / desktop CVE soaks on shared hosts.
		conc = 2
	}
	gapMS := workerfuzzloop.EnvInt("HACKME_WORKER_HYBRID_FUZZ_CLAIM_GAP_MS", 200)
	timeoutMS := workerfuzzloop.EnvInt("HACKME_WORKER_HYBRID_FUZZ_TIMEOUT_MS", 500)
	floorPct := workerfuzzloop.EnvInt("HACKME_WORKER_HYBRID_FUZZ_BACKPRESSURE_PCT", 35)
	cfg := workerfuzzloop.Config{
		CoordURL:             coordURL,
		Token:                token,
		WorkerID:             workerID,
		MinerAddr:            addr,
		TimeoutMS:            timeoutMS,
		HTTPClient:           &http.Client{Timeout: workerfuzzloop.HTTPTimeoutFromEnv()},
		Priv:                 priv,
		PubHex:               pubHex,
		Hybrid:               hybrid,
		Concurrency:          conc,
		MinClaimGap:          time.Duration(gapMS) * time.Millisecond,
		LogPrefix:            "workerpoh-fuzz",
		PohGHSMilli:          &st.pohGHSMilli,
		CalibGHSMilli:        &st.calibGHSMilli,
		BackpressureFloorPct: floorPct,
	}
	fmt.Fprintf(os.Stderr, "workerpoh: hybrid fuzz inline concurrency=%d claim_gap_ms=%d timeout_ms=%d backpressure=%d%% payout=%s\n",
		conc, gapMS, timeoutMS, floorPct, addr)
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		err := workerfuzzloop.Run(ctx, cfg, &st.stats)
		if ctx.Err() != nil {
			return
		}
		fmt.Fprintf(os.Stderr, "workerpoh: hybrid fuzz loop exited (%v); restarting in 3s\n", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func superviseHybridFuzzProcess(ctx context.Context, coordURL, token, workerID string) {
	bin := resolveWorkerfuzzBin()
	if bin == "" {
		fmt.Fprintln(os.Stderr, "workerpoh: hybrid fuzz process mode: workerfuzz binary not found (build ./cmd/workerfuzz or set HACKME_WORKERFUZZ_BIN)")
		return
	}
	niceLevel := workerfuzzloop.EnvInt("HACKME_WORKER_HYBRID_FUZZ_NICE", 10)
	timeoutMS := workerfuzzloop.EnvInt("HACKME_WORKER_HYBRID_FUZZ_TIMEOUT_MS", 500)
	lockDir := strings.TrimSpace(os.Getenv("HACKME_WORKER_LOCK_DIR"))
	backoff := 2 * time.Second
	loggedBusy := false
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		// If a standalone digger already holds the lock, do not spawn/restart-spam.
		if workerlock.Held("workerfuzz", workerID, lockDir) {
			if !loggedBusy {
				fmt.Fprintf(os.Stderr, "workerpoh: hybrid fuzz process: digger already running for worker_id=%s — waiting (no duplicate spawn)\n", workerID)
				loggedBusy = true
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
			continue
		}
		loggedBusy = false
		// Prefer `nice` wrapper so process-mode actually yields CPU to PoH/GPU
		// (SysProcAttr has no portable Nice field on Linux).
		args := []string{
			"-coord", coordURL,
			"-token", token,
			"-worker", workerID,
			"-timeout-ms", strconv.Itoa(timeoutMS),
		}
		var cmd *exec.Cmd
		if niceLevel > 0 {
			if niceBin, err := exec.LookPath("nice"); err == nil {
				cmd = exec.CommandContext(ctx, niceBin, append([]string{"-n", strconv.Itoa(niceLevel), bin}, args...)...)
			}
		}
		if cmd == nil {
			cmd = exec.CommandContext(ctx, bin, args...)
		}
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		cmd.Env = os.Environ()
		// Same WORKER_ID as PoH — intentional (one pool row). Child inherits miner seed.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		fmt.Fprintf(os.Stderr, "workerpoh: hybrid fuzz process start bin=%s nice=%d\n", bin, niceLevel)
		err := cmd.Run()
		if ctx.Err() != nil {
			return
		}
		// Child exited immediately because lock raced — treat as busy, not crash storm.
		if workerlock.Held("workerfuzz", workerID, lockDir) {
			if !loggedBusy {
				fmt.Fprintf(os.Stderr, "workerpoh: hybrid fuzz process: digger already running for worker_id=%s — waiting (no duplicate spawn)\n", workerID)
				loggedBusy = true
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
			continue
		}
		fmt.Fprintf(os.Stderr, "workerpoh: hybrid fuzz child exited (%v); restart in %s\n", err, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func resolveWorkerfuzzBin() string {
	if v := strings.TrimSpace(os.Getenv("HACKME_WORKERFUZZ_BIN")); v != "" {
		if st, err := os.Stat(v); err == nil && !st.IsDir() {
			return v
		}
	}
	candidates := []string{
		filepath.Join("bin", "workerfuzz"),
		"./workerfuzz",
		"workerfuzz",
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append([]string{
			filepath.Join(dir, "workerfuzz"),
			filepath.Join(dir, "..", "bin", "workerfuzz"),
		}, candidates...)
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs
		}
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	return ""
}
