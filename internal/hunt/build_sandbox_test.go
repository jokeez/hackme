package hunt

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestBuildToolchainRoBindsNeverMountsPrefixRoot(t *testing.T) {
	tmp := t.TempDir()
	prefix := filepath.Join(tmp, "hackme-prefix")
	for _, sub := range []string{".cargo", "bin", "toolchains"} {
		if err := os.MkdirAll(filepath.Join(prefix, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Operator secret next to toolchains — must stay outside the cage.
	if err := os.WriteFile(filepath.Join(prefix, ".env.vps"), []byte("HACKME_ADMIN_TOKEN=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_PREFIX", prefix)
	t.Setenv("HOME", filepath.Join(tmp, "home"))

	got := buildToolchainRoBinds()
	for _, p := range got {
		clean := filepath.Clean(p)
		if clean == filepath.Clean(prefix) || clean == "/opt/hackme" {
			t.Fatalf("prefix root must not be RO-bound: %#v", got)
		}
		if strings.HasSuffix(clean, ".env.vps") {
			t.Fatalf("env file must not be RO-bound: %#v", got)
		}
	}
	wantSub := filepath.Join(prefix, ".cargo")
	found := false
	for _, p := range got {
		if filepath.Clean(p) == wantSub {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected toolchain subdir %s in %#v", wantSub, got)
	}
}

func TestFilterSecretPrefixRootsDropsWholePrefix(t *testing.T) {
	t.Setenv("HACKME_PREFIX", "/opt/hackme")
	got := filterSecretPrefixRoots([]string{
		"/opt/hackme",
		"/opt/hackme/.cargo",
		"/usr",
		"/etc",
		"/home",
	})
	for _, p := range got {
		switch filepath.Clean(p) {
		case "/opt/hackme", "/etc", "/home":
			t.Fatalf("unexpected bind kept: %s in %#v", p, got)
		}
	}
	ok := map[string]bool{}
	for _, p := range got {
		ok[filepath.Clean(p)] = true
	}
	if !ok["/opt/hackme/.cargo"] || !ok["/usr"] {
		t.Fatalf("expected subdir + /usr kept: %#v", got)
	}
}

func TestFilterSecretPrefixRootsAppliesToExtraBindAndDir(t *testing.T) {
	t.Setenv("HACKME_PREFIX", "/opt/hackme")
	// Simulate catalog/inventory callers that still pass repoRoot / pin = prefix.
	extraBind := filterSecretPrefixRoots(uniqueBindPaths(
		"/opt/hackme",
		"/tmp/hunt-build",
		"/opt/hackme/.cache/oss-cve-clones/cjson",
	))
	for _, p := range extraBind {
		if filepath.Clean(p) == "/opt/hackme" {
			t.Fatalf("prefix root must not appear in ExtraBind: %#v", extraBind)
		}
	}
	if sanitizeCageDir("/opt/hackme") != "" {
		t.Fatal("Dir=/opt/hackme must be stripped")
	}
	if sanitizeCageDir("/home") != "" || sanitizeCageDir("/root") != "" {
		t.Fatal("/home and /root must not be cage dirs")
	}
	if got := sanitizeCageDir("/tmp/hunt-work"); got == "" {
		t.Fatal("safe work dir must remain")
	}
}

func TestEphemeralHomeEnvExclusiveAfterStrip(t *testing.T) {
	tmp := t.TempDir()
	env := []string{
		"PATH=/usr/bin",
		"HOME=/home/operator",
		"CARGO_HOME=/home/operator/.cargo",
		"XDG_CACHE_HOME=/home/operator/.cache",
		"HACKME_ADMIN_TOKEN=secret",
	}
	env = scrubIsolateEnv(env)
	env = stripEnvKeys(env, "CARGO_HOME", "HOME", "XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME")
	_, _, _, homeEnv := buildEphemeralHomeBinds(tmp)
	if len(homeEnv) == 0 {
		t.Fatal("expected ephemeral home env")
	}
	env = append(env, homeEnv...)
	vals := map[string]string{}
	for _, e := range env {
		k, v, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		if _, exists := vals[k]; exists {
			t.Fatalf("duplicate env key %s (first-wins leak risk): %#v", k, env)
		}
		vals[k] = v
	}
	if vals["HOME"] == "/home/operator" || vals["CARGO_HOME"] == "/home/operator/.cargo" {
		t.Fatalf("operator HOME/CARGO_HOME survived: %#v", vals)
	}
	if !strings.Contains(vals["CARGO_HOME"], ".hackme-build-home") {
		t.Fatalf("expected ephemeral CARGO_HOME, got %q", vals["CARGO_HOME"])
	}
	if _, ok := vals["HACKME_ADMIN_TOKEN"]; ok {
		t.Fatal("admin token must stay scrubbed")
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
