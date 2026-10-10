package hunt

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxHarnessArtifactBytes = 32 << 20 // 32 MiB

var harnessHashRe = regexp.MustCompile(`(?i)^[a-f0-9]{8,128}$`)

// ValidHarnessHash rejects path traversal / non-hex ids used in cache filenames.
func ValidHarnessHash(hash string) bool {
	hash = strings.TrimSpace(hash)
	return harnessHashRe.MatchString(hash)
}

// PutHarnessArtifact stores a published Hunt harness binary keyed by hash.
// When HarnessObjectDir is set, bytes go to disk and SQLite keeps metadata only
// (empty binary_blob + content_sha256) — issue #8 Phase 1.
// Report #32: BEGIN IMMEDIATE + no ON CONFLICT overwrite — concurrent publishes
// with different binaries for the same harness_hash cannot race-replace content.
func PutHarnessArtifact(ctx context.Context, db *sql.DB, hash string, data []byte, sourceRel string) error {
	if db == nil {
		return fmt.Errorf("hunt artifact: no database")
	}
	hash = strings.TrimSpace(strings.ToLower(hash))
	if !ValidHarnessHash(hash) {
		return fmt.Errorf("hunt artifact: invalid harness hash")
	}
	if len(data) == 0 {
		return fmt.Errorf("hunt artifact: empty binary")
	}
	if len(data) > maxHarnessArtifactBytes {
		return fmt.Errorf("hunt artifact: exceeds %d bytes", maxHarnessArtifactBytes)
	}
	fp := contentSHA256Hex(data)
	now := time.Now().Unix()
	sourceRel = strings.TrimSpace(sourceRel)

	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		}
	}()

	bindErr := fmt.Errorf("hunt artifact: harness_hash %s already bound to different binary", hash)

	if dir := HarnessObjectDir(); dir != "" && HarnessObjectExists(dir, hash) {
		existing, rerr := ReadHarnessObject(dir, hash)
		if rerr != nil {
			return rerr
		}
		if !bytesEqual(existing, data) {
			return bindErr
		}
	}

	var existingBlob []byte
	var existingFP string
	err = conn.QueryRowContext(ctx,
		`SELECT binary_blob, COALESCE(content_sha256,'') FROM hunt_harness_artifacts WHERE harness_hash=?`, hash).
		Scan(&existingBlob, &existingFP)
	switch {
	case err == nil:
		existingFP = strings.TrimSpace(strings.ToLower(existingFP))
		if existingFP != "" && existingFP != fp {
			return bindErr
		}
		if len(existingBlob) > 0 && !bytesEqual(existingBlob, data) {
			return bindErr
		}
		if dir := HarnessObjectDir(); dir != "" {
			if _, werr := WriteHarnessObject(dir, hash, data); werr != nil {
				return werr
			}
			_, err = conn.ExecContext(ctx,
				`UPDATE hunt_harness_artifacts SET binary_blob=X'', content_sha256=?, byte_size=?,
				 source_rel=CASE WHEN ?!='' THEN ? ELSE source_rel END WHERE harness_hash=?`,
				fp, len(data), sourceRel, sourceRel, hash)
		} else if existingFP == "" {
			_, err = conn.ExecContext(ctx,
				`UPDATE hunt_harness_artifacts SET content_sha256=?, byte_size=?,
				 source_rel=CASE WHEN ?!='' THEN ? ELSE source_rel END WHERE harness_hash=?`,
				fp, len(data), sourceRel, sourceRel, hash)
		}
		if err != nil {
			return err
		}
	case err == sql.ErrNoRows:
		storeBlob := data
		if dir := HarnessObjectDir(); dir != "" {
			if _, werr := WriteHarnessObject(dir, hash, data); werr != nil {
				return werr
			}
			storeBlob = []byte{}
		}
		_, err = conn.ExecContext(ctx,
			`INSERT INTO hunt_harness_artifacts (harness_hash, binary_blob, byte_size, source_rel, created_at, content_sha256)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			hash, storeBlob, len(data), sourceRel, now, fp)
		if err != nil {
			// UNIQUE race under a different connection: verify winner matches.
			var racedBlob []byte
			var racedFP string
			if qerr := conn.QueryRowContext(ctx,
				`SELECT binary_blob, COALESCE(content_sha256,'') FROM hunt_harness_artifacts WHERE harness_hash=?`, hash).
				Scan(&racedBlob, &racedFP); qerr == nil {
				racedFP = strings.TrimSpace(strings.ToLower(racedFP))
				if (racedFP != "" && racedFP == fp) || bytesEqual(racedBlob, data) {
					break
				}
				return bindErr
			}
			return err
		}
	default:
		return err
	}

	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return err
	}
	committed = true
	return nil
}

// GetHarnessArtifact loads a published harness binary (disk first, then SQLite BLOB).
func GetHarnessArtifact(ctx context.Context, db *sql.DB, hash string) ([]byte, error) {
	if db == nil {
		return nil, fmt.Errorf("hunt artifact: no database")
	}
	hash = strings.TrimSpace(strings.ToLower(hash))
	if !ValidHarnessHash(hash) {
		return nil, fmt.Errorf("hunt artifact: invalid harness hash")
	}
	if dir := HarnessObjectDir(); dir != "" {
		if data, err := ReadHarnessObject(dir, hash); err == nil {
			return data, nil
		}
	}
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
	if len(blob) == 0 {
		// Metadata-only row without disk file.
		if dir := HarnessObjectDir(); dir != "" && HarnessObjectExists(dir, hash) {
			return ReadHarnessObject(dir, hash)
		}
		return nil, fmt.Errorf("hunt artifact: %s not found", hash)
	}
	// Lazy backfill to disk when store is configured.
	if dir := HarnessObjectDir(); dir != "" {
		if _, werr := WriteHarnessObject(dir, hash, blob); werr == nil {
			fp := contentSHA256Hex(blob)
			_, _ = db.ExecContext(ctx,
				`UPDATE hunt_harness_artifacts SET binary_blob=X'', content_sha256=? WHERE harness_hash=?`, fp, hash)
		}
	}
	return blob, nil
}

// GetHarnessArtifactPath returns the on-disk path when the object store holds the harness.
func GetHarnessArtifactPath(hash string) (string, bool) {
	dir := HarnessObjectDir()
	if dir == "" {
		return "", false
	}
	path, err := HarnessObjectPath(dir, hash)
	if err != nil || !HarnessObjectExists(dir, hash) {
		return "", false
	}
	return path, true
}

// HarnessArtifactReady reports whether workers can fetch this harness (disk object or SQLite blob).
// Cheap claim/Tick gate — does not load the full binary into memory.
func HarnessArtifactReady(ctx context.Context, db *sql.DB, hash string) error {
	hash = strings.TrimSpace(strings.ToLower(hash))
	if !ValidHarnessHash(hash) {
		return fmt.Errorf("hunt artifact: invalid harness hash")
	}
	if dir := HarnessObjectDir(); dir != "" && HarnessObjectExists(dir, hash) {
		return nil
	}
	if db == nil {
		return fmt.Errorf("hunt artifact: %s not found", hash)
	}
	var blobLen int
	err := db.QueryRowContext(ctx,
		`SELECT length(binary_blob) FROM hunt_harness_artifacts WHERE harness_hash=?`, hash).
		Scan(&blobLen)
	if err == sql.ErrNoRows {
		return fmt.Errorf("hunt artifact: %s not found", hash)
	}
	if err != nil {
		return err
	}
	if blobLen > 0 {
		return nil
	}
	// Metadata-only row: ready only if object store file exists.
	if dir := HarnessObjectDir(); dir != "" && HarnessObjectExists(dir, hash) {
		return nil
	}
	return fmt.Errorf("hunt artifact: %s not found", hash)
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// PublishHarnessFile reads a local harness binary into the artifact store.
func PublishHarnessFile(ctx context.Context, db *sql.DB, hash, path, sourceRel string) error {
	root := RepoRoot()
	if root != "" {
		var err error
		path, err = MustUnderRoot(root, path)
		if err != nil {
			return fmt.Errorf("hunt artifact: harness path outside repo root: %w", err)
		}
		data, err := SafeReadFileUnder(root, path)
		if err != nil {
			return err
		}
		return PutHarnessArtifact(ctx, db, hash, data, sourceRel)
	}
	safe, err := allowlistedAbs(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(safe)
	if err != nil {
		return err
	}
	return PutHarnessArtifact(ctx, db, hash, data, sourceRel)
}

// GetHarnessContentSHA256 returns the attested sha256 hex of published harness bytes.
// If metadata is empty but bytes exist, it computes and backfills content_sha256.
func GetHarnessContentSHA256(ctx context.Context, db *sql.DB, hash string) (string, error) {
	if db == nil {
		return "", fmt.Errorf("hunt artifact: no database")
	}
	hash = strings.TrimSpace(strings.ToLower(hash))
	if !ValidHarnessHash(hash) {
		return "", fmt.Errorf("hunt artifact: invalid harness hash")
	}
	var fp string
	err := db.QueryRowContext(ctx,
		`SELECT COALESCE(content_sha256,'') FROM hunt_harness_artifacts WHERE harness_hash=?`, hash).
		Scan(&fp)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("hunt artifact: %s not found", hash)
	}
	if err != nil {
		return "", err
	}
	fp = strings.TrimSpace(strings.ToLower(fp))
	if fp != "" && len(fp) == 64 {
		return fp, nil
	}
	data, err := GetHarnessArtifact(ctx, db, hash)
	if err != nil {
		return "", err
	}
	fp = contentSHA256Hex(data)
	_, _ = db.ExecContext(ctx,
		`UPDATE hunt_harness_artifacts SET content_sha256=? WHERE harness_hash=?`, fp, hash)
	return fp, nil
}

// ValidContentSHA256 reports a full sha256 hex digest.
func ValidContentSHA256(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func verifyHarnessBytes(data []byte, expectedContentSHA256 string) error {
	want := strings.TrimSpace(strings.ToLower(expectedContentSHA256))
	if want == "" {
		return fmt.Errorf("hunt artifact: missing harness_content_sha256 attestation")
	}
	if !ValidContentSHA256(want) {
		return fmt.Errorf("hunt artifact: invalid harness_content_sha256")
	}
	got := contentSHA256Hex(data)
	if got != want {
		return fmt.Errorf("hunt artifact: content sha256 mismatch (got %s want %s)", got, want)
	}
	return nil
}

func quarantineHarnessCache(cachePath string) {
	if strings.TrimSpace(cachePath) == "" {
		return
	}
	_ = os.Remove(cachePath)
	_ = os.Remove(cachePath + ".sha256")
	_ = os.Remove(cachePath + ".bad")
}

func harnessCacheAttestationPath(cachePath string) string {
	return cachePath + ".sha256"
}

func writeHarnessCacheAttestation(cachePath, contentSHA string) error {
	contentSHA = strings.TrimSpace(strings.ToLower(contentSHA))
	if !ValidContentSHA256(contentSHA) {
		return fmt.Errorf("hunt artifact: invalid attestation")
	}
	return os.WriteFile(harnessCacheAttestationPath(cachePath), []byte(contentSHA+"\n"), 0o600)
}

func readVerifiedHarnessCache(cachePath, expectedContentSHA256 string) ([]byte, string, error) {
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("hunt artifact: empty cache file")
	}
	got := contentSHA256Hex(data)
	want := strings.TrimSpace(strings.ToLower(expectedContentSHA256))
	sideBytes, sideErr := os.ReadFile(harnessCacheAttestationPath(cachePath))
	side := strings.TrimSpace(strings.ToLower(string(sideBytes)))
	if sideErr == nil && side != "" {
		if side != got {
			quarantineHarnessCache(cachePath)
			return nil, "", fmt.Errorf("hunt artifact: cache attestation mismatch")
		}
		if want != "" && side != want {
			quarantineHarnessCache(cachePath)
			return nil, "", fmt.Errorf("hunt artifact: cache content sha256 mismatch")
		}
		return data, side, nil
	}
	// Legacy cache without sidecar: require expected attestation, then stamp sidecar.
	if want == "" {
		quarantineHarnessCache(cachePath)
		return nil, "", fmt.Errorf("hunt artifact: unattested cache rejected")
	}
	if verr := verifyHarnessBytes(data, want); verr != nil {
		quarantineHarnessCache(cachePath)
		return nil, "", verr
	}
	_ = writeHarnessCacheAttestation(cachePath, want)
	return data, want, nil
}

// MaterializeHarness writes a harness to repo cache, loading from DB or HTTP fetch URL when needed.
// expectedContentSHA256 is required whenever bytes come from cache or HTTP (supply-chain attestation).
// Local DB loads may omit it; the stored content_sha256 is used instead.
func MaterializeHarness(ctx context.Context, repoRoot, hash, fetchURL, expectedContentSHA256 string, db *sql.DB) (string, error) {
	hash = strings.TrimSpace(strings.ToLower(hash))
	if !ValidHarnessHash(hash) {
		return "", fmt.Errorf("hunt artifact: invalid harness hash")
	}
	want := strings.TrimSpace(strings.ToLower(expectedContentSHA256))
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	cachePath := huntHarnessCachePath(repoRoot, hash)

	// Cache hit: always re-hash; never execute unverified bytes.
	if st, err := osStat(cachePath); err == nil && st {
		att := want
		if att == "" && db != nil {
			if fp, gerr := GetHarnessContentSHA256(ctx, db, hash); gerr == nil {
				att = fp
			}
		}
		if cached, gotSHA, rerr := readVerifiedHarnessCache(cachePath, att); rerr == nil && len(cached) > 0 {
			_ = cached
			_ = gotSHA
			harnessCache.Store(hash, cachePath)
			return cachePath, nil
		}
		quarantineHarnessCache(cachePath)
		harnessCache.Delete(hash)
	}

	var data []byte
	var err error
	if db != nil {
		data, err = GetHarnessArtifact(ctx, db, hash)
		if err != nil && strings.TrimSpace(fetchURL) == "" {
			return "", err
		}
		if len(data) > 0 {
			att := want
			if att == "" {
				if fp, gerr := GetHarnessContentSHA256(ctx, db, hash); gerr == nil {
					att = fp
				} else {
					att = contentSHA256Hex(data)
				}
			}
			if verr := verifyHarnessBytes(data, att); verr != nil {
				return "", verr
			}
			want = att
		}
	}
	if len(data) == 0 && strings.TrimSpace(fetchURL) != "" {
		if want == "" || !ValidContentSHA256(want) {
			return "", fmt.Errorf("hunt artifact: HTTP fetch requires harness_content_sha256 attestation")
		}
		data, err = fetchHarnessHTTP(ctx, fetchURL)
		if err != nil {
			return "", err
		}
		if verr := verifyHarnessBytes(data, want); verr != nil {
			return "", verr
		}
	}
	if len(data) == 0 {
		return "", fmt.Errorf("hunt artifact: %s not available locally", hash)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return "", err
	}
	tmp, err := SafeCacheFile(repoRoot, "hunt-harness", hash, "bin.tmp")
	if err != nil {
		return "", err
	}
	if err := SafeWriteFileUnder(repoRoot, tmp, data, 0o755); err != nil {
		return "", err
	}
	if err := SafeRenameUnder(repoRoot, tmp, cachePath); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	att := want
	if att == "" {
		att = contentSHA256Hex(data)
	}
	if err := writeHarnessCacheAttestation(cachePath, att); err != nil {
		quarantineHarnessCache(cachePath)
		return "", err
	}
	harnessCache.Store(hash, cachePath)
	return cachePath, nil
}

func fetchHarnessHTTP(ctx context.Context, rawURL string) ([]byte, error) {
	rawURL = strings.TrimSpace(rawURL)
	if strings.HasPrefix(rawURL, "/api/fuzz/pool/hunt/harness/") {
		base := strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_URL"))
		if base == "" {
			base = strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_URL"))
		}
		if base == "" {
			base = strings.TrimSpace(os.Getenv("COORD_URL"))
		}
		if base == "" {
			return nil, fmt.Errorf("hunt artifact: relative fetch needs HACKME_POOL_COORDINATOR_URL")
		}
		rawURL = strings.TrimRight(base, "/") + rawURL
	}
	if !SafeHarnessFetchURL(rawURL) {
		return nil, fmt.Errorf("hunt artifact: fetch url not allowed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(rawURL)
	_, harnessOK := harnessFetchPathHash(u)
	attachBearer := harnessOK
	if attachBearer {
		if coord := strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_URL")); coord != "" {
			if !sameCoordinatorHost(coord, u) {
				attachBearer = false
			}
		} else if coord := strings.TrimSpace(os.Getenv("COORD_URL")); coord != "" {
			if !sameCoordinatorHost(coord, u) {
				attachBearer = false
			}
		}
	}
	if attachBearer {
		token := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_WORKER_TOKEN"))
		if token == "" {
			token = strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_WORKER_TOKEN"))
		}
		if token == "" {
			token = strings.TrimSpace(os.Getenv("COORD_TOKEN"))
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
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

// SafeHarnessFetchURL allows relative coordinator harness paths, or absolute URLs
// that match the configured coordinator host only (no arbitrary public HTTPS).
// Scheme must match the configured coordinator (https preferred); plain HTTP is
// allowed only when the coordinator URL itself is http (pool-direct / lab).
func SafeHarnessFetchURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if strings.HasPrefix(raw, "/") {
		_, ok := harnessFetchPathHash(&url.URL{Path: raw})
		return ok
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	if _, ok := harnessFetchPathHash(u); !ok {
		return false
	}
	coord := firstCoordinatorURL()
	if coord == "" {
		return false
	}
	cu, err := url.Parse(strings.TrimSpace(coord))
	if err != nil || cu.Host == "" {
		return false
	}
	if !sameCoordinatorHost(coord, u) {
		return false
	}
	// TLS policy: if coordinator is https, reject http absolute fetches.
	if strings.EqualFold(cu.Scheme, "https") && !strings.EqualFold(u.Scheme, "https") {
		return false
	}
	if strings.EqualFold(u.Scheme, "http") {
		allowHTTP := falsyEnv("HACKME_HUNT_HARNESS_REQUIRE_TLS") == false &&
			(strings.EqualFold(cu.Scheme, "http") || truthyEnv("HACKME_HUNT_HARNESS_ALLOW_HTTP"))
		if !allowHTTP {
			return false
		}
	}
	return true
}

func firstCoordinatorURL() string {
	for _, k := range []string{"HACKME_POOL_COORDINATOR_URL", "HACKME_COORDINATOR_URL", "COORD_URL"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func truthyEnv(key string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func falsyEnv(key string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	return v == "0" || v == "false" || v == "no" || v == "off"
}

func harnessFetchPathHash(u *url.URL) (string, bool) {
	if u == nil {
		return "", false
	}
	p := path.Clean(u.Path)
	for _, prefix := range []string{
		"/api/fuzz/pool/hunt/harness/",
		"/pool/coordinator/api/fuzz/pool/hunt/harness/",
	} {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		h := strings.Trim(strings.TrimPrefix(p, prefix), "/")
		if ValidHarnessHash(h) && !strings.Contains(h, "/") && !strings.Contains(h, "..") {
			return h, true
		}
	}
	return "", false
}

func sameCoordinatorHost(coordURL string, u *url.URL) bool {
	cu, err := url.Parse(strings.TrimSpace(coordURL))
	if err != nil || cu.Host == "" || u == nil || u.Host == "" {
		return false
	}
	if !strings.EqualFold(cu.Hostname(), u.Hostname()) {
		return false
	}
	return urlPortOrDefault(cu) == urlPortOrDefault(u)
}

func urlPortOrDefault(u *url.URL) string {
	if u == nil {
		return ""
	}
	if p := u.Port(); p != "" {
		return p
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}

// HarnessFetchURL builds coordinator-relative fetch path for workers.
func HarnessFetchURL(hash string) string {
	hash = strings.TrimSpace(hash)
	if !ValidHarnessHash(hash) {
		return ""
	}
	return "/api/fuzz/pool/hunt/harness/" + hash
}

// ContentFingerprint returns sha256 hex of harness bytes (ops / integrity checks).
func ContentFingerprint(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
