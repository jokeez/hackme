package workerfuzzloop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/hunt"
)

func TestInventoryZeroSeedsSoftSkips(t *testing.T) {
	out := RunHuntEngineShard(context.Background(), ResearchSlotRun{}, ClaimResp{
		HuntEngine:       hunt.EngineLibFuzzer,
		HuntSource:       "inventory",
		EngineLeaseSec:   180,
		UpstreamTargetID: "inv_empty",
	})
	if !out.SoftSkip || out.SkipReason != "inventory_seeds_missing" {
		t.Fatalf("zero-seed inventory must soft-skip, got %+v", out)
	}
}

func TestDetectHuntEnginesIncludesByte(t *testing.T) {
	got := DetectHuntEngines()
	found := false
	for _, e := range got {
		if e == hunt.EngineByte {
			found = true
		}
	}
	wantByte, _ := hunt.RunnerAvailable(hunt.EngineByte)
	if wantByte && !found {
		t.Fatalf("byte missing with working bwrap: %v", got)
	}
	if !wantByte && found {
		t.Fatalf("byte advertised without linux+bwrap: %v", got)
	}
	csv := HuntEnginesClaimCSV()
	if wantByte && !strings.Contains(csv, hunt.EngineByte) {
		t.Fatalf("csv %q", csv)
	}
}

func TestClaimRespEngineFieldsJSON(t *testing.T) {
	cr := ClaimResp{HuntEngine: hunt.EngineLibFuzzer, EngineLeaseSec: 60, EngineProfile: "fleet"}
	if !hunt.IsCoverageEngine(cr.HuntEngine) {
		t.Fatal("coverage")
	}
	if cr.EngineLeaseSec != 60 {
		t.Fatal("lease")
	}
}

func TestCorpusSeedBytes(t *testing.T) {
	b, ok := corpusSeedBytes(map[string]any{"input_bytes_hex": "7b7d"})
	if !ok || string(b) != "{}" {
		t.Fatalf("got %q ok=%v", b, ok)
	}
	if _, ok := corpusSeedBytes(nil); ok {
		t.Fatal("nil")
	}
}

func TestCollectEngineDeltaSkipsBootStarters(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "boot.bin"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real.bin"), []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	boots := []struct {
		name string
		body []byte
	}{
		{"obj.json", []byte("{}")},
		{"arr.json", []byte("[]")},
		{"null.json", []byte("null")},
		{"z.bin", []byte{0x00}},
		{"hi.bin", []byte{0x80}},
	}
	for _, b := range boots {
		if err := os.WriteFile(filepath.Join(dir, b.name), b.body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	seeds, _ := collectEngineDelta(hunt.EngineShardResult{CorpusDir: dir}, nil, 32)
	if len(seeds) != 1 || string(seeds[0].InputBytes) != `{"a":1}` {
		t.Fatalf("boot starters must not take a delta slot: %#v", seeds)
	}
}

func TestCollectEngineDeltaDedupsFullSHA256(t *testing.T) {
	dir := t.TempDir()
	prefix := []byte("0123456789abcdef0123456789abcdef")
	same := append(append([]byte{}, prefix...), []byte("-same")...)
	other := append(append([]byte{}, prefix...), []byte("-other")...)
	if err := os.WriteFile(filepath.Join(dir, "a.bin"), same, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.bin"), append([]byte(nil), same...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.bin"), other, 0o600); err != nil {
		t.Fatal(err)
	}
	seeds, _ := collectEngineDelta(hunt.EngineShardResult{CorpusDir: dir}, nil, 32)
	if len(seeds) != 2 {
		t.Fatalf("full-hash dedup got %d seeds: %#v", len(seeds), seeds)
	}
	got := map[string]int{}
	for _, s := range seeds {
		got[string(s.InputBytes)]++
	}
	if got[string(same)] != 1 || got[string(other)] != 1 {
		t.Fatalf("shared 32-byte prefix collapsed a distinct unit: %#v", got)
	}
}

func TestCollectEngineDeltaSkipsPreLeaseFiles(t *testing.T) {
	dir := t.TempDir()
	old := []byte("old-seed")
	neu := []byte("new-seed")
	if err := os.WriteFile(filepath.Join(dir, "a.bin"), old, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.bin"), neu, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(old)
	before := map[string]string{"a.bin": hex.EncodeToString(sum[:])}
	seeds, _ := collectEngineDelta(hunt.EngineShardResult{CorpusDir: dir}, before, 32)
	if len(seeds) != 1 || string(seeds[0].InputBytes) != "new-seed" {
		t.Fatalf("want only new-seed, got %#v", seeds)
	}
}

func TestCollectEngineDeltaDropsStaleCrashContent(t *testing.T) {
	dir := t.TempDir()
	crashDir := filepath.Join(dir, ".hunt-run", "crashes")
	if err := os.MkdirAll(crashDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := []byte("stale-crash-bytes")
	fresh := []byte("fresh-crash-bytes")
	stalePath := filepath.Join(crashDir, "crash-old")
	freshPath := filepath.Join(crashDir, "crash-new")
	if err := os.WriteFile(stalePath, stale, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(freshPath, fresh, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(stale)
	before := map[string]string{".hunt-crash:" + hex.EncodeToString(sum[:]): hex.EncodeToString(sum[:])}
	_, crashes := collectEngineDelta(hunt.EngineShardResult{
		CorpusDir:          dir,
		CorpusCollectRoots: []string{dir},
		CrashFiles:         []string{stalePath, freshPath},
	}, before, 32)
	if len(crashes) != 1 || string(crashes[0]) != "fresh-crash-bytes" {
		t.Fatalf("want only fresh crash, got %#v", crashes)
	}
}

func TestCollectEngineDeltaRefusesIntermediateDirSymlink(t *testing.T) {
	dir := t.TempDir()
	host := t.TempDir()
	if err := os.WriteFile(filepath.Join(host, "exfil.bin"), []byte("host-leak"), 0o600); err != nil {
		t.Fatal(err)
	}
	via := filepath.Join(dir, "nested")
	if err := os.MkdirAll(via, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(host, filepath.Join(via, "queue")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "good.bin"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	seeds, _ := collectEngineDelta(hunt.EngineShardResult{CorpusDir: dir}, nil, 32)
	if len(seeds) != 1 || string(seeds[0].InputBytes) != "ok" {
		t.Fatalf("dir symlink must not enumerate host; got %#v", seeds)
	}
}

func TestFetchCatalogLeaseSeedsInstallsWindowOverOneMiB(t *testing.T) {
	const n = 12
	const seedLen = 65536
	rawSeeds := make([][]byte, n)
	wire := make([]string, n)
	for i := 0; i < n; i++ {
		b := bytes.Repeat([]byte{byte(i + 1)}, seedLen)
		rawSeeds[i] = b
		wire[i] = base64.StdEncoding.EncodeToString(b)
	}
	payload, err := json.Marshal(map[string]any{
		"ok": true, "unchanged": false, "generation": "g-big", "seeds": wire,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) <= 1<<20 {
		t.Fatalf("fixture is %d bytes, want a body over 1MiB", len(payload))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Hackme-Admin-Token") != "tok" {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()
	gen, got, err := fetchCatalogLeaseSeeds(context.Background(), ResearchSlotRun{
		CoordURL: srv.URL, Token: "tok", WorkerID: "w1",
	}, ClaimResp{CampaignID: "c", ItemID: 7}, "")
	if err != nil {
		t.Fatal(err)
	}
	if gen != "g-big" || len(got) != n {
		t.Fatalf("gen=%q seeds=%d err=%v", gen, len(got), err)
	}
	for i := range rawSeeds {
		if string(got[i]) != string(rawSeeds[i]) {
			t.Fatalf("seed %d mismatch", i)
		}
	}
	dir := t.TempDir()
	installed, err := hunt.InstallEngineSeedFiles(dir, got)
	if err != nil || installed != n {
		t.Fatalf("installed=%d err=%v", installed, err)
	}
	ents, err := os.ReadDir(dir)
	if err != nil || len(ents) != n {
		t.Fatalf("corpus files=%d err=%v", len(ents), err)
	}

	huge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := bytes.Repeat([]byte("x"), 64<<10)
		for written := 0; written <= catalogSeedResponseMaxBytes; written += len(buf) {
			if _, err := w.Write(buf); err != nil {
				return
			}
		}
	}))
	defer huge.Close()
	gen, got, err = fetchCatalogLeaseSeeds(context.Background(), ResearchSlotRun{
		CoordURL: huge.URL, Token: "tok", WorkerID: "w1",
	}, ClaimResp{CampaignID: "c", ItemID: 7}, "g-big")
	if err == nil || gen != "" || len(got) != 0 {
		t.Fatalf("over-cap gen=%q seeds=%d err=%v", gen, len(got), err)
	}
}

func TestCollectEngineDeltaRefusesSymlinkHostSecret(t *testing.T) {
	dir := t.TempDir()
	secretDir := t.TempDir()
	secretPath := filepath.Join(secretDir, ".env.vps")
	if err := os.WriteFile(secretPath, []byte("HACKME_ADMIN_TOKEN=leak-me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secretPath, filepath.Join(dir, "leak.bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ok.bin"), []byte("ok-seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	seeds, _ := collectEngineDelta(hunt.EngineShardResult{CorpusDir: dir}, nil, 32)
	if len(seeds) != 1 || string(seeds[0].InputBytes) != "ok-seed" {
		t.Fatalf("symlink must be skipped; got %#v", seeds)
	}
}
