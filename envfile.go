package main

import (
	"bufio"
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// loadHackmeDotEnv loads KEY=VALUE pairs from optional files next to the executable
// (current working directory is already set to the exe dir on Windows, see workdir.go).
// Existing process environment wins — never overrides a variable already set.
func loadHackmeDotEnv() {
	for _, name := range []string{".env", "hackme.env"} {
		p := filepath.Clean(name)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := parseEnvFileIntoOSEnv(p); err != nil {
			log.Printf("hackme: env file %s: %v", p, err)
			continue
		}
		log.Printf("hackme: loaded environment keys from %s (only unset vars)", p)
	}
}

func parseEnvFileIntoOSEnv(path string) error {
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
		if strings.HasPrefix(val, `"`) && strings.HasSuffix(val, `"`) && len(val) >= 2 {
			val = strings.TrimSuffix(strings.TrimPrefix(val, `"`), `"`)
		} else if strings.HasPrefix(val, `'`) && strings.HasSuffix(val, `'`) && len(val) >= 2 {
			val = strings.TrimSuffix(strings.TrimPrefix(val, `'`), `'`)
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, val)
	}
	return sc.Err()
}
