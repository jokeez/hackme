package poolfuzz

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/fuzzescrow"
	"hackme/internal/hunt"
)

func TestIsHuntCampaign(t *testing.T) {
	if !IsHuntCampaign(map[string]any{"work_kind": "hunt_shard"}) {
		t.Fatal("work_kind hunt_shard")
	}
	if !IsHuntCampaign(map[string]any{"campaign_type": "hunt"}) {
		t.Fatal("campaign_type hunt")
	}
	if IsHuntCampaign(map[string]any{"work_kind": "wasm_segment"}) {
		t.Fatal("wasm should not be hunt")
	}
}

func TestHuntShardInputBytesDeterministic(t *testing.T) {
	cfg := map[string]any{"upstream_target_id": "yyjson", "max_input_bytes": 128}
	a := HuntShardInputBytes("camp-1", 3, cfg)
	b := HuntShardInputBytes("camp-1", 3, cfg)
	if len(a) != 128 {
		t.Fatalf("len=%d want 128", len(a))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatal("not deterministic")
		}
	}
	c := HuntShardInputBytes("camp-1", 4, cfg)
	if string(a) == string(c) {
		t.Fatal("different input_n should differ")
	}
}

func TestPerShardHMCFromConfig5050(t *testing.T) {
	cfg := map[string]any{
		"budget_hmc":    20.0,
		"budget_shards": 8,
		"escrow_split":  fuzzescrow.EscrowSplit5050,
		"work_kind":     "hunt_shard",
	}
	got := perShardHMCFromConfig(cfg)
	want := (20.0 * 0.50) / 8.0
	if got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("per shard got %v want %v", got, want)
	}
	if perRunHMCFromConfig(cfg) != got {
		t.Fatal("perRunHMCFromConfig should delegate to per shard for hunt")
	}
}

func TestEvalHuntSubmitCrash(t *testing.T) {
	s := &Service{}
	cfg := map[string]any{"iterations_per_shard": 32}
	req := SubmitRequest{SegmentExecDone: 32, CheckResult: 1, Trap: "hunt_crash:asan"}
	_, trap, pass, finding := s.evalHuntSubmitTrusted(cfg, req, []byte{1, 2, 3})
	if !pass || !finding || trap != "hunt_crash:asan" {
		t.Fatalf("crash submit pass=%v finding=%v trap=%q", pass, finding, trap)
	}
	req2 := SubmitRequest{SegmentExecDone: 31, Trap: "hunt_crash:asan", CheckResult: 1}
	_, _, pass2, finding2 := s.evalHuntSubmitTrusted(cfg, req2, nil)
	if pass2 || finding2 {
		t.Fatal("wrong exec count should fail")
	}
}

func TestEvalHuntSubmitRejectsFakeCrash(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang required")
	}
	t.Setenv("HACKME_REPO_ROOT", hunt.RepoRoot())
	hash, err := hunt.CatalogHarnessHash(hunt.RepoRoot(), "jsmn")
	if err != nil {
		t.Skip(err)
	}
	cfg := map[string]any{
		"iterations_per_shard": 2,
		"upstream_target_id":   "jsmn",
		"harness_hash":         hash,
		"max_input_bytes":      256,
	}
	input := []byte(`{"ok":true}`)
	s := &Service{}
	req := SubmitRequest{SegmentExecDone: 2, CheckResult: 1, Trap: "hunt_crash:asan"}
	_, trap, pass, finding, _, _, err := s.evalHuntSubmitCheck(context.Background(), "", 0, cfg, req, input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pass || finding || trap != "hunt_replay_reject:fake_crash" {
		t.Fatalf("fake crash should fail pass=%v finding=%v trap=%q", pass, finding, trap)
	}
}

func TestHuntBountyEligibleHuntOnlyCriticalHigh(t *testing.T) {
	cfg := map[string]any{"work_kind": "hunt_shard"}
	if !huntBountyEligible(cfg, "critical") || !huntBountyEligible(cfg, "high") {
		t.Fatal("hunt critical/high eligible")
	}
	if huntBountyEligible(cfg, "medium") {
		t.Fatal("hunt medium not eligible")
	}
	dig := map[string]any{"work_kind": "wasm_segment"}
	if !huntBountyEligible(dig, "medium") {
		t.Fatal("dig medium eligible via bountySeverity")
	}
}

func TestHuntCrashSeverity(t *testing.T) {
	if huntCrashSeverity("heap-buffer-overflow") != "critical" {
		t.Fatal("heap OOB critical")
	}
	if huntCrashSeverity("use-after-free") != "critical" {
		t.Fatal("UAF critical")
	}
	if huntCrashSeverity("stack-buffer-overflow") != "high" {
		t.Fatal("stack OOB high")
	}
}

func TestEvalHuntSubmitInformationalUBSan(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang required")
	}
	t.Setenv("HACKME_POOL_HUNT_REPLAY", "1")
	dir := t.TempDir()
	root := hunt.RepoRoot()
	campaignID := "ubsan-camp"
	inputN := uint64(2)
	cfg := map[string]any{
		"work_kind":            "hunt_shard",
		"campaign_type":        "hunt",
		"upstream_target_id":   "ubsan-vuln",
		"iterations_per_shard": 1,
		"max_input_bytes":      64,
		"hunt_detect_leaks":    true,
	}
	anchor := hunt.ShardAnchorBytes(campaignID, inputN, cfg)
	src := filepath.Join(dir, "fuzz_target.c")
	body := `#include <stdint.h>
int LLVMFuzzerTestOneInput(const unsigned char *d, unsigned long n) {
  if (n >= 1 && d[0]==0x55) {
    int32_t x = 0x7fffffff;
    x = x + 1;
    (void)x;
  }
  return 0;
}`
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	pin := &hunt.RepoPinResult{Path: dir, CommitSHA: "testsha"}
	build, err := hunt.BuildInventoryHarness(context.Background(), root, hunt.HarnessBuildRequest{
		Pin: pin, SourceRel: "fuzz_target.c",
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg["hunt_source"] = "inventory"
	cfg["harness_hash"] = build.HarnessHash
	cfg["hunt_pin_path"] = dir
	cfg["hunt_source_rel"] = "fuzz_target.c"
	// Force anchor byte to trigger UBSan
	anchor[0] = 0x55
	s := &Service{}
	req := SubmitRequest{SegmentExecDone: 1, CheckResult: 1, Trap: "hunt_sanitizer:ubsan/signed-overflow"}
	_, trap, pass, finding, _, _, err := s.evalHuntSubmitCheck(context.Background(), campaignID, inputN, cfg, req, anchor, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !pass || !finding || !strings.HasPrefix(trap, "hunt_sanitizer:") {
		t.Fatalf("expected informational pass=%v finding=%v trap=%q", pass, finding, trap)
	}
	ft, sev, title := classifyHuntFinding(cfg, req)
	if ft != "sanitizer_informational" || sev != "info" || !strings.Contains(title, "UBSan") {
		t.Fatalf("classify ft=%s sev=%s title=%q", ft, sev, title)
	}
}

func TestApplySanitizerDefaults(t *testing.T) {
	cfg := map[string]any{}
	hunt.ApplySanitizerDefaults(cfg, "hunt_standard")
	if cfg["hunt_detect_leaks"] != true {
		t.Fatalf("cfg=%v", cfg)
	}
	if cfg["hunt_sanitizer_profile"] != "asan+ubsan+lsan" {
		t.Fatalf("profile=%v", cfg["hunt_sanitizer_profile"])
	}
}

func TestEvalHuntSubmitConfirmsIntentionalCrash(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang required")
	}
	t.Setenv("HACKME_POOL_HUNT_REPLAY", "1")
	dir := t.TempDir()
	root := hunt.RepoRoot()
	campaignID := "confirm-camp"
	inputN := uint64(7)
	cfg := map[string]any{
		"work_kind":            "hunt_shard",
		"campaign_type":        "hunt",
		"upstream_target_id":   "intentional-vuln",
		"iterations_per_shard": 2,
		"max_input_bytes":      256,
		"depth_tier":           "oss_cve",
	}
	anchor := hunt.ShardAnchorBytes(campaignID, inputN, cfg)
	src := filepath.Join(dir, "fuzz_target.c")
	body := fmt.Sprintf(`int LLVMFuzzerTestOneInput(const unsigned char *d, unsigned long n) {
		if (n >= 4 && d[0]==0x%02x && d[1]==0x%02x && d[2]==0x%02x && d[3]==0x%02x) {
			*(volatile int*)0 = 1;
		}
		return 0;
	}`, anchor[0], anchor[1], anchor[2], anchor[3])
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	pin := &hunt.RepoPinResult{Path: dir, CommitSHA: "testsha"}
	build, err := hunt.BuildInventoryHarness(context.Background(), root, hunt.HarnessBuildRequest{
		Pin: pin, SourceRel: "fuzz_target.c",
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg["hunt_source"] = "inventory"
	cfg["harness_hash"] = build.HarnessHash
	cfg["hunt_pin_path"] = dir
	cfg["hunt_source_rel"] = "fuzz_target.c"
	s := &Service{}
	req := SubmitRequest{SegmentExecDone: 2, CheckResult: 1, Trap: "hunt_crash:asan"}
	_, trap, pass, finding, crashB, _, err := s.evalHuntSubmitCheck(context.Background(), campaignID, inputN, cfg, req, anchor, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !pass || !finding || !strings.HasPrefix(trap, "hunt_crash:") {
		t.Fatalf("expected confirmed crash pass=%v finding=%v trap=%q", pass, finding, trap)
	}
	if len(crashB) == 0 || crashB[0] != anchor[0] {
		t.Fatalf("expected crash on anchor exec, crashB len=%d", len(crashB))
	}
	ft, sev, title := classifyHuntFinding(cfg, req)
	if ft != "native_crash" || title == "" {
		t.Fatalf("classify ft=%s sev=%s title=%q", ft, sev, title)
	}
	if sev != "high" && sev != "critical" {
		t.Fatalf("unexpected severity %q", sev)
	}
}
