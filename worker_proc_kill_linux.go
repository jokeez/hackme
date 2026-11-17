//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Pool worker stop used `pkill -f "workerpoh "` / `"workerfuzz "`: a substring match over every
// process command line. It SIGTERMed unrelated processes whose arguments merely contained the
// word (a multi-package `go test … hackme/cmd/workerfuzz …` run died to it), and as root it
// reached other users' processes. On Linux we match exactly via /proc instead:
//   - binaries by the basename of /proc/<pid>/exe (the real executable, not argv);
//   - helper scripts by an argv element whose basename equals the script name;
//   - same uid as this node only; never this process or its parent.

var poolWorkerBinaryNames = []string{"workerpoh", "workerpoh-cuda", "workerpoh-opencl", "workerpoh-cpu"}
var poolWorkerScriptNames = []string{"worker_autostart.sh", "worker_loop.sh"}
var workerfuzzBinaryNames = []string{"workerfuzz"}

// procMatchesWorker reports whether pid runs one of the named binaries or scripts.
func procMatchesWorker(pid int, binaries, scripts []string) bool {
	if exe, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe"); err == nil {
		base := filepath.Base(strings.TrimSuffix(exe, " (deleted)"))
		for _, n := range binaries {
			if base == n {
				return true
			}
		}
	}
	if len(scripts) == 0 {
		return false
	}
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return false
	}
	for _, arg := range strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00") {
		base := filepath.Base(arg)
		for _, s := range scripts {
			if base == s {
				return true
			}
		}
	}
	return false
}

// signalWorkerProcs signals every matching process of this uid. allow (tests) restricts the
// candidate pids; nil means all. Returns the number of processes signalled.
func signalWorkerProcs(binaries, scripts []string, sig syscall.Signal, allow func(pid int) bool) int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	self, parent, uid := os.Getpid(), os.Getppid(), os.Getuid()
	n := 0
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 1 || pid == self || pid == parent {
			continue
		}
		if allow != nil && !allow(pid) {
			continue
		}
		var st syscall.Stat_t
		if err := syscall.Stat("/proc/"+e.Name(), &st); err != nil || int(st.Uid) != uid {
			continue
		}
		if !procMatchesWorker(pid, binaries, scripts) {
			continue
		}
		if syscall.Kill(pid, sig) == nil {
			n++
		}
	}
	return n
}

func killPoolWorkerProcs(allow func(pid int) bool) {
	signalWorkerProcs(poolWorkerBinaryNames, poolWorkerScriptNames, syscall.SIGTERM, allow)
	signalWorkerProcs(poolWorkerBinaryNames, nil, syscall.SIGKILL, allow)
}

func killWorkerfuzzProcs(allow func(pid int) bool) {
	signalWorkerProcs(workerfuzzBinaryNames, nil, syscall.SIGTERM, allow)
}

// procWorkerID returns the worker id a worker process runs as: the value of -worker/--worker
// on its command line, else WORKER_ID from its environment (helper scripts).
func procWorkerID(pid int) string {
	ps := strconv.Itoa(pid)
	if raw, err := os.ReadFile("/proc/" + ps + "/cmdline"); err == nil {
		args := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
		for i, a := range args {
			switch {
			case (a == "-worker" || a == "--worker") && i+1 < len(args):
				return strings.TrimSpace(args[i+1])
			case strings.HasPrefix(a, "-worker="):
				return strings.TrimSpace(strings.TrimPrefix(a, "-worker="))
			case strings.HasPrefix(a, "--worker="):
				return strings.TrimSpace(strings.TrimPrefix(a, "--worker="))
			}
		}
	}
	if raw, err := os.ReadFile("/proc/" + ps + "/environ"); err == nil {
		for _, kv := range strings.Split(string(raw), "\x00") {
			if strings.HasPrefix(kv, "WORKER_ID=") {
				return strings.TrimSpace(strings.TrimPrefix(kv, "WORKER_ID="))
			}
		}
	}
	return ""
}

// ownedByWorkerIDs allows only processes running as one of ids (or a fleet slot id-N of
// one of them). Nil when ids is empty: callers must then not signal external workers at all.
func ownedByWorkerIDs(ids []string) func(pid int) bool {
	var clean []string
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			clean = append(clean, id)
		}
	}
	if len(clean) == 0 {
		return nil
	}
	return func(pid int) bool { return workerIDOwned(procWorkerID(pid), clean) }
}
