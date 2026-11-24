package hunt

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAttachPublishedInventoryHarnessMatchesSourceHash(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "fuzz_target.c")
	body := []byte("int LLVMFuzzerTestOneInput(const uint8_t *d, size_t n) { (void)d; (void)n; return 0; }\n")
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	want := InventoryHarnessHash("deadbeef", "fuzz_target.c", body)
	pin := &RepoPinResult{Path: dir, CommitSHA: "deadbeef"}
	got, pinSHA, err := AttachPublishedInventoryHarness(dir, pin, "", "fuzz_target.c", want)
	if err != nil {
		t.Fatal(err)
	}
	if got != want || pinSHA != "deadbeef" {
		t.Fatalf("got hash=%s pin=%s want %s/deadbeef", got, pinSHA, want)
	}
}

func TestAttachPublishedInventoryHarnessMismatch(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "fuzz_target.c")
	if err := os.WriteFile(src, []byte("int LLVMFuzzerTestOneInput(const uint8_t *d, size_t n) { return 0; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pin := &RepoPinResult{Path: dir, CommitSHA: "abc"}
	_, _, err := AttachPublishedInventoryHarness(dir, pin, "", "fuzz_target.c", "0123456789abcdef0123456789abcdef")
	if err == nil {
		t.Fatal("expected mismatch error")
	}
}

func TestCampaignConfigAttachesPublishedHarnessWithoutBuild(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "fuzz_target.c")
	body := []byte("int LLVMFuzzerTestOneInput(const uint8_t *d, size_t n) { (void)d; (void)n; return 0; }\n")
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	// PinRepo reads git HEAD when present; empty SHA when not a git repo.
	pinSHA, _ := gitHead(context.Background(), dir)
	hash := InventoryHarnessHash(pinSHA, "fuzz_target.c", body)

	contentSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	prev := ProbeCoordinatorHarness
	ProbeCoordinatorHarness = func(ctx context.Context, h string) (bool, string) {
		if h == hash {
			return true, contentSHA
		}
		return false, ""
	}
	t.Cleanup(func() { ProbeCoordinatorHarness = prev })

	cfg, title, err := CampaignConfig(context.Background(), dir, CreateRequest{
		Title:                "win-attach",
		Package:              "hunt_lite",
		BudgetHMC:            20,
		PoolDistributed:      true,
		Catalog:              false,
		HarnessHash:          hash,
		HarnessContentSHA256: contentSHA,
		TemplateAccept:       true,
		Inventory:            &TargetSummary{Path: "fuzz_target.c", Title: "demo"},
		Repo:                 &RepoPinRequest{Path: dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	if title == "" {
		t.Fatal("empty title")
	}
	if cfg["harness_pool_attached"] != true {
		t.Fatalf("expected harness_pool_attached, cfg=%v", cfg)
	}
	if cfg["harness_hash"] != hash {
		t.Fatalf("harness_hash=%v want %s", cfg["harness_hash"], hash)
	}
	if cfg["harness_content_sha256"] != contentSHA {
		t.Fatalf("content sha missing: %v", cfg["harness_content_sha256"])
	}
	if cfg["hunt_edge_pay"] != "floor_only" {
		t.Fatalf("want floor_only, got %v", cfg["hunt_edge_pay"])
	}
}

// New pool orders take the package wall. A create that already names 90s keeps
// 90s even on Heavy — that is a stored campaign, not a restamp.
func TestCampaignConfigStampsPackageWallsAndKeepsExplicit90(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "fuzz_target.c")
	body := []byte("int LLVMFuzzerTestOneInput(const uint8_t *d, size_t n) { (void)d; (void)n; return 0; }\n")
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	pinSHA, _ := gitHead(context.Background(), dir)
	hash := InventoryHarnessHash(pinSHA, "fuzz_target.c", body)
	contentSHA := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	prev := ProbeCoordinatorHarness
	ProbeCoordinatorHarness = func(ctx context.Context, h string) (bool, string) {
		if h == hash {
			return true, contentSHA
		}
		return false, ""
	}
	t.Cleanup(func() { ProbeCoordinatorHarness = prev })

	for _, tc := range []struct {
		pkg  string
		want int
	}{
		{"hunt_lite", PoolEngineLeaseSecLite},
		{"hunt_standard", PoolEngineLeaseSecStandard},
		{"hunt_heavy", PoolEngineLeaseSecHeavy},
	} {
		cfg, _, err := CampaignConfig(context.Background(), dir, CreateRequest{
			Title:                tc.pkg,
			Package:              tc.pkg,
			PoolDistributed:      true,
			Catalog:              false,
			HarnessHash:          hash,
			HarnessContentSHA256: contentSHA,
			TemplateAccept:       true,
			Inventory:            &TargetSummary{Path: "fuzz_target.c", Title: "demo"},
			Repo:                 &RepoPinRequest{Path: dir},
		})
		if err != nil {
			t.Fatalf("%s: %v", tc.pkg, err)
		}
		if got := cfg["engine_lease_sec"]; got != tc.want {
			t.Fatalf("%s engine_lease_sec=%v want %d", tc.pkg, got, tc.want)
		}
		p := PackageByKey(tc.pkg)
		if p == nil || !strings.Contains(p.Summary, strconv.Itoa(tc.want)+"s") {
			t.Fatalf("%s blurb missing %ds: %+v", tc.pkg, tc.want, p)
		}
	}

	kept, _, err := CampaignConfig(context.Background(), dir, CreateRequest{
		Title:                "stored-90",
		Package:              "hunt_heavy",
		PoolDistributed:      true,
		Catalog:              false,
		HarnessHash:          hash,
		HarnessContentSHA256: contentSHA,
		TemplateAccept:       true,
		EngineLeaseSec:       90,
		Inventory:            &TargetSummary{Path: "fuzz_target.c", Title: "demo"},
		Repo:                 &RepoPinRequest{Path: dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := kept["engine_lease_sec"]; got != 90 {
		t.Fatalf("explicit 90s create restamped to %v", got)
	}
}

func TestCampaignConfigAttachRequiresCoordinator(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "fuzz_target.c")
	body := []byte("int LLVMFuzzerTestOneInput(const uint8_t *d, size_t n) { return 0; }\n")
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	hash := InventoryHarnessHash("", "fuzz_target.c", body)
	contentSHA := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	prev := ProbeCoordinatorHarness
	ProbeCoordinatorHarness = func(ctx context.Context, h string) (bool, string) { return false, "" }
	t.Cleanup(func() { ProbeCoordinatorHarness = prev })
	_, _, err := CampaignConfig(context.Background(), dir, CreateRequest{
		Title: "x", Package: "hunt_lite", BudgetHMC: 20, PoolDistributed: true,
		HarnessHash: hash, HarnessContentSHA256: contentSHA, TemplateAccept: true,
		Inventory: &TargetSummary{Path: "fuzz_target.c"},
		Repo:      &RepoPinRequest{Path: dir},
	})
	if err == nil {
		t.Fatal("expected coordinator missing error")
	}
}

func TestCampaignConfigAttachRejectsContentSHAMismatch(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "fuzz_target.c")
	body := []byte("int LLVMFuzzerTestOneInput(const uint8_t *d, size_t n) { (void)d; (void)n; return 0; }\n")
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	pinSHA, _ := gitHead(context.Background(), dir)
	hash := InventoryHarnessHash(pinSHA, "fuzz_target.c", body)
	prev := ProbeCoordinatorHarness
	ProbeCoordinatorHarness = func(ctx context.Context, h string) (bool, string) {
		return true, "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	}
	t.Cleanup(func() { ProbeCoordinatorHarness = prev })
	_, _, err := CampaignConfig(context.Background(), dir, CreateRequest{
		Title: "squat", Package: "hunt_lite", BudgetHMC: 20, PoolDistributed: true,
		HarnessHash:          hash,
		HarnessContentSHA256: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		TemplateAccept:       true,
		Inventory:            &TargetSummary{Path: "fuzz_target.c"},
		Repo:                 &RepoPinRequest{Path: dir},
	})
	if err == nil {
		t.Fatal("expected content sha mismatch")
	}
}
