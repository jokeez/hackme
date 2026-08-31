package hunt

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxHarnessArtifactBytes = 32 << 20 // 32 MiB

// PutHarnessArtifact stores a published Hunt harness binary keyed by hash.
func PutHarnessArtifact(ctx context.Context, db *sql.DB, hash string, data []byte, sourceRel string) error {
	if db == nil {
		return fmt.Errorf("hunt artifact: no database")
	}
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return fmt.Errorf("hunt artifact: hash required")
	}
	if len(data) == 0 {
		return fmt.Errorf("hunt artifact: empty binary")
	}
	if len(data) > maxHarnessArtifactBytes {
		return fmt.Errorf("hunt artifact: exceeds %d bytes", maxHarnessArtifactBytes)
	}
	now := time.Now().Unix()
	_, err := db.ExecContext(ctx,
		`INSERT INTO hunt_harness_artifacts (harness_hash, binary_blob, byte_size, source_rel, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(harness_hash) DO UPDATE SET
		   binary_blob=excluded.binary_blob,
		   byte_size=excluded.byte_size,
		   source_rel=CASE WHEN excluded.source_rel != '' THEN excluded.source_rel ELSE hunt_harness_artifacts.source_rel END,
		   created_at=excluded.created_at`,
		hash, data, len(data), strings.TrimSpace(sourceRel), now)
	return err
}

// GetHarnessArtifact loads a published harness binary.
func GetHarnessArtifact(ctx context.Context, db *sql.DB, hash string) ([]byte, error) {
	if db == nil {
		return nil, fmt.Errorf("hunt artifact: no database")
	}
	hash = strings.TrimSpace(hash)
	var blob []byte
	err := db.QueryRowContext(ctx,
		`SELECT binary_blob FROM hunt_harness_artifacts WHERE harness_hash=?`, hash).
		Scan(&blob)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("hunt artifact: %s not found", hash)
	}
	if err != nil {
		return nil, err
	}
	return blob, nil
}

// PublishHarnessFile reads a local harness binary into the artifact store.
func PublishHarnessFile(ctx context.Context, db *sql.DB, hash, path, sourceRel string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return PutHarnessArtifact(ctx, db, hash, data, sourceRel)
}

// MaterializeHarness writes a harness to repo cache, loading from DB or HTTP fetch URL when needed.
func MaterializeHarness(ctx context.Context, repoRoot, hash, fetchURL string, db *sql.DB) (string, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return "", fmt.Errorf("hunt artifact: hash required")
	}
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	cachePath := huntHarnessCachePath(repoRoot, hash)
	if st, err := osStat(cachePath); err == nil && st {
		harnessCache.Store(hash, cachePath)
		return cachePath, nil
	}
	var data []byte
	var err error
	if db != nil {
		data, err = GetHarnessArtifact(ctx, db, hash)
		if err != nil && strings.TrimSpace(fetchURL) == "" {
			return "", err
		}
	}
	if len(data) == 0 && strings.TrimSpace(fetchURL) != "" {
		data, err = fetchHarnessHTTP(ctx, fetchURL)
		if err != nil {
			return "", err
		}
	}
	if len(data) == 0 {
		return "", fmt.Errorf("hunt artifact: %s not available locally", hash)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return "", err
	}
	tmp := cachePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, cachePath); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	harnessCache.Store(hash, cachePath)
	return cachePath, nil
}

func fetchHarnessHTTP(ctx context.Context, url string) ([]byte, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, fmt.Errorf("hunt artifact: fetch url required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_WORKER_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_WORKER_TOKEN"))
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, fmt.Errorf("hunt artifact fetch HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxHarnessArtifactBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxHarnessArtifactBytes {
		return nil, fmt.Errorf("hunt artifact: fetch exceeds max size")
	}
	return data, nil
}

// HarnessFetchURL builds coordinator-relative fetch path for workers.
func HarnessFetchURL(hash string) string {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return ""
	}
	return "/api/fuzz/pool/hunt/harness/" + hash
}
