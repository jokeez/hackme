package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkerHeartbeatNeedsRestartGrace(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().Unix()
	started := now - 30
	need, reason := workerHeartbeatNeedsRestart(dir, "w1", started, now, 180, 120)
	if need {
		t.Fatalf("within grace should not restart, reason=%s", reason)
	}
}

func TestWorkerHeartbeatNeedsRestartNoSignalPastGraceButWithinStale(t *testing.T) {
	// CUDA cold-start window: past grace (120) but silence age still < stale (180).
	dir := t.TempDir()
	now := time.Now().Unix()
	started := now - 150
	need, reason := workerHeartbeatNeedsRestart(dir, "w1", started, now, 180, 120)
	if need {
		t.Fatalf("should wait full stale from start, got reason=%s", reason)
	}
}

func TestWorkerHeartbeatNeedsRestartNoSignalAfterStale(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().Unix()
	started := now - 200
	need, reason := workerHeartbeatNeedsRestart(dir, "w1", started, now, 180, 120)
	if !need || reason != "no_submit_heartbeat_after_grace" {
		t.Fatalf("got need=%v reason=%q", need, reason)
	}
}

func TestWorkerHeartbeatNeedsRestartFreshNonce(t *testing.T) {
	dir := t.TempDir()
	wid := "worker-win-test"
	safe := sanitizeWorkerIDForNonce(wid)
	p := filepath.Join(dir, "miner_submit_nonce."+safe+".seq")
	if err := os.WriteFile(p, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	started := now - 600
	need, reason := workerHeartbeatNeedsRestart(dir, wid, started, now, 180, 120)
	if need {
		t.Fatalf("fresh nonce must not restart, reason=%s", reason)
	}
}

func TestWorkerHeartbeatNeedsRestartStaleNonce(t *testing.T) {
	dir := t.TempDir()
	wid := "worker-win-test"
	safe := sanitizeWorkerIDForNonce(wid)
	p := filepath.Join(dir, "miner_submit_nonce."+safe+".seq")
	if err := os.WriteFile(p, []byte("9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	started := now - 600
	// In-session but stale (mtime after start, age > staleSec).
	stamp := time.Unix(now-400, 0)
	if err := os.Chtimes(p, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	need, reason := workerHeartbeatNeedsRestart(dir, wid, started, now, 180, 120)
	if !need || reason != "submit_heartbeat_stale" {
		t.Fatalf("got need=%v reason=%q", need, reason)
	}
}

func TestWorkerHeartbeatIgnoresPreSessionNonce(t *testing.T) {
	dir := t.TempDir()
	wid := "worker-win-test"
	safe := sanitizeWorkerIDForNonce(wid)
	p := filepath.Join(dir, "miner_submit_nonce."+safe+".seq")
	if err := os.WriteFile(p, []byte("9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	started := now - 100
	// Leftover from previous process — older than this session.
	stamp := time.Unix(now-3600, 0)
	if err := os.Chtimes(p, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	// Within stale-from-start window → must NOT restart yet.
	need, _ := workerHeartbeatNeedsRestart(dir, wid, started, now, 180, 60)
	if need {
		t.Fatal("pre-session nonce must be ignored; silence age is only 100s")
	}
	hb := workerSubmitHeartbeatUnixSince(dir, wid, started)
	if hb != 0 {
		t.Fatalf("expected filtered hb=0, got %d", hb)
	}
}

func TestWorkerSubmitNonceHeartbeatUnixGlobGPU(t *testing.T) {
	dir := t.TempDir()
	wid := "rig-a"
	safe := sanitizeWorkerIDForNonce(wid)
	p := filepath.Join(dir, "miner_submit_nonce."+safe+".gpu0.seq")
	if err := os.WriteFile(p, []byte("2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := workerSubmitNonceHeartbeatUnix(dir, wid); got <= 0 {
		t.Fatal("expected gpu nonce mtime")
	}
}

func TestWorkerNonceFilenameNoPrefixCollision(t *testing.T) {
	dir := t.TempDir()
	// Other worker's file must not count for "rig".
	other := filepath.Join(dir, "miner_submit_nonce.rig-extra.seq")
	if err := os.WriteFile(other, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := workerSubmitNonceHeartbeatUnix(dir, "rig"); got != 0 {
		t.Fatalf("prefix collision: got hb=%d", got)
	}
	if !workerNonceFilenameOwnsWorker("miner_submit_nonce.rig.seq", "rig") {
		t.Fatal("exact should own")
	}
	if workerNonceFilenameOwnsWorker("miner_submit_nonce.rig-extra.seq", "rig") {
		t.Fatal("prefix must not own")
	}
	if !workerNonceFilenameOwnsWorker("miner_submit_nonce.rig.gpu12.seq", "rig") {
		t.Fatal("gpu suffix should own")
	}
}

func TestWorkerLogSubmitHeartbeatUnix(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "worker_participant.log")
	if err := os.WriteFile(p, []byte("boot\nsubmit ok ghs=12.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := workerLogSubmitHeartbeatUnix(dir); got <= 0 {
		t.Fatal("expected log heartbeat")
	}
	old := time.Now().Add(-15 * time.Minute)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	started := now - 600
	// Ensure log mtime is still after startedAt for in-session stale.
	stamp := time.Unix(now-400, 0)
	_ = os.Chtimes(p, stamp, stamp)
	need, reason := workerHeartbeatNeedsRestart(dir, "x", started, now, 180, 120)
	if !need || reason != "submit_heartbeat_stale" {
		t.Fatalf("stale log: need=%v reason=%q", need, reason)
	}
}

func TestSanitizeWorkerIDForNonce(t *testing.T) {
	if got := sanitizeWorkerIDForNonce("worker-win-20260507zzp"); got != "worker-win-20260507zzp" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeWorkerIDForNonce("a b/c"); got != "a_b_c" {
		t.Fatalf("got %q", got)
	}
}

func TestPoolWorkerWatchdogTickFrozen(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(filepath.Join(dir, "scripts", "ops"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "ops", "worker_loop.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_REPO_ROOT", dir)
	wid := "worker-frozen"
	safe := sanitizeWorkerIDForNonce(wid)
	nonce := filepath.Join(logDir, "miner_submit_nonce."+safe+".seq")
	if err := os.WriteFile(nonce, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	started := now - 600
	stamp := time.Unix(now-400, 0)
	_ = os.Chtimes(nonce, stamp, stamp)

	a := &app{dataDir: filepath.Join(dir, "data"), workerID: wid, workerStartedAt: started}
	a.workerRunningCacheMu.Lock()
	a.workerRunningCached = true
	a.workerRunningCacheAt = now
	a.workerRunningCacheMu.Unlock()

	t.Setenv("HACKME_WORKER_HEARTBEAT_STALE_SEC", "180")
	t.Setenv("HACKME_WORKER_HEARTBEAT_GRACE_SEC", "60")
	action, detail := a.poolWorkerWatchdogTick(now)
	if action != "restart_frozen" || detail != "submit_heartbeat_stale" {
		t.Fatalf("got action=%q detail=%q", action, detail)
	}
}

func TestPoolWorkerWatchdogTickPaused(t *testing.T) {
	t.Setenv("HACKME_MINING_PAUSED", "1")
	a := &app{}
	action, _ := a.poolWorkerWatchdogTick(time.Now().Unix())
	if action != "paused" {
		t.Fatalf("got %q want paused", action)
	}
}

func TestPoolWorkerWatchdogTickExternalNoStartedAt(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "scripts", "ops"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "scripts", "ops", "worker_loop.sh"), []byte("#!/bin/sh\n"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "logs"), 0o755)
	t.Setenv("HACKME_REPO_ROOT", dir)
	t.Setenv("HACKME_MINING_PAUSED", "0")

	a := &app{dataDir: filepath.Join(dir, "data"), workerID: "ext", workerStartedAt: 0}
	a.workerRunningCacheMu.Lock()
	a.workerRunningCached = true
	a.workerRunningCacheAt = time.Now().Unix()
	a.workerRunningCacheMu.Unlock()

	action, detail := a.poolWorkerWatchdogTick(time.Now().Unix())
	if action != "ok" || detail != "external_no_started_at" {
		t.Fatalf("got action=%q detail=%q", action, detail)
	}
}

func TestPoolWorkerWatchdogTickMissing(t *testing.T) {
	t.Setenv("HACKME_MINING_PAUSED", "0")
	a := &app{}
	a.workerRunningCacheMu.Lock()
	a.workerRunningCached = false
	a.workerRunningCacheAt = time.Now().Unix()
	a.workerRunningCacheMu.Unlock()
	action, detail := a.poolWorkerWatchdogTick(time.Now().Unix())
	if action != "restart_missing" {
		t.Fatalf("got action=%q detail=%q", action, detail)
	}
}

func TestPoolWorkerWatchdogTickDesktopRecentSubmit(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(filepath.Join(dir, "scripts", "ops"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// resolveWorkerRepoRoot only accepts HACKME_REPO_ROOT when the tree looks like a worker checkout.
	if err := os.WriteFile(filepath.Join(dir, "scripts", "ops", "worker_loop.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_REPO_ROOT", dir)
	t.Setenv("HACKME_MINING_PAUSED", "0")
	t.Setenv("HACKME_DESKTOP_MODE", "1")

	wid := "desk-w"
	safe := sanitizeWorkerIDForNonce(wid)
	nonce := filepath.Join(logDir, "miner_submit_nonce."+safe+".seq")
	if err := os.WriteFile(nonce, []byte("42\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &app{dataDir: filepath.Join(dir, "data"), workerID: wid}
	a.workerRunningCacheMu.Lock()
	a.workerRunningCached = false
	a.workerRunningCacheAt = time.Now().Unix()
	a.workerRunningCacheMu.Unlock()

	action, detail := a.poolWorkerWatchdogTick(time.Now().Unix())
	if action != "ok" || detail != "desktop_recent_submit" {
		t.Fatalf("got action=%q detail=%q want ok/desktop_recent_submit", action, detail)
	}
}

func TestWorkerStopStartViaAPILoopback(t *testing.T) {
	t.Setenv("HACKME_ADMIN_TOKEN", "test-admin-token-heartbeat")
	t.Setenv("HACKME_DESKTOP_MODE", "1")
	a := &app{}
	// stop with no worker should succeed
	if err := a.stopPoolWorkerViaAPI(); err != nil {
		t.Fatalf("stop empty: %v", err)
	}
	// start without coord should fail cleanly
	err := a.restartPoolWorkerViaAPI()
	if err == nil {
		t.Fatal("expected start failure without coordinator URL")
	}
}

func TestHandleWorkerStatusHeartbeatFields(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	_ = os.MkdirAll(filepath.Join(dir, "scripts", "ops"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "scripts", "ops", "worker_loop.sh"), []byte("#!/bin/sh\n"), 0o755)
	_ = os.MkdirAll(logDir, 0o755)
	t.Setenv("HACKME_REPO_ROOT", dir)

	wid := "status-w"
	safe := sanitizeWorkerIDForNonce(wid)
	nonce := filepath.Join(logDir, "miner_submit_nonce."+safe+".seq")
	_ = os.WriteFile(nonce, []byte("1\n"), 0o644)

	a := &app{
		dataDir:         filepath.Join(dir, "data"),
		workerID:        wid,
		workerStartedAt: time.Now().Unix() - 60,
		workerLogPath:   filepath.Join(logDir, "worker_participant.log"),
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/worker/status", nil)
	a.handleWorkerStatus(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"submit_heartbeat_unix", "submit_heartbeat_age_sec", "submit_heartbeat_stale_sec", "submit_heartbeat_frozen"} {
		if _, ok := out[k]; !ok {
			t.Fatalf("missing field %s in %v", k, out)
		}
	}
}
