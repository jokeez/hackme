package fuzzingcli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// TokenConfigPath returns the default developer token file location.
func TokenConfigPath() string {
	if v := strings.TrimSpace(os.Getenv("HACKME_DEVELOPER_TOKEN_FILE")); v != "" {
		return v
	}
	if h := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); h != "" {
		return filepath.Join(h, "hackme", "developer.token")
	}
	if runtime.GOOS == "windows" {
		if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
			return filepath.Join(appData, "HackMe", "developer.token")
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".config", "hackme", "developer.token")
	}
	return "developer.token"
}

// BuildHelperCandidates returns paths to hackme-fuzzing-build next to the main CLI binary.
func BuildHelperCandidates() []string {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	dir := filepath.Dir(exe)
	name := filepath.Base(exe)
	var out []string
	if strings.Contains(name, "hackme-fuzzing") {
		repl := strings.Replace(name, "hackme-fuzzing", "hackme-fuzzing-build", 1)
		out = append(out, filepath.Join(dir, repl))
	}
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	out = append(out, filepath.Join(dir, "hackme-fuzzing-build"+suffix))
	return out
}
