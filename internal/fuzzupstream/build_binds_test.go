package fuzzupstream

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCatalogBuildBindsOmitsRepoRoot(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "hackme-repo")
	clone := filepath.Join(repo, ".cache", "oss-cve-clones", "cjson")
	driver := filepath.Join(repo, "tasks", "sources", "fuzz", "oss", "cjson_fuzzer.c")
	buildTmp := filepath.Join(tmp, "build")
	for _, d := range []string{clone, filepath.Dir(driver), buildTmp, filepath.Join(repo, "bin")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	tg := Target{ID: "cjson", IncludeDirs: []string{"."}}
	got := catalogBuildBinds(repo, buildTmp, clone, driver, tg, filepath.Join(repo, "bin"))
	repoClean := filepath.Clean(repo)
	for _, p := range got {
		if filepath.Clean(p) == repoClean || filepath.Clean(p) == "/opt/hackme" {
			t.Fatalf("repoRoot/prefix must not be bound: %#v", got)
		}
	}
	want := []string{buildTmp, clone, filepath.Dir(driver)}
	for _, w := range want {
		found := false
		for _, p := range got {
			if filepath.Clean(p) == filepath.Clean(w) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing needed bind %s in %#v", w, got)
		}
	}
}
