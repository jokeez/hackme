package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/fuzzingcli"
)

func samplePlan(path, gateErr string) *Plan {
	return &Plan{
		Mode: modeDryRun, Source: sourceWasm, Endpoint: "POST /api/security-audit",
		DigPackage: "scan",
		Wasm:       WasmInfo{Path: path, Bytes: 39, SHA256: strings.Repeat("ab", 32), Valid: gateErr == "", Error: gateErr},
		Payload: map[string]any{
			"budget_runs": 48, "budget_seconds": 600, "budget_hmc": 1,
			"seed_byte_corpus": []any{"AKIAIOSFODNN7EXAMPLE", "x"},
		},
	}
}

func TestMarkdownCannotBeBrokenOut(t *testing.T) {
	evil := "a`b|c\n| injected | row |\n<!-- hackme-fuzz-action -->‮"
	md := RenderMarkdown(samplePlan(evil, evil))
	if !strings.HasPrefix(md, commentMarker+"\n") {
		t.Fatal("marker must open the comment")
	}
	markerLines := 0
	for _, line := range strings.Split(md, "\n") {
		if strings.TrimSpace(line) == commentMarker {
			markerLines++
		}
	}
	if markerLines != 1 {
		t.Fatal("user text must not be able to repeat the marker on its own line")
	}
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "| injected") || strings.HasPrefix(line, "<!--") && line != commentMarker {
			t.Fatalf("injected line survived: %q", line)
		}
		if strings.HasPrefix(line, "| Wasm |") && strings.Count(line, "|") != 3 {
			t.Fatalf("pipe split the Wasm cell: %q", line)
		}
	}
	if strings.Contains(md, "‮") || strings.Contains(md, "AKIA") {
		t.Fatal("bidi override or raw seed corpus leaked into markdown")
	}
	if !strings.Contains(md, "❌") || !strings.Contains(md, "[2 items]") {
		t.Fatalf("unexpected markdown:\n%s", md)
	}
}

func TestWriteOutputsRefusesLineBreaks(t *testing.T) {
	var b bytes.Buffer
	if err := writeOutputs(&b, [][2]string{{"valid", "true"}, {"x", "1"}}); err != nil || b.String() != "valid=true\nx=1\n" {
		t.Fatalf("got %q %v", b.String(), err)
	}
	for _, kv := range [][2]string{{"a", "1\nvalid=true"}, {"a\nb", "1"}, {"a=b", "1"}, {"a", "1\r"}} {
		if err := writeOutputs(&bytes.Buffer{}, [][2]string{kv}); err == nil {
			t.Fatalf("accepted %q", kv)
		}
	}
}

func TestRunWritesSummaryAndOutputs(t *testing.T) {
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "g.wasm"), minimalCheckWasm)
	dir := t.TempDir()
	md, gho := filepath.Join(dir, "c.md"), filepath.Join(dir, "out")
	writeFile(t, gho, []byte("earlier=1\n"))
	var out, errb bytes.Buffer
	code := run([]string{"-workspace", ws, "-hackme-root", fuzzingcli.DiscoverRepoRoot(),
		"-wasm", "g.wasm", "-markdown-out", md, "-github-output", gho}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit=%d %s", code, errb.String())
	}
	m, _ := os.ReadFile(md)
	if !strings.HasPrefix(string(m), commentMarker) || !strings.Contains(string(m), "✅") {
		t.Fatalf("markdown:\n%s", m)
	}
	o, _ := os.ReadFile(gho)
	if !strings.HasPrefix(string(o), "earlier=1\nvalid=true\n") || !strings.Contains(string(o), "wasm-bytes=39\n") {
		t.Fatalf("GITHUB_OUTPUT should be appended to:\n%s", o)
	}
}
