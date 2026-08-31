package hunt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// RepoPinRequest pins a local path or git clone at ref.
type RepoPinRequest struct {
	Path   string `json:"path,omitempty"`
	GitURL string `json:"git_url,omitempty"`
	Ref    string `json:"ref,omitempty"`
}

// RepoPinResult is a pinned repo root for Hunt inventory/build.
type RepoPinResult struct {
	Path      string `json:"path"`
	GitURL    string `json:"git_url,omitempty"`
	Ref       string `json:"ref,omitempty"`
	CommitSHA string `json:"commit_sha,omitempty"`
	PinnedAt  int64  `json:"pinned_at"`
}

// PinRepo resolves a local directory or shallow git clone for Hunt.
func PinRepo(ctx context.Context, repoRoot string, req RepoPinRequest) (*RepoPinResult, error) {
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	gitURL := strings.TrimSpace(req.GitURL)
	ref := strings.TrimSpace(req.Ref)
	if ref == "" {
		ref = "main"
	}
	now := time.Now().Unix()
	if gitURL != "" {
		if !strings.HasPrefix(gitURL, "https://") && !strings.HasPrefix(gitURL, "git@") {
			return nil, errors.New("hunt pin: git_url must be https:// or git@")
		}
		sum := sha256.Sum256([]byte(gitURL))
		dest := filepath.Join(repoRoot, ".cache", "hunt-repos", hex.EncodeToString(sum[:8]))
		if err := cloneOrUpdate(ctx, gitURL, ref, dest); err != nil {
			return nil, err
		}
		sha, _ := gitHead(ctx, dest)
		return &RepoPinResult{Path: dest, GitURL: gitURL, Ref: ref, CommitSHA: sha, PinnedAt: now}, nil
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		return nil, errors.New("hunt pin: path or git_url required")
	}
	abs, err := resolveInventoryRoot(repoRoot, path)
	if err != nil {
		return nil, err
	}
	sha, _ := gitHead(ctx, abs)
	return &RepoPinResult{Path: abs, Ref: ref, CommitSHA: sha, PinnedAt: now}, nil
}

func cloneOrUpdate(ctx context.Context, gitURL, ref, dest string) error {
	if _, err := os.Stat(filepath.Join(dest, ".git")); err == nil {
		return checkoutCloneRef(ctx, dest, ref)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	cloneCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cloneCtx, "git", "clone", "--depth", "1", "--branch", ref, gitURL, dest)
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(dest)
		cmd2 := exec.CommandContext(cloneCtx, "git", "clone", "--depth", "1", gitURL, dest)
		if err2 := cmd2.Run(); err2 != nil {
			return fmt.Errorf("hunt pin: git clone: %w", err)
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
	return fmt.Errorf("hunt pin: git checkout %s failed", ref)
}

func gitHead(ctx context.Context, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}
