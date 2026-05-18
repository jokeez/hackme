package main

import (
	"bufio"
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// applyFromCodeToolchainEnv prepends compiler paths from generated env files so
// POST /api/tasks/from_code works even when the node was started without sourcing
// .hackme-toolchains.env in the parent shell (Windows shortcuts, systemd, nohup).
func applyFromCodeToolchainEnv() {
	var applied []string
	for _, p := range fromCodeToolchainEnvCandidates() {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := mergeToolchainEnvFile(p); err != nil {
			log.Printf("hackme: toolchain env %s: %v", p, err)
			continue
		}
		applied = append(applied, p)
	}
	if len(applied) > 0 {
		log.Printf("hackme: from_code toolchain env loaded from %s", strings.Join(applied, ", "))
	}
}

func fromCodeToolchainEnvCandidates() []string {
	seen := make(map[string]bool)
	add := func(paths ...string) []string {
		var out []string
		for _, p := range paths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if abs, err := filepath.Abs(p); err == nil {
				p = abs
			}
			if seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
		return out
	}

	var all []string
	if v := strings.TrimSpace(os.Getenv("HACKME_TOOLCHAIN_ENV")); v != "" {
		all = append(all, add(v)...)
	}
	if exe, err := os.Executable(); err == nil {
		if sym, err := filepath.EvalSymlinks(exe); err == nil && sym != "" {
			exe = sym
		}
		dir := filepath.Dir(exe)
		all = append(all, add(
			filepath.Join(dir, "toolchains", ".env.toolchains"),
			filepath.Join(dir, ".hackme-toolchains.env"),
			filepath.Join(dir, ".env.toolchains"),
			filepath.Clean(filepath.Join(dir, "..", "..", ".hackme-toolchains.env")),
			filepath.Clean(filepath.Join(dir, "..", "toolchains", ".env.toolchains")),
		)...)
	}
	if wd, err := os.Getwd(); err == nil {
		all = append(all, add(
			filepath.Join(wd, "logs", "desktop", "toolchains", ".env.toolchains"),
			filepath.Join(wd, ".hackme-toolchains.env"),
		)...)
	}
	if p := strings.TrimSpace(os.Getenv("HACKME_PREFIX")); p != "" {
		all = append(all, add(filepath.Join(p, ".env.toolchains"))...)
	}
	all = append(all, add("/opt/hackme/.env.toolchains")...)
	return all
}

func mergeToolchainEnvFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "export ") {
			line = strings.TrimSpace(line[7:])
		}
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:i])
		val := strings.TrimSpace(line[i+1:])
		if key == "" {
			continue
		}
		switch strings.ToUpper(key) {
		case "PATH":
			_ = os.Setenv("PATH", mergePathList(val, os.Getenv("PATH")))
		default:
			if _, exists := os.LookupEnv(key); !exists {
				_ = os.Setenv(key, val)
			}
		}
	}
	return sc.Err()
}

func mergePathList(prefix, existing string) string {
	sep := string(os.PathListSeparator)
	var merged []string
	merged = append(merged, splitPathList(prefix)...)
	merged = append(merged, splitPathList(existing)...)
	seen := make(map[string]bool, len(merged))
	var out []string
	for _, p := range merged {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return strings.Join(out, sep)
}

func splitPathList(s string) []string {
	if s == "" {
		return nil
	}
	sep := string(os.PathListSeparator)
	if strings.Contains(s, sep) {
		return strings.Split(s, sep)
	}
	// Generated files often use ':' on Windows for portability; accept both.
	if os.PathSeparator == '\\' && strings.Contains(s, ":") {
		return strings.Split(s, ":")
	}
	return []string{s}
}
