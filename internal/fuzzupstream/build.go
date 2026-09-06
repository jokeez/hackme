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
	sumInput := t.ID + t.Repo + t.Ref + t.Driver + strings.Join(t.UpstreamSrc, ",") + strings.Join(t.BuildFlags, ",")
	sum := sha256.Sum256([]byte(sumInput))
	binPath = filepath.Join(outDir, t.ID+"-"+hex.EncodeToString(sum[:6])+".bin")

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
		"-fsanitize=address,undefined",
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
	cmd := exec.CommandContext(buildCtx, "clang", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("clang build %s: %w (%s)", t.ID, err, strings.TrimSpace(stderr.String()))
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

func cloneRepo(ctx context.Context, repo, ref, dest string) error {
	if _, err := os.Stat(filepath.Join(dest, ".git")); err == nil {
		return checkoutCloneRef(ctx, dest, ref)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	cloneCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cloneCtx, "git", "clone", "--depth", "1", "--branch", ref, repo, dest)
	if err := cmd.Run(); err != nil {
		// fallback: clone default branch then checkout ref
		_ = os.RemoveAll(dest)
		cmd2 := exec.CommandContext(cloneCtx, "git", "clone", "--depth", "1", repo, dest)
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
	checkCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	refs := []string{ref}
	if ref == "master" {
		refs = append(refs, "main")
	} else if ref == "main" {
		refs = append(refs, "master")
	}
	for _, r := range refs {
		_ = exec.CommandContext(checkCtx, "git", "-C", dest, "fetch", "--depth", "1", "origin", r).Run()
		if exec.CommandContext(checkCtx, "git", "-C", dest, "checkout", "--force", r).Run() == nil {
			return nil
		}
	}
	if exec.CommandContext(checkCtx, "git", "-C", dest, "rev-parse", "HEAD").Run() == nil {
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
