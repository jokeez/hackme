package fuzzupstream

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MSANReport is a triage-only MemorySanitizer pass over a seed corpus.
// Verdict is never CVE_CANDIDATE — MSAN needs human confirmation before any claim.
type MSANReport struct {
	TargetID       string         `json:"target_id"`
	Driver         string         `json:"driver"`
	HarnessVariant string         `json:"harness_variant,omitempty"`
	BinaryPath     string         `json:"binary_path,omitempty"`
	SeedsTried     int            `json:"seeds_tried"`
	Hits           []CrashFinding `json:"hits"`
	Verdict        string         `json:"verdict"` // CLEAN | MSAN_TRIAGE
	Note           string         `json:"note"`
	ElapsedSec     float64        `json:"elapsed_sec"`
}

// RunMSANCorpusSession builds an MSAN driver and replays seeds from ExtraSeedDirs / corpus.
func RunMSANCorpusSession(ctx context.Context, repoRoot string, t Target, outDir string, maxSeeds int, wallSec int) (*MSANReport, error) {
	if repoRoot == "" {
		repoRoot = "."
	}
	t = ApplyHarnessVariant(t, "")
	if maxSeeds <= 0 {
		maxSeeds = 256
	}
	if wallSec <= 0 {
		wallSec = 120
	}
	if outDir == "" {
		outDir = filepath.Join(repoRoot, "reports", "oss-cve-msan", t.ID)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}

	start := time.Now()
	bin, _, err := BuildTargetMSAN(ctx, repoRoot, t)
	if err != nil {
		return nil, err
	}

	var seeds [][]byte
	seen := map[string]struct{}{}
	for _, dir := range loadExtraSeedDirs(repoRoot, t.ID) {
		for _, s := range loadCorpusDir(dir, maxSeeds) {
			k := corpusKey(s)
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			seeds = append(seeds, s)
			if len(seeds) >= maxSeeds {
				break
			}
		}
		if len(seeds) >= maxSeeds {
			break
		}
	}
	if len(seeds) == 0 {
		seeds = [][]byte{[]byte("{}"), []byte("[]"), {0x00}, {0x80}}
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(wallSec)*time.Second)
	defer cancel()

	rep := &MSANReport{
		TargetID:       t.ID,
		Driver:         t.Driver,
		HarnessVariant: t.HarnessVariant,
		BinaryPath:     bin,
		Verdict:        "CLEAN",
		Note:           "MSAN hits require triage; do not auto-claim CVE",
	}
	crashDir := filepath.Join(outDir, "hits")
	_ = os.MkdirAll(crashDir, 0o755)
	const maxMSANHits = 32

	for i, seed := range seeds {
		if runCtx.Err() != nil {
			break
		}
		if len(rep.Hits) >= maxMSANHits {
			break
		}
		rep.SeedsTried++
		crash, info, tail, err := RunInputDetailed(runCtx, bin, seed, RunInputOpts{MaxInput: 65536})
		if err != nil || !crash {
			continue
		}
		if info.Class == "" {
			info = ClassifySanitizer(tail)
		}
		if info.Class != "msan" && !strings.Contains(strings.ToLower(tail), "memorysanitizer") {
			// Non-MSAN crashes under MSAN build still go to triage bucket.
			if info.Class == "" {
				info.Class = "msan"
				info.Subtype = "signal"
			}
		}
		cf := CrashFinding{
			TargetID:         t.ID,
			Title:            t.Title,
			Repo:             t.Repo,
			InputHex:         hex.EncodeToString(seed),
			InputLen:         len(seed),
			Sanitizer:        info.Raw,
			SanitizerClass:   info.Class,
			SanitizerSubtype: info.Subtype,
			SanitizerLabel:   SanitizerDisplayLabel(info),
			Tail:             truncateTail(tail, 2000),
			Iteration:        i,
			CWE:              t.CWE,
			Disclosure:       "MSAN_TRIAGE",
		}
		p, _ := SaveCrashArtifact(crashDir, cf)
		cf.ArtifactPath = p
		rep.Hits = append(rep.Hits, cf)
		// Wire interesting inputs into durable research corpus / L2 seeds.
		_ = persistResearchSeed(repoRoot, t.ID, seed, "msan")
	}

	if len(rep.Hits) > 0 {
		rep.Verdict = "MSAN_TRIAGE"
	}
	rep.ElapsedSec = time.Since(start).Seconds()
	b, _ := json.MarshalIndent(rep, "", "  ")
	_ = os.WriteFile(filepath.Join(outDir, "MSAN_REPORT.json"), append(b, '\n'), 0o644)
	return rep, nil
}

func truncateTail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func persistResearchSeed(repoRoot, targetID string, seed []byte, prefix string) error {
	if len(seed) == 0 {
		return nil
	}
	id, ok := SanitizeCatalogID(targetID)
	if !ok {
		return nil
	}
	prefix, ok = SanitizeCatalogID(prefix)
	if !ok {
		prefix = "seed"
	}
	dirs := []string{
		filepath.Join(repoRoot, "reports", "oss-cve-libfuzzer", id, "corpus"),
		filepath.Join(repoRoot, ".cache", "hunt-lf-seeds", id),
	}
	name := fmt.Sprintf("%s-%s.bin", prefix, corpusKey(seed))
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			continue
		}
		_ = os.WriteFile(filepath.Join(dir, name), seed, 0o600)
	}
	return nil
}
