package hunt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanInventoryFindsMarker(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "fuzz_me.c")
	if err := os.WriteFile(src, []byte("int LLVMFuzzerTestOneInput(const uint8_t *d, size_t n) { return 0; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ScanInventory(dir, dir, 50, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Targets) != 1 {
		t.Fatalf("targets=%d want 1", len(res.Targets))
	}
	if res.Targets[0].Source != "inventory" {
		t.Fatalf("source=%s", res.Targets[0].Source)
	}
}

func TestScanInventoryBlocksEtc(t *testing.T) {
	_, err := ScanInventory("", "/etc", 10, 3)
	if err == nil {
		t.Fatal("expected block for /etc")
	}
}

func TestListCatalogTargets(t *testing.T) {
	root, err := repoRootForTest()
	if err != nil {
		t.Skip(err)
	}
	targets, err := ListCatalogTargets(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) == 0 {
		t.Fatal("expected catalog targets")
	}
	found := false
	for _, tg := range targets {
		if tg.ReuseReady && tg.Source == "catalog" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no reuse-ready catalog target")
	}
}

func repoRootForTest() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err2 := os.Stat(filepath.Join(dir, "upstream", "oss_cve_targets.json")); err2 == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", os.ErrNotExist
}
