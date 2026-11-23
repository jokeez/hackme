package workerfuzzloop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/hunt"
)

func TestCollectEngineDeltaCorpusEdgesUploadedOnce(t *testing.T) {
	dir := t.TempDir()
	prefix := []byte("0123456789abcdef0123456789abcdef")
	same := append(append([]byte{}, prefix...), []byte("-same")...)
	other := append(append([]byte{}, prefix...), []byte("-other")...)
	huge := bytes.Repeat([]byte("H"), 65536)
	over := bytes.Repeat([]byte("Z"), 65537)
	writeCorpus := func(name string, body []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeCorpus("a.bin", same)
	writeCorpus("b.bin", append([]byte(nil), same...))
	writeCorpus("c.bin", other)
	writeCorpus("empty.bin", nil)
	writeCorpus("huge.bin", huge)
	writeCorpus("over.bin", over)
	writeCorpus("boot.json", []byte("{}"))
	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "link.bin")); err != nil {
		t.Fatal(err)
	}

	seeds, _ := collectEngineDelta(hunt.EngineShardResult{CorpusDir: dir}, nil, 32)
	got := map[string]int{}
	for _, s := range seeds {
		got[string(s.InputBytes)]++
	}
	if got[string(same)] != 1 || got[string(other)] != 1 || got[string(huge)] != 1 || len(seeds) != 3 {
		t.Fatalf("corpus edges: %d seeds %#v", len(seeds), got)
	}

	var posted []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		posted = append([]byte(nil), body...)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "seeds_accepted": 3, "findings": 0})
	}))
	t.Cleanup(srv.Close)
	acc, findings, err := uploadResearchCorpusDelta(context.Background(), srv.Client(), srv.URL, "tok", "w", "m", "c", 1, "research:fixture",
		seeds, nil, "libfuzzer", "t")
	if err != nil || acc != 3 || findings != 0 {
		t.Fatalf("upload acc=%d findings=%d err=%v", acc, findings, err)
	}
	var wrap struct {
		Seeds []struct {
			InputBytes string `json:"input_bytes"`
		} `json:"seeds"`
	}
	if err := json.Unmarshal(posted, &wrap); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, s := range wrap.Seeds {
		raw, err := base64.StdEncoding.DecodeString(s.InputBytes)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		counts[hex.EncodeToString(sum[:])]++
	}
	if len(wrap.Seeds) != 3 {
		t.Fatalf("uploaded %d seeds", len(wrap.Seeds))
	}
	for _, n := range counts {
		if n != 1 {
			t.Fatalf("same unit uploaded %d times: %#v", n, counts)
		}
	}
}

func TestCollectEngineDeltaKeepsCrashOfKnownSeed(t *testing.T) {
	dir := t.TempDir()
	known := []byte("known-seed-bytes")
	fresh := []byte("fresh-unit")
	if err := os.WriteFile(filepath.Join(dir, "old.bin"), known, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.bin"), fresh, 0o600); err != nil {
		t.Fatal(err)
	}
	crashDir := filepath.Join(dir, ".hunt-run", "crashes")
	if err := os.MkdirAll(crashDir, 0o755); err != nil {
		t.Fatal(err)
	}
	crashPath := filepath.Join(crashDir, "crash-now")
	if err := os.WriteFile(crashPath, known, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(known)
	before := map[string]string{"old.bin": hex.EncodeToString(sum[:])}
	seeds, crashes := collectEngineDelta(hunt.EngineShardResult{
		CorpusDir:          dir,
		CorpusCollectRoots: []string{dir},
		CrashFiles:         []string{crashPath},
	}, before, 32)
	if len(seeds) != 1 || string(seeds[0].InputBytes) != "fresh-unit" {
		t.Fatalf("known seed re-uploaded: %#v", seeds)
	}
	if len(crashes) != 1 || string(crashes[0]) != string(known) {
		t.Fatalf("crash of a known seed was dropped: %#v", crashes)
	}
}

func TestCollectResearchDeltaPinsFreshAndSkipsStaleCrash(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.bin")
	newPath := filepath.Join(dir, "new.bin")
	if err := os.WriteFile(oldPath, []byte("old-unit"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("new-unit"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldWhen := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	newWhen := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(oldPath, oldWhen, oldWhen); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newPath, newWhen, newWhen); err != nil {
		t.Fatal(err)
	}
	crashDir := filepath.Join(dir, ".hunt-run", "crashes")
	if err := os.MkdirAll(crashDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := []byte("stale-crash")
	freshCrash := []byte("fresh-crash")
	if err := os.WriteFile(filepath.Join(crashDir, "crash-old"), stale, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(crashDir, "crash-new"), freshCrash, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(stale)
	before := map[string]string{".hunt-crash:" + hex.EncodeToString(sum[:]): hex.EncodeToString(sum[:])}
	seeds, crashes, err := collectResearchDelta(dir, before, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(seeds) != 1 || string(seeds[0].InputBytes) != "new-unit" {
		t.Fatalf("fresh unit not pinned: %#v", seeds)
	}
	if len(crashes) != 1 || string(crashes[0]) != "fresh-crash" {
		t.Fatalf("stale crash re-uploaded: %#v", crashes)
	}
}

func TestResearchSlotDoesNotReuploadStaleCrash(t *testing.T) {
	t.Setenv("HACKME_WORKER_RESEARCH_SLOT", "1")
	cfg := ResearchSlotFromEnv()
	dir := t.TempDir()
	corpus := filepath.Join(dir, "reports", "oss-cve-libfuzzer", "jsmn", "corpus")
	crashDir := filepath.Join(corpus, ".hunt-run", "crashes")
	if err := os.MkdirAll(crashDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(crashDir, "crash-old"), []byte("stale-crash"), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := researchLFRunner
	t.Cleanup(func() { researchLFRunner = prev })
	researchLFRunner = func(ctx context.Context, repoRoot, targetID string, wallSec int) (int, error) {
		if err := os.WriteFile(filepath.Join(corpus, "unit-new"), []byte("unit-new"), 0o600); err != nil {
			return 0, err
		}
		if err := os.WriteFile(filepath.Join(crashDir, "crash-new"), []byte("fresh-crash"), 0o600); err != nil {
			return 0, err
		}
		return 1, nil
	}
	var seeds, crashes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Seeds []struct {
				InputBytes string `json:"input_bytes"`
			} `json:"seeds"`
			Crashes []string `json:"crashes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		seeds = seeds[:0]
		for _, s := range body.Seeds {
			raw, _ := base64.StdEncoding.DecodeString(s.InputBytes)
			seeds = append(seeds, string(raw))
		}
		crashes = crashes[:0]
		for _, c := range body.Crashes {
			raw, _ := base64.StdEncoding.DecodeString(c)
			crashes = append(crashes, string(raw))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "seeds_accepted": len(body.Seeds), "findings": 0})
	}))
	t.Cleanup(srv.Close)
	res := MaybeRunResearchSlot(context.Background(), ResearchSlotRun{
		Config: cfg, CoordURL: srv.URL, Token: "tok", WorkerID: "w1",
		HTTPClient: srv.Client(), RepoRoot: dir,
		Claim: ClaimResp{
			CampaignID: "camp", ItemID: 7, UpstreamTargetID: "jsmn",
			WorkKind: "hunt_shard", TaskClass: "hunt",
		},
	})
	if !res.Ran || res.Err != "" {
		t.Fatalf("res=%+v", res)
	}
	if len(seeds) != 1 || seeds[0] != "unit-new" {
		t.Fatalf("seeds=%v", seeds)
	}
	if len(crashes) != 1 || crashes[0] != "fresh-crash" {
		t.Fatalf("crashes=%v", crashes)
	}
}
