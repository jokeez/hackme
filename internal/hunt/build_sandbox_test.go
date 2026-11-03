package hunt

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildSandboxEnabledDefaultOn(t *testing.T) {
	t.Setenv("HACKME_HUNT_BUILD_SANDBOX", "")
	if !BuildSandboxEnabled() {
		t.Fatal("empty env: build sandbox should default ON")
	}
	t.Setenv("HACKME_HUNT_BUILD_SANDBOX", "0")
	if BuildSandboxEnabled() {
		t.Fatal("HACKME_HUNT_BUILD_SANDBOX=0 should disable cage")
	}
}

func TestUniqueBindPathsDedupes(t *testing.T) {
	got := uniqueBindPaths("/tmp/a", "/tmp/a", "", "/tmp/b")
	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
}

func TestRunBareBuildSeesEphemeralWrapper(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not in PATH")
	}
	t.Setenv("HACKME_HUNT_BUILD_SANDBOX", "0")
	t.Setenv("HACKME_HUNT_ISOLATE_STRICT", "0")
	t.Setenv("HACKME_HUNT_ALLOW_ROOT", "1")
	if BuildSandboxEnabled() {
		t.Fatal("sandbox should be off")
	}
	tmp := t.TempDir()
	wrapper := filepath.Join(tmp, "hunt_wrapper.c")
	if err := os.WriteFile(wrapper, []byte("int main(void){return 0;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(tmp, "t.o")
	stderr, err := runCompilerBuild(context.Background(), "clang", []string{"-c", "-o", out, wrapper}, []string{tmp}, 30*time.Second)
	if err != nil {
		t.Fatalf("bare clang failed: %v (%s)", err, stderr)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
}
