// zz_asanhunt runs the production libFuzzer hunt session against one catalog target and
// reports what it found: corpus growth, crash artifacts, and the stable bucket of each
// crash. Scratch tool — it exercises the same path a pool lease takes, so a finding here
// is a finding the fleet would have made.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzupstream"
	"hackme/internal/hunt"
)

func main() {
	target := flag.String("target", "", "catalog target id")
	wall := flag.Int("wall", 180, "wall seconds")
	flag.Parse()
	if strings.TrimSpace(*target) == "" {
		fmt.Fprintln(os.Stderr, "need -target")
		os.Exit(2)
	}
	repoRoot := hunt.RepoRoot()
	m, err := fuzzupstream.LoadManifest(repoRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "manifest:", err)
		os.Exit(1)
	}
	var t *fuzzupstream.Target
	for i := range m.Targets {
		if m.Targets[i].ID == *target {
			t = &m.Targets[i]
			break
		}
	}
	if t == nil {
		fmt.Fprintf(os.Stderr, "target %q not in catalog\n", *target)
		os.Exit(1)
	}
	fmt.Printf("target=%s ref=%s driver=%s\n", t.ID, t.Ref, t.Driver)

	// Which rail will carry it: the in-process shim, or the fork wrapper.
	if bin, ok := hunt.CachedInprocLibFuzzerBin(repoRoot, t.ID); ok {
		fmt.Printf("rail=in-process (cached %s)\n", filepath.Base(bin))
	} else {
		fmt.Println("rail=will build (in-process if the driver supports it)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*wall+900)*time.Second)
	defer cancel()
	start := time.Now()
	imported, err := hunt.RunPersistentLibFuzzerSession(ctx, repoRoot, t.ID, *wall)
	elapsed := time.Since(start).Round(time.Second)
	if err != nil {
		fmt.Printf("session error after %s: %v\n", elapsed, err)
	} else {
		fmt.Printf("session ok in %s, corpus imported=%d\n", elapsed, imported)
	}

	// Crash artifacts land beside the corpus.
	corpus := filepath.Join(repoRoot, "reports", "oss-cve-libfuzzer", t.ID, "corpus")
	crashDir := filepath.Join(corpus, ".hunt-run", "crashes")
	ents, _ := os.ReadDir(crashDir)
	var crashes []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "crash-") {
			crashes = append(crashes, filepath.Join(crashDir, e.Name()))
		}
	}
	sort.Strings(crashes)
	files, _ := os.ReadDir(corpus)
	n := 0
	for _, f := range files {
		if !f.IsDir() {
			n++
		}
	}
	fmt.Printf("corpus files=%d  crash artifacts=%d\n", n, len(crashes))

	stderrPath := filepath.Join(corpus, ".hunt-run", "session.stderr")
	raw, _ := os.ReadFile(stderrPath)
	if len(raw) > 0 {
		for _, line := range strings.Split(string(raw), "\n") {
			l := strings.TrimSpace(line)
			if strings.Contains(l, "ERROR: AddressSanitizer") || strings.Contains(l, "SUMMARY: AddressSanitizer") ||
				strings.Contains(l, "ERROR: libFuzzer") || strings.HasPrefix(l, "==") && strings.Contains(l, "ERROR") {
				fmt.Println("  sanitizer:", l)
			}
		}
		// Only bucket when there is something to bucket: the session log alone always
		// hashes to something, which read like a finding when there was none.
		if len(crashes) > 0 {
			if bucket := strings.TrimSpace(fuzzengine.StableCrashBucket("native_crash", string(raw))); bucket != "" {
				fmt.Println("  session-log bucket:", bucket)
			}
		}
	}
	for _, c := range crashes {
		st, err := os.Stat(c)
		if err != nil {
			continue
		}
		fmt.Printf("  artifact %s (%d bytes)\n", filepath.Base(c), st.Size())
	}
}
