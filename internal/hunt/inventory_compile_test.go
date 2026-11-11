package hunt

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSourceLanguage(t *testing.T) {
	if SourceLanguage("foo.c") != "c" {
		t.Fatal("c")
	}
	if SourceLanguage("bar.cpp") != "cpp" {
		t.Fatal("cpp")
	}
	if SourceLanguage("baz.rs") != "rust" {
		t.Fatal("rust")
	}
}

func TestCollectCompanionSourcesSkipsMain(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fuzz.c"), []byte("int LLVMFuzzerTestOneInput(const unsigned char*,unsigned long){return 0;}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "helper.c"), []byte("int helper(int x){return x+1;}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test.c"), []byte("int main(void){return 0;}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := collectCompanionSources(dir, "fuzz.c")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "helper.c" {
		t.Fatalf("companions=%v", got)
	}
}

// Medium: pin-planted symlink must not resolve as inventory/template source (clang follows).
func TestResolveSourceFileRefusesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret.env")
	if err := os.WriteFile(secret, []byte("TOKEN=leak\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pin := filepath.Join(dir, "pin")
	if err := os.MkdirAll(pin, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(pin, "fuzz.c")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveSourceFile("", pin, "fuzz.c"); err == nil {
		t.Fatal("expected refuse symlink source")
	}
	regular := filepath.Join(pin, "ok.c")
	if err := os.WriteFile(regular, []byte("int x;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveSourceFile("", pin, "ok.c")
	if err != nil || got != regular {
		t.Fatalf("regular source: got=%q err=%v", got, err)
	}
}

// Low: companion enumeration must skip symlink entries (Lstat+IsRegular).
func TestCollectCompanionSourcesSkipsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fuzz.c"), []byte("int LLVMFuzzerTestOneInput(const unsigned char*,unsigned long){return 0;}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "helper.c"), []byte("int helper(int x){return x+1;}"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.c")
	if err := os.WriteFile(outside, []byte("int outside(void){return 1;}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "evil.c")); err != nil {
		t.Fatal(err)
	}
	got, err := collectCompanionSources(dir, "fuzz.c")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "helper.c" {
		t.Fatalf("companions=%v want only helper.c (symlink skipped)", got)
	}
}

func TestCollectCompanionSources(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fuzz_target.cpp"), []byte("int LLVMFuzzerTestOneInput(const unsigned char*,unsigned long){return 0;}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "helper.cpp"), []byte("int helper(int x){return x+1;}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other_fuzz.c"), []byte("int LLVMFuzzerTestOneInput(const unsigned char*,unsigned long){return 0;}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := collectCompanionSources(dir, "fuzz_target.cpp")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "helper.cpp" {
		t.Fatalf("companions=%v", got)
	}
}

func TestScanInventoryCppAndCMakeHints(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CMakeLists.txt"), []byte("cmake_minimum_required(VERSION 3.10)"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fuzz.cpp"), []byte("extern \"C\" int LLVMFuzzerTestOneInput(const unsigned char*,unsigned long){return 0;}"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ScanInventory(dir, dir, 50, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Targets) != 1 || res.Targets[0].Language != "cpp" {
		t.Fatalf("targets=%+v", res.Targets)
	}
	hasCMake := false
	for _, h := range res.BuildHints {
		if h == "cmake_present" || h == "cpp_sources" {
			hasCMake = true
		}
	}
	if !hasCMake {
		t.Fatalf("hints=%v", res.BuildHints)
	}
}

func TestBuildInventoryHarnessCppMultiFile(t *testing.T) {
	t.Setenv("HACKME_HUNT_ISOLATE_STRICT", "0")
	t.Setenv("HACKME_HUNT_BUILD_SANDBOX", "0")
	if _, err := exec.LookPath("clang++"); err != nil {
		t.Skip("clang++ required")
	}
	dir := t.TempDir()
	helper := `extern "C" int parse_token(const char *s, int n) {
		int v = 0;
		for (int i = 0; i < n && s[i]; i++) { v = v * 10 + (s[i]-'0'); }
		return v;
	}`
	main := `#include <stdint.h>
#include <stddef.h>
extern "C" int parse_token(const char *s, int n);
extern "C" int LLVMFuzzerTestOneInput(const uint8_t *d, size_t n) {
	if (n > 4 && parse_token((const char*)d, (int)n) == 1234) {
		*(volatile int*)0 = 1;
	}
	return 0;
}`
	if err := os.WriteFile(filepath.Join(dir, "helper.cpp"), []byte(helper), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fuzz_target.cpp"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	pin := &RepoPinResult{Path: dir, CommitSHA: "cppmulti"}
	res, err := BuildInventoryHarness(context.Background(), dir, HarnessBuildRequest{
		Pin:       pin,
		SourceRel: "fuzz_target.cpp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Language != "cpp" {
		t.Fatalf("lang=%s", res.Language)
	}
	if len(res.CompanionSources) != 1 || res.CompanionSources[0] != "helper.cpp" {
		t.Fatalf("companions=%v", res.CompanionSources)
	}
	if _, err := os.Stat(res.BinaryPath); err != nil {
		t.Fatal(err)
	}
}

func TestCollectParentCompanionsCjson(t *testing.T) {
	root, err := repoRootForTest()
	if err != nil {
		t.Skip(err)
	}
	clone := filepath.Join(root, ".cache", "oss-cve-clones", "cjson")
	if _, err := os.Stat(filepath.Join(clone, "fuzzing", "cjson_read_fuzzer.c")); err != nil {
		t.Skip("cjson clone missing; run build_oss_cve_pack")
	}
	got, err := collectCompanionSources(clone, "fuzzing/cjson_read_fuzzer.c")
	if err != nil {
		t.Fatal(err)
	}
	hasCJSON := false
	for _, rel := range got {
		if filepath.Base(rel) == "cJSON.c" {
			hasCJSON = true
		}
	}
	if !hasCJSON {
		t.Fatalf("expected cJSON.c companion, got %v", got)
	}
}

func TestBuildInventoryHarnessCjsonCustomerRepo(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang required")
	}
	root, err := repoRootForTest()
	if err != nil {
		t.Skip(err)
	}
	clone := filepath.Join(root, ".cache", "oss-cve-clones", "cjson")
	if _, err := os.Stat(filepath.Join(clone, "fuzzing", "cjson_read_fuzzer.c")); err != nil {
		t.Skip("cjson clone missing")
	}
	pin := &RepoPinResult{Path: clone, CommitSHA: "customer-cjson-demo", GitURL: "https://github.com/DaveGamble/cJSON", Ref: "master"}
	res, err := BuildInventoryHarness(context.Background(), root, HarnessBuildRequest{
		Pin:       pin,
		SourceRel: "fuzzing/cjson_read_fuzzer.c",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Language != "c" {
		t.Fatalf("lang=%s", res.Language)
	}
	found := false
	for _, c := range res.CompanionSources {
		if filepath.Base(c) == "cJSON.c" {
			found = true
		}
	}
	if !found {
		t.Fatalf("companions=%v", res.CompanionSources)
	}
	if _, err := os.Stat(res.BinaryPath); err != nil {
		t.Fatal(err)
	}
}

func TestBuildInventoryHarnessMixedCCompanion(t *testing.T) {
	if _, err := exec.LookPath("clang++"); err != nil {
		t.Skip("clang++ required")
	}
	t.Setenv("HACKME_HUNT_ISOLATE_STRICT", "0")
	t.Setenv("HACKME_HUNT_BUILD_SANDBOX", "0")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "helper.c"), []byte("int bump(int x){return x+1;}"), 0o644); err != nil {
		t.Fatal(err)
	}
	main := `#include <stdint.h>
#include <stddef.h>
extern "C" int bump(int x);
extern "C" int LLVMFuzzerTestOneInput(const uint8_t *d, size_t n) {
	if (n > 0 && bump((int)d[0]) == 99) { *(volatile int*)0 = 1; }
	return 0;
}`
	if err := os.WriteFile(filepath.Join(dir, "fuzz.cpp"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := BuildInventoryHarness(context.Background(), dir, HarnessBuildRequest{
		Pin:       &RepoPinResult{Path: dir, CommitSHA: "mix"},
		SourceRel: "fuzz.cpp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Language != "cpp" {
		t.Fatalf("lang=%s", res.Language)
	}
}
