package hunt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Issue #13 D — customer-repo edge cases must fail closed (no stub that never hits target code).

func TestRustBuildRefusePackageStubWithoutFuzzTarget(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Cargo.toml", `[package]
name = "weird_crate"
version = "0.1.0"
edition = "2021"
`)
	write("src/lib.rs", `pub fn parse(b: &[u8]) -> usize { b.len() }
`)
	// Source mentions neither fuzz_target! nor cargo-fuzz layout — must refuse package stub.
	write("src/not_a_harness.rs", `pub fn touch() {}
`)
	pin := &RepoPinResult{Path: dir, CommitSHA: "deadbeef"}
	_, err := BuildInventoryRustHarness(context.Background(), RepoRoot(), HarnessBuildRequest{
		Pin:       pin,
		SourceRel: "src/not_a_harness.rs",
	})
	if err == nil {
		t.Fatal("expected fail-closed refuse for package stub without fuzz_target!")
	}
	if !strings.Contains(err.Error(), "refuse package driver stub") && !strings.Contains(err.Error(), "no fuzz_target") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInventoryBuildRefuseMissingLLVMFuzzerWithoutTemplate(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "parser.c")
	body := `int parse(const unsigned char *d, unsigned long n) { return (int)n; }
`
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	pin := &RepoPinResult{Path: dir, CommitSHA: "cafebabe"}
	_, err := BuildInventoryHarness(context.Background(), RepoRoot(), HarnessBuildRequest{
		Pin:            pin,
		SourceRel:      "parser.c",
		TemplateAccept: false,
	})
	if err == nil {
		t.Fatal("expected fail-closed when LLVMFuzzerTestOneInput missing and template_accept=false")
	}
	if !strings.Contains(err.Error(), "LLVMFuzzerTestOneInput missing") {
		t.Fatalf("unexpected error: %v", err)
	}
}
