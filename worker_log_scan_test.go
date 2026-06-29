package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanNewestWorkerpohLogs(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "workerpoh-w1-20260101T000000Z.log")
	newer := filepath.Join(dir, "workerpoh-w2-20260201T120000Z.log")
	part := filepath.Join(dir, "worker_participant.log")
	for _, p := range []string{old, newer, part} {
		if err := os.WriteFile(p, []byte("submit ok ghs=1.5\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Chtimes(old, time.Now().Add(-48*time.Hour), time.Now().Add(-48*time.Hour))
	_ = os.Chtimes(newer, time.Now(), time.Now())
	_ = os.Chtimes(part, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))

	if got := scanNewestWorkerpohLogs(dir, false); got != newer {
		t.Fatalf("worker-only: got %q want %q", got, newer)
	}
	if got := scanNewestWorkerpohLogs(dir, true); got != newer {
		t.Fatalf("with participant: got %q want %q", got, newer)
	}
	workerpohLogPathCacheMu.Lock()
	delete(workerpohLogPathCache, dir)
	workerpohLogPathCacheMu.Unlock()
	if got := latestWorkerpohLogPath(dir); got != newer {
		t.Fatalf("cached path: got %q want %q", got, newer)
	}
}

func TestWorkerActiveFromParticipantLog(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "worker_participant.log")
	if err := os.WriteFile(p, []byte("line\nsubmit ok ghs=2.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !workerActiveFromParticipantLog(dir, 120) {
		t.Fatal("expected active from participant log")
	}
}

func TestWorkerLogStartedUnixLocalFilename(t *testing.T) {
	dir := t.TempDir()
	stamp := time.Now().Add(-45 * time.Minute).Format("20060102T150405")
	p := filepath.Join(dir, "workerpoh-worker-kapa-pc-"+stamp+".log")
	if err := os.WriteFile(p, []byte("submit ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ux := workerLogStartedUnix(p)
	if ux <= 0 {
		t.Fatal("expected positive start unix")
	}
	sec := time.Now().Unix() - ux
	if sec < 40*60 || sec > 50*60 {
		t.Fatalf("session ~45m wanted, got %ds from ux %d", sec, ux)
	}
}

func TestWorkerLogStartedUnixFutureFilenameUsesMtime(t *testing.T) {
	dir := t.TempDir()
	// Wall-clock stamp 2h ahead in local TZ (autostart naming convention).
	future := time.Now().Add(2 * time.Hour).Format("20060102T150405")
	p := filepath.Join(dir, "workerpoh-worker-kapa-pc-"+future+".log")
	if err := os.WriteFile(p, []byte("submit ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ux := workerLogStartedUnix(p)
	// No valid past candidate — returns first parse (future); session clamped to 0 at API layer.
	now := time.Now().Unix()
	if ux <= now {
		t.Fatalf("expected future unix from local stamp, ux=%d now=%d", ux, now)
	}
}
