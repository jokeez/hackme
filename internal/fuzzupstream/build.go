package fuzzupstream

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"hackme/internal/gitutil"
)

var buildMu sync.Mutex

// BuildTarget clones upstream (if needed) and compiles ASAN stdin fuzz driver.
func BuildTarget(ctx context.Context, repoRoot string, t Target) (binPath, clonePath string, err error) {
	if repoRoot == "" {
		repoRoot = "."
	}
	if TargetLanguage(t) == "rust" {
		return buildTargetRust(ctx, repoRoot, t)
	}
	return buildTargetC(ctx, repoRoot, t)
}

func buildTargetC(ctx context.Context, repoRoot string, t Target) (binPath, clonePath string, err error) {
	return buildTargetCSanitize(ctx, repoRoot, t, "address,undefined", "")
}

// BuildTargetMSAN clones upstream and compiles a MemorySanitizer stdin driver.
// MSAN hits are triage-only — not CVE claims without confirmation.
func BuildTargetMSAN(ctx context.Context, repoRoot string, t Target) (binPath, clonePath string, err error) {
	if TargetLanguage(t) == "rust" {
		return "", "", fmt.Errorf("fuzzupstream: MSAN lane is C/C++ only")
	}
	return buildTargetCSanitize(ctx, repoRoot, t, "memory", "msan")
}

// BuildTargetAFL compiles the stdin Hunt driver with afl-clang-fast / afl-cc and
// AFL_USE_ASAN=1 so AFL++ and afl-showmap observe real compile-time edges.
func BuildTargetAFL(ctx context.Context, repoRoot string, t Target) (binPath, clonePath string, err error) {
	if TargetLanguage(t) == "rust" {
		return "", "", fmt.Errorf("fuzzupstream: AFL lane is C/C++ only")
	}
	aflCC, err := lookAFLClangFast()
	if err != nil {
		return "", "", err
	}
	if repoRoot == "" {
		repoRoot = "."
	}
	cacheDir := filepath.Join(repoRoot, ".cache", "oss-cve-clones")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", "", err
	}
	clonePath = filepath.Join(cacheDir, t.ID)
	if err := cloneRepo(ctx, t.Repo, t.Ref, clonePath); err != nil {
		return "", "", err
	}
	if err := injectOSSCveBuildStubs(repoRoot, t.ID, clonePath); err != nil {
		return "", "", err
	}
	driverSrc := DriverSourcePath(repoRoot, t)
	if _, err := os.Stat(driverSrc); err != nil {
		return "", "", fmt.Errorf("fuzzupstream: driver %s: %w", driverSrc, err)
	}
	outDir := filepath.Join(repoRoot, ".cache", "hunt-afl-bin")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", "", err
	}
	sumInput := "aflasan|" + t.ID + t.Repo + t.Ref + t.Driver + strings.Join(t.UpstreamSrc, ",") + strings.Join(t.BuildFlags, ",")
	sum := sha256.Sum256([]byte(sumInput))
	binPath = filepath.Join(outDir, t.ID+"-aflasan-"+hex.EncodeToString(sum[:6])+".bin")

	buildMu.Lock()
	defer buildMu.Unlock()
	if st, err := os.Stat(binPath); err == nil && st.Mode().IsRegular() {
		return binPath, clonePath, nil
	}

	tmpDir, err := os.MkdirTemp("", "oss-cve-afl-build-*")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmpDir)
	tmpBin := filepath.Join(tmpDir, "fuzz")

	driverDir := filepath.Dir(driverSrc)
	args := []string{
		"-fno-omit-frame-pointer",
		"-g", "-O1",
		"-I", driverDir,
		"-o", tmpBin,
		driverSrc,
	}
	for _, inc := range t.IncludeDirs {
		args = append(args, "-I", includeDir(repoRoot, clonePath, inc))
	}
	for _, src := range expandUpstreamSrc(clonePath, t.UpstreamSrc) {
		args = append(args, src)
	}
	for _, flag := range t.BuildFlags {
		args = append(args, flag)
	}

	buildCtx, cancel := context.WithTimeout(ctx, buildTimeout(t))
	defer cancel()
	env := append(ScrubBuildEnv(os.Environ()),
		"AFL_USE_ASAN=1",
		"AFL_QUIET=1",
		"AFL_PATH="+filepath.Dir(aflCC),
	)
	binds := catalogBuildBinds(repoRoot, tmpDir, clonePath, driverSrc, t, filepath.Dir(aflCC))
	stderr, err := runBuildCmd(buildCtx, aflCC, args, env, "", binds, buildTimeout(t))
	if err != nil {
		return "", "", fmt.Errorf("afl-cc build %s: %w (%s)", t.ID, err, strings.TrimSpace(stderr))
	}
	in, err := os.ReadFile(tmpBin)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(binPath, in, 0o755); err != nil {
		return "", "", err
	}
	return binPath, clonePath, nil
}

func lookAFLClangFast() (string, error) {
	for _, name := range []string{"afl-clang-fast", "afl-cc", "afl-clang"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	prefixes := []string{
		strings.TrimSpace(os.Getenv("HACKME_PREFIX")),
		"/opt/hackme",
		filepath.Join(os.Getenv("HOME"), ".local/share/hackme/toolchains"),
	}
	for _, pref := range prefixes {
		if pref == "" {
			continue
		}
		for _, rel := range []string{
			"aflpp/bin/afl-clang-fast", "aflpp/bin/afl-cc",
			"bin/afl-clang-fast", "bin/afl-cc",
		} {
			p := filepath.Join(pref, rel)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("fuzzupstream: afl-clang-fast/afl-cc not found")
}

func buildTargetCSanitize(ctx context.Context, repoRoot string, t Target, sanitize, suffix string) (binPath, clonePath string, err error) {
	if _, err := exec.LookPath("clang"); err != nil {
		return "", "", fmt.Errorf("fuzzupstream: clang required")
	}
	cacheDir := filepath.Join(repoRoot, ".cache", "oss-cve-clones")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", "", err
	}
	clonePath = filepath.Join(cacheDir, t.ID)
	if err := cloneRepo(ctx, t.Repo, t.Ref, clonePath); err != nil {
		return "", "", err
	}
	if err := injectOSSCveBuildStubs(repoRoot, t.ID, clonePath); err != nil {
		return "", "", err
	}
	driverSrc := DriverSourcePath(repoRoot, t)
	if _, err := os.Stat(driverSrc); err != nil {
		return "", "", fmt.Errorf("fuzzupstream: driver %s: %w", driverSrc, err)
	}
	outDir := filepath.Join(repoRoot, ".cache", "oss-cve-bin")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", "", err
	}
	sumInput := t.ID + t.Repo + t.Ref + t.Driver + sanitize + strings.Join(t.UpstreamSrc, ",") + strings.Join(t.BuildFlags, ",")
	sum := sha256.Sum256([]byte(sumInput))
	name := t.ID + "-" + hex.EncodeToString(sum[:6])
	if suffix != "" {
		name += "-" + suffix
	}
	binPath = filepath.Join(outDir, name+".bin")

	buildMu.Lock()
	defer buildMu.Unlock()
	if st, err := os.Stat(binPath); err == nil && st.Mode().IsRegular() {
		return binPath, clonePath, nil
	}

	tmpDir, err := os.MkdirTemp("", "oss-cve-build-*")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmpDir)
	tmpBin := filepath.Join(tmpDir, "fuzz")

	driverDir := filepath.Dir(driverSrc)
	args := []string{
		"-fsanitize=" + sanitize,
		"-fno-omit-frame-pointer",
		"-g", "-O1",
		"-I", driverDir,
		"-o", tmpBin,
		driverSrc,
	}
	for _, inc := range t.IncludeDirs {
		args = append(args, "-I", includeDir(repoRoot, clonePath, inc))
	}
	for _, src := range expandUpstreamSrc(clonePath, t.UpstreamSrc) {
		args = append(args, src)
	}
	for _, flag := range t.BuildFlags {
		args = append(args, flag)
	}

	buildCtx, cancel := context.WithTimeout(ctx, buildTimeout(t))
	defer cancel()
	binds := catalogBuildBinds(repoRoot, tmpDir, clonePath, driverSrc, t)
	stderr, err := runBuildCmd(buildCtx, "clang", args, ScrubBuildEnv(os.Environ()), "", binds, buildTimeout(t))
	if err != nil {
		return "", "", fmt.Errorf("clang build %s: %w (%s)", t.ID, err, strings.TrimSpace(stderr))
	}
	in, err := os.ReadFile(tmpBin)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(binPath, in, 0o755); err != nil {
		return "", "", err
	}
	return binPath, clonePath, nil
}

func includeDir(repoRoot, clonePath, inc string) string {
	if filepath.IsAbs(inc) || strings.HasPrefix(inc, "tasks/") {
		return filepath.Join(repoRoot, inc)
	}
	return filepath.Join(clonePath, inc)
}

// catalogBuildBinds lists RW mounts for catalog clang/afl-cc cages.
// Never bind the whole repoRoot (/opt/hackme on hub) — only tmp, clone, driver,
// include dirs, and optional toolchain paths (e.g. afl-cc dir).
func catalogBuildBinds(repoRoot, tmpDir, clonePath, driverSrc string, t Target, extra ...string) []string {
	binds := make([]string, 0, 8+len(t.IncludeDirs)+len(extra))
	binds = append(binds, tmpDir, clonePath, filepath.Dir(driverSrc))
	for _, inc := range t.IncludeDirs {
		binds = append(binds, includeDir(repoRoot, clonePath, inc))
	}
	binds = append(binds, extra...)
	out := make([]string, 0, len(binds))
	seen := map[string]struct{}{}
	repoClean := ""
	if repoRoot != "" {
		if abs, err := filepath.Abs(filepath.Clean(repoRoot)); err == nil {
			repoClean = abs
		}
	}
	for _, p := range binds {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		abs, err := filepath.Abs(filepath.Clean(p))
		if err != nil || abs == "" || abs == "/" {
			continue
		}
		// Defense in depth: never remount the repo root wholesale.
		if repoClean != "" && abs == repoClean {
			continue
		}
		if abs == "/opt/hackme" {
			continue
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		out = append(out, abs)
	}
	return out
}

func injectOSSCveBuildStubs(repoRoot, targetID, clonePath string) error {
	copyStub := func(src, dst string) error {
		in, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, in, 0o644)
	}
	switch targetID {
	case "msgpack-c":
		src := filepath.Join(repoRoot, "tasks", "sources", "fuzz", "oss", "msgpack-config", "msgpack", "sysdep.h")
		return copyStub(src, filepath.Join(clonePath, "include", "msgpack", "sysdep.h"))
	case "libyaml":
		src := filepath.Join(repoRoot, "tasks", "sources", "fuzz", "oss", "libyaml-config", "config.h")
		return copyStub(src, filepath.Join(clonePath, "src", "config.h"))
	case "mxml":
		src := filepath.Join(repoRoot, "tasks", "sources", "fuzz", "oss", "mxml-config", "config.h")
		return copyStub(src, filepath.Join(clonePath, "config.h"))
	case "oniguruma":
		src := filepath.Join(repoRoot, "tasks", "sources", "fuzz", "oss", "oniguruma-config", "config.h")
		return copyStub(src, filepath.Join(clonePath, "src", "config.h"))
	case "miniz":
		src := filepath.Join(repoRoot, "tasks", "sources", "fuzz", "oss", "miniz-config", "miniz_export.h")
		return copyStub(src, filepath.Join(clonePath, "miniz_export.h"))
	case "nghttp2":
		cfgDir := filepath.Join(repoRoot, "tasks", "sources", "fuzz", "oss", "nghttp2-config")
		if err := copyStub(filepath.Join(cfgDir, "config.h"), filepath.Join(clonePath, "config.h")); err != nil {
			return err
		}
		src := filepath.Join(cfgDir, "nghttp2", "nghttp2ver.h")
		return copyStub(src, filepath.Join(clonePath, "lib", "includes", "nghttp2", "nghttp2ver.h"))
	case "pcre2":
		cfgDir := filepath.Join(repoRoot, "tasks", "sources", "fuzz", "oss", "pcre2-config")
		if err := copyStub(filepath.Join(cfgDir, "config.h"), filepath.Join(clonePath, "src", "config.h")); err != nil {
			return err
		}
		if err := copyStub(filepath.Join(cfgDir, "pcre2_chartables.c"), filepath.Join(clonePath, "src", "pcre2_chartables.c")); err != nil {
			return err
		}
		return copyStub(filepath.Join(cfgDir, "pcre2.h"), filepath.Join(clonePath, "src", "pcre2.h"))
	case "libxml2":
		cfgDir := filepath.Join(repoRoot, "tasks", "sources", "fuzz", "oss", "libxml2-config")
		if err := copyStub(filepath.Join(cfgDir, "config.h"), filepath.Join(clonePath, "config.h")); err != nil {
			return err
		}
		return copyStub(filepath.Join(cfgDir, "xmlversion.h"), filepath.Join(clonePath, "include", "libxml", "xmlversion.h"))
	case "uriparser":
		src := filepath.Join(repoRoot, "tasks", "sources", "fuzz", "oss", "uriparser-config", "UriConfig.h")
		return copyStub(src, filepath.Join(clonePath, "src", "UriConfig.h"))
	case "duktape":
		if _, err := os.Stat(filepath.Join(clonePath, "src", "duktape.c")); err == nil {
			return nil
		}
		return extractDuktapeRelease(clonePath, "v2.7.0")
	case "libcbor":
		cfgDir := filepath.Join(repoRoot, "tasks", "sources", "fuzz", "oss", "libcbor-config")
		if err := copyStub(filepath.Join(cfgDir, "cbor", "cbor_export.h"), filepath.Join(clonePath, "src", "cbor", "cbor_export.h")); err != nil {
			return err
		}
		return copyStub(filepath.Join(cfgDir, "cbor", "configuration.h"), filepath.Join(clonePath, "src", "cbor", "configuration.h"))
	case "tinycbor":
		cfgDir := filepath.Join(repoRoot, "tasks", "sources", "fuzz", "oss", "tinycbor-config")
		if err := copyStub(filepath.Join(cfgDir, "tinycbor-export.h"), filepath.Join(clonePath, "src", "tinycbor-export.h")); err != nil {
			return err
		}
		return copyStub(filepath.Join(cfgDir, "tinycbor-version.h"), filepath.Join(clonePath, "src", "tinycbor-version.h"))
	case "expat":
		src := filepath.Join(repoRoot, "tasks", "sources", "fuzz", "oss", "expat_config.h")
		return copyStub(src, filepath.Join(clonePath, "expat", "lib", "expat_config.h"))
	case "redisx":
		// RESP parse harness needs Sigmyne/xchange (headers + sources) vendored into the clone.
		xchangeClone := filepath.Join(repoRoot, ".cache", "oss-cve-clones", "xchange")
		if _, err := os.Stat(filepath.Join(xchangeClone, ".git")); err != nil {
			if err := cloneRepo(context.Background(), "https://github.com/Sigmyne/xchange", "main", xchangeClone); err != nil {
				return fmt.Errorf("redisx: ensure xchange clone: %w", err)
			}
		}
		depDir := filepath.Join(clonePath, "deps", "xchange")
		if err := os.MkdirAll(depDir, 0o755); err != nil {
			return err
		}
		for _, name := range []string{"xchange.h", "xjson.h", "xmutex.h"} {
			if err := copyStub(filepath.Join(xchangeClone, "include", name), filepath.Join(depDir, name)); err != nil {
				return err
			}
		}
		for _, name := range []string{"xchange.c", "xjson.c", "xlookup.c", "xstruct.c"} {
			if err := copyStub(filepath.Join(xchangeClone, "src", name), filepath.Join(depDir, name)); err != nil {
				return err
			}
		}
		return nil
	default:
		return nil
	}
}

func expandUpstreamSrc(clonePath string, patterns []string) []string {
	var out []string
	for _, pat := range patterns {
		if strings.Contains(pat, "*") {
			matches, err := filepath.Glob(filepath.Join(clonePath, pat))
			if err != nil || len(matches) == 0 {
				continue
			}
			sort.Strings(matches)
			out = append(out, matches...)
			continue
		}
		out = append(out, filepath.Join(clonePath, pat))
	}
	return out
}

func extractDuktapeRelease(clonePath, version string) error {
	url := fmt.Sprintf("https://github.com/svaarala/duktape/releases/download/%s/duktape-%s.tar.xz", version, strings.TrimPrefix(version, "v"))
	tmp, err := os.CreateTemp("", "duktape-release-*.tar.xz")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		tmp.Close()
		return err
	}
	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("duktape release download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		tmp.Close()
		return fmt.Errorf("duktape release download: HTTP %d", resp.StatusCode)
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	srcDir := filepath.Join(clonePath, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		return err
	}
	prefix := "duktape-" + strings.TrimPrefix(version, "v") + "/src"
	cmd := exec.Command("tar", "-xJf", tmpPath, "-C", srcDir, "--strip-components=2", prefix)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("duktape release extract: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	if _, err := os.Stat(filepath.Join(srcDir, "duktape.c")); err != nil {
		return fmt.Errorf("duktape release: src/duktape.c missing after extract")
	}
	return nil
}

var cloneMu sync.Map // dest path -> *sync.Mutex

func lockCloneDest(dest string) func() {
	v, _ := cloneMu.LoadOrStore(dest, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func cloneRepo(ctx context.Context, repo, ref, dest string) error {
	unlock := lockCloneDest(dest)
	defer unlock()
	var err error
	repo, err = gitutil.SanitizeURL(repo)
	if err != nil {
		return err
	}
	ref, err = gitutil.SanitizeRef(ref)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); err == nil {
		return checkoutCloneRef(ctx, dest, ref)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	cloneCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cloneCtx, "git", "clone", "--depth", "1", "--branch", ref, "--", repo, dest)
	gitutil.IsolateCmd(cmd)
	if err := cmd.Run(); err != nil {
		// fallback: clone default branch then checkout ref
		_ = os.RemoveAll(dest)
		cmd2 := exec.CommandContext(cloneCtx, "git", "clone", "--depth", "1", "--", repo, dest)
		gitutil.IsolateCmd(cmd2)
		if err2 := cmd2.Run(); err2 != nil {
			return fmt.Errorf("git clone %s: %w", repo, err)
		}
		return checkoutCloneRef(ctx, dest, ref)
	}
	return nil
}

func checkoutCloneRef(ctx context.Context, dest, ref string) error {
	if ref == "" {
		return nil
	}
	var err error
	ref, err = gitutil.SanitizeRef(ref)
	if err != nil {
		return err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	refs := []string{ref}
	if ref == "master" {
		refs = append(refs, "main")
	} else if ref == "main" {
		refs = append(refs, "master")
	}
	for _, r := range refs {
		r, err := gitutil.SanitizeRef(r)
		if err != nil {
			continue
		}
		fetch := exec.CommandContext(checkCtx, "git", "-C", dest, "fetch", "--depth", "1", "origin", r)
		gitutil.IsolateCmd(fetch)
		_ = fetch.Run()
		// Checkout a branch/tag/commit — do not use "--" (pathspec) before the ref.
		co := exec.CommandContext(checkCtx, "git", "-C", dest, "checkout", "--force", r)
		gitutil.IsolateCmd(co)
		if co.Run() == nil {
			return nil
		}
	}
	// Prefer an independent short timeout: checkCtx may already be expired under
	// parallel fetch load, which would falsely report "no valid ref".
	revCtx, revCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer revCancel()
	rev := exec.CommandContext(revCtx, "git", "-C", dest, "rev-parse", "HEAD")
	gitutil.IsolateCmd(rev)
	if rev.Run() == nil {
		return nil
	}
	return fmt.Errorf("git checkout %s in %s: no valid ref", ref, dest)
}

func buildTimeout(t Target) time.Duration {
	switch t.ID {
	case "libxml2", "duktape", "nghttp2":
		return 300 * time.Second
	case "serde_json", "memchr", "quick_xml":
		return 600 * time.Second
	default:
		if TargetLanguage(t) == "rust" {
			return 600 * time.Second
		}
		return 120 * time.Second
	}
}

// InjectOSSCveBuildStubs applies target-specific clone stubs before OSS builds.
func InjectOSSCveBuildStubs(repoRoot, targetID, clonePath string) error {
	return injectOSSCveBuildStubs(repoRoot, targetID, clonePath)
}

// IncludeDir resolves manifest include_dirs relative to clone or repo tasks/.
func IncludeDir(repoRoot, clonePath, inc string) string {
	return includeDir(repoRoot, clonePath, inc)
}

// ExpandUpstreamSrc expands manifest upstream_src globs under a clone directory.
func ExpandUpstreamSrc(clonePath string, patterns []string) []string {
	return expandUpstreamSrc(clonePath, patterns)
}
