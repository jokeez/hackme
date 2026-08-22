package fuzznative

import (
	"os"
	"os/exec"
	"testing"
)

func TestEvalReproOssStdinExpatClean(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root, err := repoRootForTest()
	if err != nil {
		t.Skip(err)
	}
	input := []byte(`<?xml version="1.0"?><root/>`)
	res, ok := evalReproOssStdin("expat", "parser_expat", input, root)
	if !ok {
		t.Fatalf("oss repro unavailable: %s", res.Note)
	}
	if res.Status != StatusRejected {
		t.Fatalf("expected rejected on clean XML, got %s note=%s", res.Status, res.Note)
	}
	if res.UpstreamTarget != "expat" {
		t.Fatalf("upstream=%q", res.UpstreamTarget)
	}
}

func TestEvalReproOssStdinMalformedPortable(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed")
	}
	root, err := repoRootForTest()
	if err != nil {
		t.Skip(err)
	}
	// Unclosed tag — portable wasm guard hits; native expat may reject without ASAN crash.
	input := []byte("<root><child")
	res, ok := evalReproOssStdin("expat", "parser_expat", input, root)
	if !ok {
		t.Skipf("oss repro unavailable: %s", res.Note)
	}
	if res.Status != StatusRejected && res.Status != StatusNativeCrash {
		t.Fatalf("unexpected status %s note=%s", res.Status, res.Note)
	}
}

func TestResolveOssTargetID(t *testing.T) {
	if got := resolveOssTargetID("expat", ""); got != "expat" {
		t.Fatalf("got %q", got)
	}
	if got := resolveOssTargetID("", "parser_expat"); got != "expat" {
		t.Fatalf("got %q", got)
	}
	if got := ResolveTarget("oss", "parser_expat"); got != "expat" {
		t.Fatalf("ResolveTarget oss+parser got %q", got)
	}
}

func TestParseReproModeOssUpstream(t *testing.T) {
	mode := ParseReproMode(map[string]any{"native_repro_mode": "oss_upstream"})
	if mode != ReproModeOssStdin {
		t.Fatalf("mode=%q", mode)
	}
	mode = ParseReproMode(map[string]any{"depth_tier": "oss_cve"})
	if mode != ReproModeOssStdin {
		t.Fatalf("depth oss_cve mode=%q", mode)
	}
}

func TestParserExpatWasmPortableGuard(t *testing.T) {
	root, err := repoRootForTest()
	if err != nil {
		t.Skip(err)
	}
	wasm := root + "/tasks/artifacts/security/rust_parser_expat_bytes_guard.wasm"
	raw, err := os.ReadFile(wasm)
	if err != nil {
		t.Skip("parser expat wasm not built:", err)
	}
	// validated in sandbox tests if needed — smoke read only here
	if len(raw) < 100 {
		t.Fatal("wasm too small")
	}
}
