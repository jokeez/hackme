package workerfuzzloop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/hunt"
	"hackme/internal/poolfuzz"
)

// ResearchSlotConfig is the hybrid worker-local libFuzzer persist window (Stage D).
// Default ON for Hunt claims (same escape-hatch pattern as HybridFuzzEnabled).
// Missing clang/LF soft-skips without failing Dig/Hunt submit.
// Does not enable HACKME_POOL_SEED_FROM_RESEARCH (customer Dig seed feed stays OFF).
type ResearchSlotConfig struct {
	Enabled       bool
	AllowDig      bool // HACKME_WORKER_RESEARCH_SLOT_DIG (default off)
	WindowSec     int  // short local LF persist window
	MaxDeltaFiles int
	TargetID      string // HACKME_WORKER_RESEARCH_TARGET override; Hunt uses claim target
}

// ResearchSlotFromEnv reads HACKME_WORKER_RESEARCH_SLOT.
// Empty / unset → enabled (hybrid fleet default). Explicit 0|false|no|off → disabled.
func ResearchSlotFromEnv() ResearchSlotConfig {
	cfg := ResearchSlotConfig{
		WindowSec:     EnvInt("HACKME_WORKER_RESEARCH_WINDOW_SEC", 30),
		MaxDeltaFiles: EnvInt("HACKME_WORKER_RESEARCH_MAX_DELTA", 32),
		TargetID:      strings.TrimSpace(os.Getenv("HACKME_WORKER_RESEARCH_TARGET")),
	}
	v := strings.TrimSpace(os.Getenv("HACKME_WORKER_RESEARCH_SLOT"))
	if v == "" {
		cfg.Enabled = true
	} else {
		cfg.Enabled = !Falsy(v)
	}
	cfg.AllowDig = Truthy(os.Getenv("HACKME_WORKER_RESEARCH_SLOT_DIG"))
	if cfg.WindowSec < 5 {
		cfg.WindowSec = 5
	}
	if cfg.WindowSec > 120 {
		cfg.WindowSec = 120
	}
	if cfg.MaxDeltaFiles < 1 {
		cfg.MaxDeltaFiles = 1
	}
	if cfg.MaxDeltaFiles > 256 {
		cfg.MaxDeltaFiles = 256
	}
	return cfg
}

// ResearchSlotResult is the outcome of a local research window.
type ResearchSlotResult struct {
	Ran           bool
	CorpusDeltas  int
	Crashes       int
	Findings      int
	SkippedReason string
	Err           string
}

// ResearchSlotRun is optional wiring for a claim (lease still held).
type ResearchSlotRun struct {
	Config     ResearchSlotConfig
	CoordURL   string
	Token      string
	WorkerID   string
	MinerAddr  string
	HTTPClient *http.Client
	Claim      ClaimResp
	RepoRoot   string
}

// researchLFRunner runs a bounded persistent LF session. Overridable in tests.
var researchLFRunner = defaultResearchLFRunner

func defaultResearchLFRunner(ctx context.Context, repoRoot, targetID string, wallSec int) (int, error) {
	return hunt.RunPersistentLibFuzzerSession(ctx, repoRoot, targetID, wallSec)
}

// MaybeRunResearchSlot runs a bounded LF persist window and submits corpus deltas +
// crash artifacts to the coordinator (lease-bound). Hunt hybrid default ON.
func MaybeRunResearchSlot(ctx context.Context, run ResearchSlotRun) ResearchSlotResult {
	cfg := run.Config
	if !cfg.Enabled {
		return ResearchSlotResult{SkippedReason: "disabled"}
	}
	if err := ctx.Err(); err != nil {
		return ResearchSlotResult{SkippedReason: "ctx_done"}
	}
	huntClaim := IsHuntClaim(run.Claim)
	if !huntClaim && !cfg.AllowDig {
		return ResearchSlotResult{SkippedReason: "hunt_only"}
	}
	targetID := strings.TrimSpace(run.Claim.UpstreamTargetID)
	if targetID == "" {
		targetID = cfg.TargetID
	}
	if targetID == "" {
		return ResearchSlotResult{SkippedReason: "no_target"}
	}
	if strings.TrimSpace(run.CoordURL) == "" || strings.TrimSpace(run.WorkerID) == "" {
		return ResearchSlotResult{SkippedReason: "no_coord"}
	}
	if run.Claim.CampaignID == "" || run.Claim.ItemID <= 0 {
		return ResearchSlotResult{SkippedReason: "no_lease_bind"}
	}

	repoRoot := strings.TrimSpace(run.RepoRoot)
	if repoRoot == "" {
		repoRoot = hunt.RepoRoot()
	}
	corpusDir := hunt.PersistentLibFuzzerCorpusDir(repoRoot, targetID)
	before, err := snapshotCorpusHashes(corpusDir)
	if err != nil {
		// Empty dir is fine — session may create it.
		before = map[string]string{}
	}

	if _, err := researchLFRunner(ctx, repoRoot, targetID, cfg.WindowSec); err != nil {
		// LF missing / harness build failure: skip without failing the claim path.
		return ResearchSlotResult{SkippedReason: "lf_session", Err: err.Error()}
	}

	deltas, crashes, err := collectResearchDelta(corpusDir, before, cfg.MaxDeltaFiles)
	if err != nil {
		return ResearchSlotResult{SkippedReason: "collect", Err: err.Error()}
	}
	if len(deltas) == 0 && len(crashes) == 0 {
		return ResearchSlotResult{Ran: true, SkippedReason: "no_delta"}
	}

	ns := poolfuzz.ResearchNamespaceForTarget(targetID)
	cl := run.HTTPClient
	if cl == nil {
		cl = http.DefaultClient
	}
	accepted, findings, upErr := uploadResearchCorpusDelta(ctx, cl, run.CoordURL, run.Token, run.WorkerID, run.MinerAddr,
		run.Claim.CampaignID, run.Claim.ItemID, ns, deltas, crashes)
	if upErr != nil {
		return ResearchSlotResult{
			Ran: true, CorpusDeltas: len(deltas), Crashes: len(crashes),
			SkippedReason: "upload", Err: upErr.Error(),
		}
	}
	return ResearchSlotResult{
		Ran:          true,
		CorpusDeltas: accepted,
		Crashes:      len(crashes),
		Findings:     findings,
	}
}

func snapshotCorpusHashes(dir string) (map[string]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(ents))
	for _, e := range ents {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(p)
		if err != nil || len(b) == 0 {
			continue
		}
		sum := sha256.Sum256(b)
		out[e.Name()] = hex.EncodeToString(sum[:])
	}
	return out, nil
}

func collectResearchDelta(dir string, before map[string]string, maxFiles int) (seeds []fuzzengine.PoolCorpusSeed, crashes [][]byte, err error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	if maxFiles < 1 {
		maxFiles = 32
	}
	for _, e := range ents {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		name := e.Name()
		low := strings.ToLower(name)
		p := filepath.Join(dir, name)
		b, rerr := os.ReadFile(p)
		if rerr != nil || len(b) == 0 || len(b) > 65536 {
			continue
		}
		sum := sha256.Sum256(b)
		h := hex.EncodeToString(sum[:])
		if strings.HasPrefix(low, "crash-") || strings.HasPrefix(low, "oom-") || strings.HasPrefix(low, "timeout-") ||
			strings.HasPrefix(low, "leak-") {
			if len(crashes) < 16 {
				crashes = append(crashes, b)
			}
			continue
		}
		if prev, ok := before[name]; ok && prev == h {
			continue
		}
		if len(seeds) >= maxFiles {
			continue
		}
		seeds = append(seeds, fuzzengine.PoolCorpusSeed{
			Input:      fuzzengine.PackInputBytesToU64(b),
			InputBytes: b,
			Energy:     2,
		})
	}
	return seeds, crashes, nil
}

func uploadResearchCorpusDelta(ctx context.Context, cl *http.Client, base, token, workerID, minerAddr, campaignID string, itemID int64, namespace string, seeds []fuzzengine.PoolCorpusSeed, crashes [][]byte) (accepted, findings int, err error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	payloadSeeds := make([]map[string]any, 0, len(seeds))
	for _, s := range seeds {
		payloadSeeds = append(payloadSeeds, map[string]any{
			"input_u64":   s.Input,
			"input_bytes": base64.StdEncoding.EncodeToString(s.InputBytes),
			"energy":      s.Energy,
			"edge":        s.Edge,
			"path":        s.Path,
		})
	}
	crashB64 := make([]string, 0, len(crashes))
	for _, c := range crashes {
		crashB64 = append(crashB64, base64.StdEncoding.EncodeToString(c))
	}
	body, err := json.Marshal(map[string]any{
		"worker_id":     workerID,
		"campaign_id":   campaignID,
		"item_id":       itemID,
		"namespace":     namespace,
		"miner_address": minerAddr,
		"seeds":         payloadSeeds,
		"crashes":       crashB64,
	})
	if err != nil {
		return 0, 0, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, base+"/api/fuzz/work/corpus_delta", bytes.NewReader(body))
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Hackme-Admin-Token", token)
	}
	res, err := cl.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return 0, 0, fmt.Errorf("corpus_delta HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	var wrap struct {
		OK            bool `json:"ok"`
		SeedsAccepted int  `json:"seeds_accepted"`
		Findings      int  `json:"findings"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return 0, 0, err
	}
	if !wrap.OK {
		return 0, 0, fmt.Errorf("corpus_delta rejected: %s", strings.TrimSpace(string(raw)))
	}
	return wrap.SeedsAccepted, wrap.Findings, nil
}
