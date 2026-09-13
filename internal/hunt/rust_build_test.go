package hunt

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExtractFuzzTargetBody(t *testing.T) {
	src := `#![no_main]
use libfuzzer_sys::fuzz_target;
use mycrate::parse;
fuzz_target!(|input: &[u8]| {
    if input.len() > 3 && input[0] == b'x' {
        panic!("boom");
    }
});
`
	param, body, ok := extractFuzzTargetBody(src)
	if !ok || body == "" || param != "input" {
		t.Fatalf("param=%q body=%q ok=%v", param, body, ok)
	}
	if !containsSub(body, "input.len()") {
		t.Fatalf("body=%q", body)
	}
	uses := extractRustUseImports(src)
	if !containsSub(uses, "use mycrate::parse;") || containsSub(uses, "libfuzzer_sys") {
		t.Fatalf("uses=%q", uses)
	}
}

func TestPlanRustHarnessModes(t *testing.T) {
	dir := t.TempDir()
	body := `fuzz_target!(|data: &[u8]| { let _ = data; });`
	plan, err := planRustHarness(dir, "fuzz_parse.rs", []byte(body))
	if err != nil || plan.Mode != "stdin_fuzz_target" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	fuzzDir := filepath.Join(dir, "fuzz")
	if err := os.MkdirAll(filepath.Join(fuzzDir, "fuzz_targets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fuzzDir, "Cargo.toml"), []byte("[package]\nname=\"f\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Sibling fuzz/Cargo.toml must not force cargo_fuzz for root fuzz_parse.rs.
	planRoot, err := planRustHarness(dir, "fuzz_parse.rs", []byte(body))
	if err != nil || planRoot.Mode != "stdin_fuzz_target" {
		t.Fatalf("planRoot=%+v err=%v", planRoot, err)
	}
	plan2, err := planRustHarness(dir, "fuzz/fuzz_targets/example.rs", []byte(body))
	if err != nil || plan2.Mode != "cargo_fuzz" {
		t.Fatalf("plan2=%+v err=%v", plan2, err)
	}
}

func TestBuildInventoryRustHarnessFuzzTarget(t *testing.T) {
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo required")
	}
	if err := requireRustNightlyASAN(); err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "fuzz_parse.rs")
	body := `#![no_main]
use libfuzzer_sys::fuzz_target;
fuzz_target!(|data: &[u8]| {
	if data.len() > 4 && data[0] == b'c' && data[1] == b'r' && data[2] == b'a' && data[3] == b's' {
		let v = vec![0u8; data.len() * 1024];
		let _ = v[data.len() / 2];
	}
});
`
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	pin := &RepoPinResult{Path: dir, CommitSHA: "rusttest"}
	res, err := BuildInventoryHarness(context.Background(), dir, HarnessBuildRequest{
		Pin: pin, SourceRel: "fuzz_parse.rs",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Language != "rust" || res.BinaryPath == "" {
		t.Fatalf("res=%+v", res)
	}
	if _, err := os.Stat(res.BinaryPath); err != nil {
		t.Fatal(err)
	}
}

func containsSub(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOfSub(s, sub) >= 0)
}

func indexOfSub(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
