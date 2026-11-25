package hunt_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/hunt"
)

// The dict header said "derived from the harness binary" no matter where the tokens came
// from. Reading a dict file built from sources, that line sent me looking for a
// binary-scan regression that was not happening.
func TestAugmentLibFuzzerDictHeaderNamesItsSource(t *testing.T) {
	out := string(hunt.AugmentLibFuzzerDict(nil, []string{"ID3"}, "sources"))
	if !strings.Contains(out, "# derived from sources") {
		t.Fatalf("header does not name the source:\n%s", out)
	}
	if strings.Contains(out, "harness binary") {
		t.Fatalf("header still claims the binary:\n%s", out)
	}
	// Unspecified provenance must not claim one.
	plain := string(hunt.AugmentLibFuzzerDict(nil, []string{"ID3"}))
	if strings.Contains(plain, "derived from") {
		t.Fatalf("unspecified provenance invented one:\n%s", plain)
	}
}

// A demo tree carries another program's whole vocabulary. minimp3 ships player/glad.{c,h},
// a vendored OpenGL loader, and its gl* symbol names became the dictionary an MP3 decoder
// was fuzzed with: 192 tokens, every one a mutation spent on nothing. Measured after this
// skip, the same clone yields 9 tokens and they are the format's own magic — ID3, TAG,
// TAG+, APETAGEX.
func TestDeriveDictTokensFromSourcesSkipsDemoTrees(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The library's own vocabulary.
	write("decode.c", `static const char *magic = "APETAGEX"; const char *t = "ID3";`)
	// A vendored GL loader under a demo tree, exactly minimp3's layout.
	for _, dir := range []string{"player", "demo", "samples", "glad", "contrib"} {
		write(dir+"/glad.c", `const char *a = "glBindBufferBase"; const char *b = "glProgramUniformMatrix4fvEXT";`)
	}
	toks := hunt.DeriveDictTokensFromSources(root, 0)
	joined := strings.Join(toks, " ")
	if !strings.Contains(joined, "APETAGEX") || !strings.Contains(joined, "ID3") {
		t.Fatalf("the library's own tokens were lost: %v", toks)
	}
	if strings.Contains(joined, "glBindBufferBase") || strings.Contains(joined, "glProgramUniform") {
		t.Fatalf("a demo tree's vocabulary leaked into the dictionary: %v", toks)
	}
}
