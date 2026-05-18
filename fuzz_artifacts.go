package main

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type fuzzArtifactCleanupRequest struct {
	TTLSeconds int64 `json:"ttl_sec"`
	MaxBytes   int64 `json:"max_bytes"`
}

type fuzzArtifactCleanupResult struct {
	RootPath     string `json:"root_path"`
	TTLSeconds   int64  `json:"ttl_sec"`
	MaxBytes     int64  `json:"max_bytes"`
	DeletedFiles int64  `json:"deleted_files"`
	DeletedBytes int64  `json:"deleted_bytes"`
	KeptFiles    int64  `json:"kept_files"`
	KeptBytes    int64  `json:"kept_bytes"`
}

type fuzzArtifactFile struct {
	path    string
	size    int64
	modUnix int64
}

func fuzzArtifactRoot() string {
	v := strings.TrimSpace(os.Getenv("HACKME_FUZZ_ARTIFACT_DIR"))
	if v == "" {
		return filepath.Join(".", "data", "fuzz-artifacts")
	}
	return v
}

func fuzzArtifactTTLSeconds(def int64) int64 {
	v := strings.TrimSpace(os.Getenv("HACKME_FUZZ_ARTIFACT_TTL_SEC"))
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	if n < 0 {
		return 0
	}
	if n > 31536000 {
		return 31536000
	}
	return n
}

func fuzzArtifactMaxBytes(def int64) int64 {
	v := strings.TrimSpace(os.Getenv("HACKME_FUZZ_ARTIFACT_MAX_BYTES"))
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	if n < 0 {
		return 0
	}
	maxCap := int64(50 * 1024 * 1024 * 1024) // 50 GiB hard safety cap.
	if n > maxCap {
		return maxCap
	}
	return n
}

func (a *app) runFuzzArtifactCleanup(ctx context.Context, ttlSec, maxBytes int64) (fuzzArtifactCleanupResult, error) {
	root := fuzzArtifactRoot()
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fuzzArtifactCleanupResult{}, err
	}
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		return fuzzArtifactCleanupResult{}, err
	}
	res := fuzzArtifactCleanupResult{
		RootPath:   absRoot,
		TTLSeconds: ttlSec,
		MaxBytes:   maxBytes,
	}
	now := time.Now().Unix()
	files := make([]fuzzArtifactFile, 0, 256)
	var totalBytes int64
	walkErr := filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return nil
		}
		if rel, relErr := filepath.Rel(absRoot, absPath); relErr != nil || strings.HasPrefix(rel, "..") {
			return nil
		}
		size := info.Size()
		totalBytes += size
		files = append(files, fuzzArtifactFile{
			path:    absPath,
			size:    size,
			modUnix: info.ModTime().Unix(),
		})
		return nil
	})
	if walkErr != nil {
		return res, walkErr
	}

	kept := make([]fuzzArtifactFile, 0, len(files))
	for _, f := range files {
		expired := ttlSec > 0 && now-f.modUnix > ttlSec
		if expired {
			if err := os.Remove(f.path); err == nil {
				res.DeletedFiles++
				res.DeletedBytes += f.size
				totalBytes -= f.size
				continue
			}
		}
		kept = append(kept, f)
	}

	if maxBytes > 0 && totalBytes > maxBytes && len(kept) > 0 {
		sort.Slice(kept, func(i, j int) bool {
			if kept[i].modUnix == kept[j].modUnix {
				return kept[i].path < kept[j].path
			}
			return kept[i].modUnix < kept[j].modUnix
		})
		for _, f := range kept {
			if totalBytes <= maxBytes {
				break
			}
			if err := os.Remove(f.path); err == nil {
				res.DeletedFiles++
				res.DeletedBytes += f.size
				totalBytes -= f.size
			}
		}
	}

	_ = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			return nil
		}
		res.KeptFiles++
		res.KeptBytes += info.Size()
		return nil
	})
	return res, nil
}

func (a *app) handleFuzzArtifactsCleanup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	if !requireAdminAuth(w, r) {
		return
	}
	logAdminAction(r, "fuzz_artifacts_cleanup_post")
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	req := fuzzArtifactCleanupRequest{
		TTLSeconds: fuzzArtifactTTLSeconds(7 * 24 * 3600),
		MaxBytes:   fuzzArtifactMaxBytes(512 * 1024 * 1024),
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.TTLSeconds < 0 {
		req.TTLSeconds = 0
	}
	if req.MaxBytes < 0 {
		req.MaxBytes = 0
	}
	result, err := a.runFuzzArtifactCleanup(r.Context(), req.TTLSeconds, req.MaxBytes)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "artifacts_cleanup_failed", "artifacts cleanup failed", nil)
		return
	}
	writeJSON(w, map[string]any{
		"ok":        true,
		"artifacts": result,
	})
}
