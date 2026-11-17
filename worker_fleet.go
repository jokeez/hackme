package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"hackme/internal/gpuhost"
)

func fleetEnabledFromEnv() bool {
	v := strings.TrimSpace(os.Getenv("HACKME_GPU_FLEET"))
	if v == "" {
		return true
	}
	v = strings.ToLower(v)
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func buildWorkerFleetPlan(repoRoot, workerBase string) gpuhost.FleetPlan {
	explicit := -1
	if v := strings.TrimSpace(os.Getenv("HACKME_GPU_DEVICE")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			explicit = n
		}
	}
	forceOCL := strings.TrimSpace(os.Getenv("HACKME_FORCE_OPENCL")) == "1" ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("HACKME_FORCE_OPENCL")), "true")
	disabled := strings.TrimSpace(os.Getenv("HACKME_GPU_DISABLE")) == "1"
	return gpuhost.BuildFleetPlan(gpuhost.FleetPlanInput{
		RepoRoot:       repoRoot,
		WorkerIDBase:   workerBase,
		HybridMode:     strings.TrimSpace(os.Getenv("HACKME_GPU_HYBRID")),
		FleetEnabled:   fleetEnabledFromEnv(),
		FleetMax:       20,
		ForceOpenCL:    forceOCL,
		GPUDisabled:    disabled,
		ExplicitDevice: explicit,
	})
}

func resolveWorkerExeForSlot(repoRoot, backend string) (string, error) {
	switch strings.ToLower(backend) {
	case "cuda":
		for _, name := range []string{"workerpoh-cuda.exe", "workerpoh-cuda", "workerpoh.exe", "workerpoh"} {
			p := filepath.Join(repoRoot, name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p, nil
			}
			if exe, err := os.Executable(); err == nil {
				p = filepath.Join(filepath.Dir(exe), name)
				if st, err := os.Stat(p); err == nil && !st.IsDir() {
					return p, nil
				}
			}
		}
	case "opencl":
		for _, name := range []string{"workerpoh-opencl.exe", "workerpoh-opencl", "workerpoh.exe", "workerpoh"} {
			p := filepath.Join(repoRoot, name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p, nil
			}
			if exe, err := os.Executable(); err == nil {
				p = filepath.Join(filepath.Dir(exe), name)
				if st, err := os.Stat(p); err == nil && !st.IsDir() {
					return p, nil
				}
			}
		}
	}
	return resolveWorkerpohExePathForBackend(backend)
}

func startWorkerFleetProcesses(repoRoot, coordURL, coordToken, workerBase string, batchSize uint64, logPath string, extraEnv []string) ([]*exec.Cmd, error) {
	plan := buildWorkerFleetPlan(repoRoot, workerBase)
	if plan.TotalSlots <= 1 && len(plan.Slots) == 1 {
		return nil, fmt.Errorf("single_slot")
	}
	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	var cmds []*exec.Cmd
	for _, slot := range plan.Slots {
		wid := workerBase + slot.WorkerSuffix
		exe, err := resolveWorkerExeForSlot(repoRoot, slot.Backend)
		if err != nil {
			_ = logF.Close()
			for _, c := range cmds {
				if c.Process != nil {
					_ = c.Process.Kill()
				}
			}
			return nil, err
		}
		slotBatch := batchSize
		if v := strings.TrimSpace(slot.Env["HACKME_WORKER_BATCH_SIZE"]); v != "" {
			if x, err := strconv.ParseUint(v, 10, 64); err == nil && x > 0 {
				slotBatch = x
			}
		}
		args := []string{
			"-coord", coordURL,
			"-token", coordToken,
			"-worker", wid,
			"-batch", strconv.FormatUint(slotBatch, 10),
			"-gpu-backend", slot.Backend,
			"-gpu-device", strconv.Itoa(slot.DeviceIndex),
		}
		cmd := exec.Command(exe, args...)
		cmd.Stdout = logF
		cmd.Stderr = logF
		env := append([]string{}, os.Environ()...)
		env = append(env, extraEnv...)
		for k, v := range slot.Env {
			env = append(env, k+"="+v)
		}
		if slot.Backend == "opencl" {
			env = append(env, "HACKME_FORCE_OPENCL=1")
		}
		cmd.Env = env
		configurePoolWorkerCmd(cmd)
		if err := cmd.Start(); err != nil {
			_ = logF.Close()
			for _, c := range cmds {
				if c.Process != nil {
					_ = c.Process.Kill()
				}
			}
			return nil, err
		}
		cmds = append(cmds, cmd)
	}
	_ = logF.Close()
	return cmds, nil
}

// killExternalWorkerFleet stops pool workers this node owns: processes running as one of ids
// (base id or a fleet slot id-N). It used to stop every workerpoh of this uid on the machine,
// so a second node (an Orders/test node, a devnet) restarting its own worker every ~45 s took
// down the operator's systemd GPU miner and all named fleet units with it (2026-11-14).
// With no known id it signals nothing; lock-file reaping still handles real orphans.
func killExternalWorkerFleet(repoRoot string, ids []string) {
	if runtime.GOOS == "windows" {
		// taskkill /IM has no per-command-line filter; Windows nodes are single-rig.
		_ = exec.Command("taskkill", "/F", "/IM", "workerpoh.exe").Run()
		_ = exec.Command("taskkill", "/F", "/IM", "workerpoh-cuda.exe").Run()
		_ = exec.Command("taskkill", "/F", "/IM", "workerpoh-opencl.exe").Run()
		_ = exec.Command("taskkill", "/F", "/IM", "workerpoh-cpu.exe").Run()
		return
	}
	// Exact match only (see worker_proc_kill_linux.go): binaries by executable name, helper
	// scripts by script file name — never a substring of an arbitrary command line.
	if allow := ownedByWorkerIDs(ids); allow != nil {
		killPoolWorkerProcs(allow)
	}
	if repoRoot != "" {
		_ = os.Remove(filepath.Join(repoRoot, "logs", ".worker_autostart.lock"))
	}
	_ = os.Remove(filepath.Join("logs", ".worker_autostart.lock"))
}

// workerIDOwned: got equals one of ids, or is one of its fleet slots: "<id>-<N>",
// "<id>-cuda<N>" / "-ocl<N>" / "-gpu<N>" (gpuhost fleet plan) or "<id>-fuzz".
func workerIDOwned(got string, ids []string) bool {
	got = strings.TrimSpace(got)
	if got == "" {
		return false
	}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if got == id {
			return true
		}
		rest, ok := strings.CutPrefix(got, id+"-")
		if !ok || rest == "" {
			continue
		}
		if rest == "fuzz" {
			return true
		}
		for _, p := range []string{"cuda", "ocl", "gpu", ""} {
			if n, ok := strings.CutPrefix(rest, p); ok && n != "" && allDigits(n) {
				return true
			}
		}
	}
	return false
}

func allDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}
