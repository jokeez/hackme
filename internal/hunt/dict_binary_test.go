package hunt_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/hunt"
)

func TestDeriveDictTokensFromBinaryLiftsTargetVocabulary(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "vocab.c")
	// A parser's vocabulary is exactly what ends up in .rodata: keywords it compares
	// against and the separators it emits. These are the tokens a dict should carry.
	code := `#include <stdio.h>
#include <string.h>
static const char *kw[] = {"BEGIN_RECORD", "END_RECORD", "chunk=", "<?manifest", "]]>", "k=v", "0x1f8b"};
int main(void) {
	char buf[64];
	if (!fgets(buf, sizeof(buf), stdin)) return 0;
	for (unsigned i = 0; i < sizeof(kw)/sizeof(kw[0]); i++) {
		if (strstr(buf, kw[i])) return (int)i;
	}
	return 0;
}
`
	if err := os.WriteFile(src, []byte(code), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "vocab")
	out, err := exec.Command("clang", "-g", "-O1", "-fsanitize=address", src, "-o", bin).CombinedOutput()
	if err != nil {
		t.Skipf("clang build unavailable: %v: %s", err, out)
	}

	toks := hunt.DeriveDictTokensFromBinary(bin, 0)
	if len(toks) == 0 {
		t.Fatal("no tokens derived from a binary that carries string literals")
	}
	have := map[string]bool{}
	for _, tk := range toks {
		have[tk] = true
	}
	for _, want := range []string{"BEGIN_RECORD", "END_RECORD", "chunk=", "<?manifest", "]]>", "k=v", "0x1f8b"} {
		if !have[want] {
			t.Errorf("derived dict is missing the target's own token %q (got %d tokens: %v)", want, len(toks), firstN(toks, 25))
		}
	}
	// An ASan binary carries thousands of runtime diagnostics; none of them belong here.
	for _, tk := range toks {
		low := strings.ToLower(tk)
		for _, bad := range []string{"sanitizer", "asan", "llvm", "clang", "/usr/", "__"} {
			if strings.Contains(low, bad) {
				t.Errorf("toolchain noise leaked into the dict: %q", tk)
			}
		}
		if strings.Contains(tk, " ") {
			t.Errorf("prose leaked into the dict: %q", tk)
		}
		if len(tk) < 3 || len(tk) > 40 {
			t.Errorf("token out of range: %q", tk)
		}
	}
	if len(toks) > 192 {
		t.Errorf("token cap not applied: %d", len(toks))
	}
}

func firstN(s []string, n int) []string {
	if len(s) < n {
		return s
	}
	return s[:n]
}

func TestDeriveDictTokensFromBinaryRejectsUnusableInput(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"missing":   filepath.Join(dir, "nope"),
		"directory": dir,
		"empty":     empty,
		"blank":     "   ",
	} {
		if toks := hunt.DeriveDictTokensFromBinary(path, 0); len(toks) != 0 {
			t.Errorf("%s: want no tokens, got %v", name, toks)
		}
	}
}

func TestAugmentLibFuzzerDictKeepsBaseAndDedupes(t *testing.T) {
	base := []byte("# hackme hunt profile dict\n\"null\"\n\"true\"\n")
	out := string(hunt.AugmentLibFuzzerDict(base, []string{"null", "BEGIN", "chunk=", "", "BEGIN"}))

	if !strings.Contains(out, `"null"`) || !strings.Contains(out, `"true"`) {
		t.Fatalf("base dict lines must survive:\n%s", out)
	}
	if strings.Count(out, `"null"`) != 1 {
		t.Fatalf("token already in the base must not be repeated:\n%s", out)
	}
	if strings.Count(out, `"BEGIN"`) != 1 {
		t.Fatalf("duplicate token in the addition must collapse:\n%s", out)
	}
	if !strings.Contains(out, `"chunk="`) {
		t.Fatalf("new token missing:\n%s", out)
	}
	// Every non-comment line has to be loadable libFuzzer dict grammar.
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, `"`) || !strings.HasSuffix(line, `"`) {
			if !strings.HasPrefix(line, `\x`) {
				t.Fatalf("line is not dict grammar: %q", line)
			}
		}
	}
	// Nothing to add leaves the body untouched.
	if got := string(hunt.AugmentLibFuzzerDict(base, nil)); got != string(base) {
		t.Fatalf("empty addition changed the dict:\n%s", got)
	}
	// No base still produces a loadable dict.
	fresh := string(hunt.AugmentLibFuzzerDict(nil, []string{"ABC"}))
	if !strings.Contains(fresh, `"ABC"`) || !strings.HasPrefix(fresh, "#") {
		t.Fatalf("dict built from nothing is not loadable:\n%s", fresh)
	}
}

func TestAugmentLibFuzzerDictCapsSize(t *testing.T) {
	many := make([]string, 0, 4096)
	for i := 0; i < 4096; i++ {
		many = append(many, "tok"+strings.Repeat("x", 30)+string(rune('a'+i%26))+string(rune('a'+(i/26)%26))+string(rune('a'+(i/676)%26)))
	}
	out := hunt.AugmentLibFuzzerDict([]byte("# base\n\"null\"\n"), many)
	if len(out) > 24<<10 {
		t.Fatalf("dict grew past the cap: %d bytes", len(out))
	}
	if !strings.Contains(string(out), `"null"`) {
		t.Fatal("base token dropped while capping")
	}
}

func TestDeriveDictTokensFromSourcesLiftsVocabularyNotScaffolding(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("src/parser.c", `
#include "parser.h"
static const char *ops[] = {"replace", "add", "remove", "op", "path"};
int parse(const char *s) {
	if (!strcmp(s, "true") || !strcmp(s, "false")) return 1;   // literals
	if (!strncmp(s, "<?cfg", 5)) return 2;
	printf("parsed %s with %d ops\n", s, 5);                   // format string, not grammar
	return 0;
}
`)
	// A vendored test framework must not contribute its vocabulary.
	write("tests/unity/unity.c", `const char *UnityStrFail = "FAIL"; const char *UnityStrOk = "testrunner";`)
	write("examples/demo.c", `const char *demo = "EXAMPLE_ONLY";`)

	toks := hunt.DeriveDictTokensFromSources(dir, 0)
	have := map[string]bool{}
	for _, tk := range toks {
		have[tk] = true
	}
	for _, want := range []string{"replace", "add", "remove", "op", "path", "true", "false", "<?cfg"} {
		if !have[want] {
			t.Errorf("missing target vocabulary %q (got %v)", want, toks)
		}
	}
	for _, unwanted := range []string{"FAIL", "testrunner", "EXAMPLE_ONLY", "parser.h"} {
		if have[unwanted] {
			t.Errorf("scaffolding token %q must not reach the dict", unwanted)
		}
	}
	for _, tk := range toks {
		if strings.Contains(tk, "%") || strings.Contains(tk, " ") {
			t.Errorf("format string or prose leaked: %q", tk)
		}
	}
	if len(hunt.DeriveDictTokensFromSources(filepath.Join(dir, "nope"), 0)) != 0 {
		t.Error("missing directory must yield no tokens")
	}
}

// printableRunsAt is index arithmetic over attacker-supplied bytes, so the edges
// matter: a run that ends exactly at the last byte must still be collected, and no
// input shape may read past the slice.
func TestDeriveDictTokensHandlesRunsAtTheVeryEnd(t *testing.T) {
	dir := t.TempDir()
	// "TAILTOKEN" sits flush against the end of the file with no terminator after it.
	body := append([]byte{0x00, 0x01, 0x02}, []byte("MIDDLE_ONE")...)
	body = append(body, 0x00)
	body = append(body, []byte("TAILTOKEN")...)
	path := filepath.Join(dir, "blob.bin")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	toks := hunt.DeriveDictTokensFromBinary(path, 0)
	have := map[string]bool{}
	for _, tk := range toks {
		have[tk] = true
	}
	if !have["TAILTOKEN"] {
		t.Errorf("a run ending at the last byte must be collected, got %v", toks)
	}
	if !have["MIDDLE_ONE"] {
		t.Errorf("a terminated run must be collected, got %v", toks)
	}
	// Degenerate shapes must not panic.
	for _, raw := range [][]byte{{}, {0x00}, {'A'}, {'A', 'B'}, {'A', 'B', 'C'}, {0x7f, 0x1f}} {
		p := filepath.Join(dir, "d.bin")
		if err := os.WriteFile(p, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		_ = hunt.DeriveDictTokensFromBinary(p, 0)
	}
}
