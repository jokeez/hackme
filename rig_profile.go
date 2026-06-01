package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"hackme/internal/gpuhost"
	"hackme/internal/gputune"
)

var rigProfilePreserveKeys = map[string]bool{
	"HACKME_ADMIN_TOKEN":                true,
	"HACKME_POOL_COORDINATOR_TOKEN":     true,
	"HACKME_PUBLIC_AUTHORITY_BASE":      true,
	"HACKME_CANONICAL_CHAIN_URL":        true,
	"HACKME_REQUIRE_ADMIN_TOKEN":        true,
	"HACKME_DESKTOP_MODE":               true,
	"HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN": true,
}

func rigProfileEnvPath() string {
	root := resolveWorkerRepoRoot(strings.TrimSpace(os.Getenv("HACKME_DATA_DIR")))
	for _, name := range []string{"hackme.env", ".env"} {
		if root != "" {
			p := filepath.Join(root, name)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		if _, err := os.Stat(name); err == nil {
			return name
		}
	}
	if root != "" {
		return filepath.Join(root, "hackme.env")
	}
	return "hackme.env"
}

func mergeRigProfileIntoEnvFile(profileID string) (gputune.RigProfile, error) {
	p, ok := gputune.GetRigProfile(profileID)
	if !ok {
		return gputune.RigProfile{}, errRigProfileNotFound
	}
	p = gputune.AdaptRigProfileForHost(p)
	path := rigProfileEnvPath()
	lines, err := readEnvFileLines(path)
	if err != nil && !os.IsNotExist(err) {
		return gputune.RigProfile{}, err
	}
	keySet := make(map[string]bool)
	for _, k := range gputune.RigProfileEnvKeys {
		keySet[k] = true
	}
	var out []string
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			out = append(out, line)
			continue
		}
		i := strings.IndexByte(trim, '=')
		if i <= 0 {
			out = append(out, line)
			continue
		}
		key := strings.TrimSpace(trim[:i])
		if rigProfilePreserveKeys[key] {
			out = append(out, line)
			continue
		}
		if keySet[key] {
			continue
		}
		out = append(out, line)
	}
	for _, k := range gputune.RigProfileEnvKeys {
		if v, ok := p.Env[k]; ok && strings.TrimSpace(v) != "" {
			out = append(out, k+"="+v)
		}
	}
	body := strings.Join(out, "\n")
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return gputune.RigProfile{}, err
	}
	for k, v := range p.Env {
		if rigProfilePreserveKeys[k] {
			continue
		}
		_ = os.Setenv(k, v)
	}
	return p, nil
}

func readEnvFileLines(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	var lines []string
	for _, line := range strings.Split(string(raw), "\n") {
		lines = append(lines, strings.TrimRight(line, "\r"))
	}
	return lines, nil
}

var errRigProfileNotFound = &rigProfileError{msg: "rig profile not found"}

type rigProfileError struct{ msg string }

func (e *rigProfileError) Error() string { return e.msg }

func collectLocalGPUNames() []string {
	names := gpuhost.CollectGPUNames()
	if len(names) > 0 {
		return names
	}
	for _, row := range queryNVIDIAMulti() {
		if strings.TrimSpace(row.Name) != "" {
			names = append(names, row.Name)
		}
	}
	return names
}

func (a *app) handleRigProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		active := strings.TrimSpace(os.Getenv("HACKME_RIG_PROFILE"))
		writeJSON(w, map[string]any{
			"profiles":      gputune.ListRigProfiles(),
			"active_id":     active,
			"env_file":      rigProfileEnvPath(),
			"platform_note": rigPlatformNote(),
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *app) handleRigProfilesDetect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	names := collectLocalGPUNames()
	rep := gpuhost.DetectHostGPUs()
	if len(rep.Names) == 0 && len(names) > 0 {
		rep.Names = names
	}
	p, ok := gputune.DetectRigProfile(names)
	if ok {
		p = gputune.AdaptRigProfileForHost(p)
	}
	rep = enrichHostReportWithProfile(rep)
	repoRoot := resolveWorkerRepoRoot(strings.TrimSpace(a.dataDir))
	backend := strings.TrimSpace(rep.SuggestedBackend)
	if backend == "" {
		backend = resolveAutoGPUBackend(repoRoot)
	}
	if ok && strings.TrimSpace(p.Env["HACKME_GPU_BACKEND"]) != "" {
		backend = strings.TrimSpace(p.Env["HACKME_GPU_BACKEND"])
	}
	writeJSON(w, map[string]any{
		"gpu_names":         names,
		"detected":          ok,
		"profile":           p,
		"profile_id":        p.ID,
		"platform_note":     rigPlatformNote(),
		"host":              rep,
		"suggested_backend": backend,
		"has_nvidia":        rep.HasNVIDIA,
		"has_amd":           rep.HasAMD,
		"has_intel":         rep.HasIntel,
		"hybrid":            rep.Hybrid,
		"vendor_summary":    rep.VendorSummary,
		"notes":             rep.Notes,
	})
}

type rigProfileApplyRequest struct {
	ProfileID     string `json:"profile_id"`
	RestartWorker bool   `json:"restart_worker"`
}

func (a *app) handleRigProfilesApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireAdminAuth(w, r) {
		return
	}
	logAdminAction(r, "rig_profile_apply")
	var req rigProfileApplyRequest
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	pid := strings.TrimSpace(req.ProfileID)
	if pid == "" {
		if p, ok := gputune.DetectRigProfile(collectLocalGPUNames()); ok {
			pid = p.ID
		}
	}
	if pid == "" {
		http.Error(w, "profile_id required (or no GPU match for auto-detect)", http.StatusBadRequest)
		return
	}
	p, err := mergeRigProfileIntoEnvFile(pid)
	if err != nil {
		if err == errRigProfileNotFound {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, "merge env: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = parseEnvFileIntoOSEnv(rigProfileEnvPath())
	writeJSON(w, map[string]any{
		"ok":             true,
		"profile":        p,
		"env_file":       rigProfileEnvPath(),
		"env_merged":     true,
		"restart_worker": req.RestartWorker,
		"platform_note":  rigPlatformNote(),
		"message":        "Rig profile merged into hackme.env. Stop/start pool worker (or restart HackMe) to pick up batch/GPU env.",
	})
}

func rigPlatformNote() string {
	if runtime.GOOS == "windows" {
		dir := "."
		if exe, err := os.Executable(); err == nil {
			dir = filepath.Dir(exe)
		}
		ocl := filepath.Join(dir, "workerpoh-opencl.exe")
		if st, err := os.Stat(ocl); err != nil || st.IsDir() {
			return "Windows: workerpoh-opencl.exe not bundled — profile uses HACKME_GPU_BACKEND=auto until OpenCL build ships; batch/cooldown/thermal env still apply."
		}
	}
	return ""
}

func appendWorkerEnvPassthrough(env []string) []string {
	for _, k := range gputune.RigProfileEnvKeys {
		if rigProfilePreserveKeys[k] {
			continue
		}
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

func hackmeEnvLacksRigTune() bool {
	lines, err := readEnvFileLines(rigProfileEnvPath())
	if err != nil {
		return true
	}
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "HACKME_RIG_PROFILE=") ||
			strings.HasPrefix(trim, "HACKME_WORKER_BATCH_SIZE=") {
			return false
		}
	}
	return true
}

func applyRigProfileAtStartup() {
	pid := strings.TrimSpace(os.Getenv("HACKME_RIG_PROFILE"))
	if pid == "" {
		autoOff := strings.EqualFold(strings.TrimSpace(os.Getenv("HACKME_RIG_PROFILE_AUTO")), "0") ||
			strings.EqualFold(strings.TrimSpace(os.Getenv("HACKME_RIG_PROFILE_AUTO")), "false")
		if autoOff {
			return
		}
		if !hackmeEnvLacksRigTune() {
			return
		}
		names := collectLocalGPUNames()
		if p, ok := gputune.DetectRigProfile(names); ok {
			pid = p.ID
			log.Printf("hackme: auto-detected rig profile %q from GPU %v", pid, names)
		} else {
			return
		}
	}
	if _, err := mergeRigProfileIntoEnvFile(pid); err != nil {
		log.Printf("hackme: rig profile %q: %v", pid, err)
		return
	}
	log.Printf("hackme: applied rig profile %q to %s", pid, rigProfileEnvPath())
}
