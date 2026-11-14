package main

import (
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"hackme/internal/logsafe"
)

// defaultPoolWorkerID picks the pool worker id when none is configured.
//
// It used to be "worker-<hostname>". The coordinator locks a worker id to the first payout
// address it sees, so a second machine with the same hostname (cloud VMs "ubuntu", "debian",
// Raspberry Pi images, containers on one host) got `403 payout_address_locked` on every claim
// and mined nothing, retrying silently forever (miner e2e 2026-11-13).
//
// Now: a machine whose worker-<hostname> has already mined successfully keeps that id (no
// identity change for running miners); everyone else gets worker-<hostname>-<6 hex of this
// node's wallet>, which is unique per wallet. The choice is persisted so it never changes.
func (a *app) defaultPoolWorkerID() string {
	hostID := "worker-local-01"
	if h, err := os.Hostname(); err == nil && strings.TrimSpace(h) != "" {
		hostID = "worker-" + sanitizeWorkerIDHost(h)
	}
	logDir := filepath.Join(resolveWorkerRepoRoot(strings.TrimSpace(os.Getenv("HACKME_DATA_DIR"))), "logs")
	if workerIDHasMined(logDir, hostID) {
		return a.persistWorkerID(hostID)
	}
	addr := ""
	if a != nil && a.signer != nil {
		addr = strings.TrimSpace(a.signer.Address())
	}
	suffix := strings.ToLower(strings.TrimPrefix(addr, "HMC-"))
	if len(suffix) < 6 {
		return hostID
	}
	return a.persistWorkerID(hostID + "-" + suffix[:6])
}

// workerIDHasMined reports whether a workerpoh run log for exactly this id shows an accepted
// submit. Run logs are named workerpoh-<id>-<UTC timestamp>.log.
func workerIDHasMined(logDir, id string) bool {
	matches, _ := filepath.Glob(filepath.Join(logDir, "workerpoh-"+id+"-2*.log"))
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	for i, p := range matches {
		if i >= 20 {
			break
		}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		if st, err := f.Stat(); err == nil && st.Size() > 256<<10 {
			_, _ = f.Seek(-256<<10, io.SeekEnd)
		}
		b, _ := io.ReadAll(io.LimitReader(f, 256<<10))
		_ = f.Close()
		if bytes.Contains(b, []byte("submit ok")) {
			return true
		}
	}
	return false
}

// persistWorkerID writes WORKER_ID to the node env file once so restarts keep the identity.
func (a *app) persistWorkerID(id string) string {
	_ = os.Setenv("WORKER_ID", id)
	p := rigProfileEnvPath()
	if p == "" {
		return id
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return id
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "WORKER_ID=") {
			return id
		}
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return id
	}
	prefix := ""
	if len(b) > 0 && b[len(b)-1] != '\n' {
		prefix = "\n"
	}
	_, werr := f.WriteString(prefix + "WORKER_ID=" + id + "\n")
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		log.Printf("hackme: pool worker id %s (saved to %s)", logsafe.ID(id), logsafe.ID(p))
	} else {
		log.Printf("hackme: pool worker id %s not saved: %s", logsafe.ID(id), logsafe.Err(werr))
	}
	return id
}
