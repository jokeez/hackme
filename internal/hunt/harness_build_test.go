package hunt

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPreviewTemplateNeedsAccept(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "plain.c")
	body := `int parse(const unsigned char *d, int n) { if (n > 0 && d[0] == 0xff) return *(int*)d; return 0; }`
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, err := PreviewTemplate(dir, dir, "plain.c")
	if err != nil {
		t.Fatal(err)
	}
	if prev.HasFuzzEntry {
		t.Fatal("expected no fuzz entry")
	}
	if !prev.NeedsAccept {
		t.Fatal("expected needs_accept")
	}
}

func TestBuildInventoryHarnessWithTemplate(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang required")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "fuzz_target.c")
	body := `int LLVMFuzzerTestOneInput(const unsigned char *d, unsigned long n) {
		if (n > 4 && d[0]=='c' && d[1]=='r' && d[2]=='a' && d[3]=='s' && d[4]=='h') {
			*(volatile int*)0 = 1;
		}
		return 0;
	}`
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	pin := &RepoPinResult{Path: dir, CommitSHA: "testsha"}
	res, err := BuildInventoryHarness(context.Background(), dir, HarnessBuildRequest{
		Pin:            pin,
		SourceRel:      "fuzz_target.c",
		TemplateAccept: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.HarnessHash == "" || res.BinaryPath == "" {
		t.Fatalf("build=%+v", res)
	}
	if _, err := os.Stat(res.BinaryPath); err != nil {
		t.Fatal(err)
	}
}

func TestInventoryHarnessHashStable(t *testing.T) {
	content := []byte("int LLVMFuzzerTestOneInput(const unsigned char *d, unsigned long n) { return 0; }")
	h1 := InventoryHarnessHash("abc", "foo.c", content)
	h2 := InventoryHarnessHash("abc", "foo.c", content)
	if h1 != h2 || h1 == "" {
		t.Fatalf("hash=%q", h1)
	}
}
