package hunt

import (
	"path/filepath"
	"testing"
)

func TestSafeJoinUnderRejectsEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := SafeJoinUnder(root, "..", "etc", "passwd"); err == nil {
		t.Fatal("expected escape reject")
	}
	got, err := SafeJoinUnder(root, "a", "b.c")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "a", "b.c")
	if got != want {
		// Abs may differ by symlink; compare Rel
		rel, err := filepath.Rel(root, got)
		if err != nil || rel != filepath.Join("a", "b.c") {
			t.Fatalf("got %q want under %q", got, want)
		}
	}
}

func TestValidateGitURLAndRef(t *testing.T) {
	t.Setenv("HACKME_ALLOW_PRIVATE_EGRESS", "0")
	if _, err := ValidateGitURL("https://github.com/foo/bar.git"); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateGitURL("https://evil;rm -rf /"); err == nil {
		t.Fatal("expected reject")
	}
	if _, err := ValidateGitURL("https://127.0.0.1/repo.git"); err == nil {
		t.Fatal("expected reject loopback SSRF")
	}
	if _, err := ValidateGitURL("https://user:pass@github.com/foo/bar.git"); err == nil {
		t.Fatal("expected reject userinfo")
	}
	if _, err := ValidateGitRef("main"); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateGitRef("-rf"); err == nil {
		t.Fatal("expected reject leading dash")
	}
	if err := ValidateHexHash("d5a1703fcfaf5f296993b4b6373e9cb9"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateHexHash("../x"); err == nil {
		t.Fatal("expected reject")
	}
}
