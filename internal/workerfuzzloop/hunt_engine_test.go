package workerfuzzloop

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/hunt"
)

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
