package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// loadEnvFileIntoEnviron reads KEY=VALUE lines and calls os.Setenv only when
// the variable is not already set in the process environment (shell exports win).
func loadEnvFileIntoEnviron(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	const maxLine = 256 * 1024
	buf := make([]byte, maxLine)
	sc.Buffer(buf, maxLine)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
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
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if os.Getenv(key) != "" {
			continue
		}
		if err := os.Setenv(key, val); err != nil {
			return fmt.Errorf("%s:%d: setenv %s: %w", path, lineNo, key, err)
		}
	}
	return sc.Err()
}

func applyTelegramEnvFiles(configPath string) error {
	if strings.TrimSpace(configPath) != "" {
		return loadEnvFileIntoEnviron(configPath)
	}
	if p := strings.TrimSpace(os.Getenv("HACKME_TELEGRAM_CONFIG")); p != "" {
		return loadEnvFileIntoEnviron(p)
	}
	for _, p := range []string{".env.telegram", "telegram_bot.env"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return loadEnvFileIntoEnviron(p)
		}
	}
	return nil
}
