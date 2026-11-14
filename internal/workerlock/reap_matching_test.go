package workerlock

import (
	"bufio"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Two rigs share one lock dir (operator's systemd GPU miner + a second node). Reaping for one
// worker id must leave the other rig's live lock holder alone (2026-11-14 restart loop).
func TestReapAllMatchingSparesOtherRigs(t *testing.T) {
	if os.Getenv("WORKERLOCK_HOLDER2") == "1" {
		g, err := Acquire("workerpoh", "worker-operator-gpu", os.Getenv("WORKERLOCK_DIR"))
		if err != nil {
			os.Exit(1)
		}
		defer g.Release()
		os.Stdout.WriteString("READY\n")
		time.Sleep(45 * time.Second)
		return
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestReapAllMatchingSparesOtherRigs$")
	cmd.Env = append(os.Environ(), "WORKERLOCK_HOLDER2=1", "WORKERLOCK_DIR="+dir)
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	sc := bufio.NewScanner(out)
	ready := false
	for sc.Scan() {
		if strings.Contains(sc.Text(), "READY") {
			ready = true
			break
		}
	}
	if !ready {
		t.Fatal("holder not ready")
	}
	res, err := ReapAllMatching(dir, func(w string) bool { return w == "worker-other-node" })
	if err != nil || len(res) != 0 {
		t.Fatalf("reaped foreign rig: %+v %v", res, err)
	}
	if !Held("workerpoh", "worker-operator-gpu", dir) || !processAlive(cmd.Process.Pid) {
		t.Fatal("foreign live miner was killed")
	}
}
