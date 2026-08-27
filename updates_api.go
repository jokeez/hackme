package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const latestSchemaV1 = "hackme.release.latest.v1"

type latestPlatform struct {
	ID        string `json:"id"`
	File      string `json:"file"`
	SHA256    string `json:"sha256"`
	Kind      string `json:"kind"`
	URL       string `json:"url"`
	MirrorURL string `json:"mirror_url"`
	SizeBytes int64  `json:"size_bytes"`
}

type latestDoc struct {
	Schema       string           `json:"schema"`
	App          string           `json:"app"`
	Version      string           `json:"version"`
	Commit       string           `json:"commit"`
	BuildDateUTC string           `json:"build_date_utc"`
	Channel      string           `json:"channel"`
	MinUpdater   int              `json:"min_updater"`
	Notes        string           `json:"notes"`
	Platforms    []latestPlatform `json:"platforms"`
}

// Local updater protocol version (bump when latest.json shape breaks old scripts).
const localUpdaterProtocol = 1


type updateCheckCache struct {
	mu      sync.Mutex
	at      time.Time
	payload map[string]any
}

var updateCheckCached updateCheckCache

func latestJSONURLs() []string {
	var urls []string
	if u := strings.TrimSpace(os.Getenv("HACKME_LATEST_URL")); u != "" {
		urls = append(urls, u)
	}
	urls = append(urls,
		"https://hackme.tech/dist/latest.json",
		"https://github.com/jokeez/hackme/releases/latest/download/latest.json",
	)
	return urls
}

func fetchLatestJSON(client *http.Client) (*latestDoc, string, error) {
	var lastErr error
	for _, u := range latestJSONURLs() {
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "HackMe-UpdateCheck/"+Version)
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = errHTTPStatus(resp.StatusCode)
			continue
		}
		var doc latestDoc
		if err := json.Unmarshal(body, &doc); err != nil {
			lastErr = err
			continue
		}
		if doc.Schema != latestSchemaV1 {
			lastErr = errBadSchema(doc.Schema)
			continue
		}
		if strings.TrimSpace(doc.Version) == "" {
			lastErr = errEmptyVersion
			continue
		}
		return &doc, u, nil
	}
	if lastErr == nil {
		lastErr = errNoLatest
	}
	return nil, "", lastErr
}

type simpleError string

func (e simpleError) Error() string { return string(e) }

const (
	errNoLatest     simpleError = "could not fetch latest.json"
	errEmptyVersion simpleError = "latest.json missing version"
)

func errHTTPStatus(code int) error {
	return simpleError("latest.json HTTP " + strconv.Itoa(code))
}

func errBadSchema(got string) error {
	if got == "" {
		got = "empty"
	}
	return simpleError("unsupported latest.json schema: " + got)
}

func normalizeReleaseVersion(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Tolerate "HackMe 0.1.0-rc15" / "version=0.1.0-rc15"
	if i := strings.LastIndexAny(s, " \t="); i >= 0 && i+1 < len(s) {
		cand := strings.TrimSpace(s[i+1:])
		if looksLikeReleaseVersion(cand) {
			return cand
		}
	}
	if looksLikeReleaseVersion(s) {
		return s
	}
	return s
}

func looksLikeReleaseVersion(s string) bool {
	if len(s) < 3 || s[0] < '0' || s[0] > '9' {
		return false
	}
	dot := false
	for _, c := range s {
		if c == '.' {
			dot = true
			continue
		}
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '-' || c == '_' || c == '+' {
			continue
		}
		return false
	}
	return dot
}

func updateAvailable(local, remote string) bool {
	local = normalizeReleaseVersion(local)
	remote = normalizeReleaseVersion(remote)
	if local == "" || remote == "" {
		return false
	}
	return local != remote
}

func buildUpdateCheckPayload(doc *latestDoc, sourceURL string) map[string]any {
	local := normalizeReleaseVersion(Version)
	remote := normalizeReleaseVersion(doc.Version)
	plats := make([]map[string]any, 0, len(doc.Platforms))
	for _, p := range doc.Platforms {
		plats = append(plats, map[string]any{
			"id":         p.ID,
			"file":       p.File,
			"kind":       p.Kind,
			"sha256":     p.SHA256,
			"url":        p.URL,
			"mirror_url": p.MirrorURL,
			"size_bytes": p.SizeBytes,
		})
	}
	hintLinux := "bash update_hackme_miner.sh   # or: bash /opt/hackme/update_hackme_miner.sh"
	hintWin := "update_hackme_miner.bat   # or PowerShell: .\\update_hackme_miner.ps1"
	hintOS := "bash update_hackme_os_binaries.sh   # HackMe OS /opt/hackme in-place"
	minUp := doc.MinUpdater
	if minUp <= 0 {
		minUp = 1
	}
	return map[string]any{
		"ok":                    true,
		"schema":                doc.Schema,
		"local_version":         local,
		"remote_version":        remote,
		"remote_commit":         doc.Commit,
		"remote_build_date":     doc.BuildDateUTC,
		"channel":               doc.Channel,
		"min_updater":           minUp,
		"local_updater":         localUpdaterProtocol,
		"updater_protocol_ok":   localUpdaterProtocol >= minUp,
		"update_available":      updateAvailable(local, remote),
		"source_url":            sourceURL,
		"notes":                 doc.Notes,
		"platforms":             plats,
		"downloads_url":         "https://hackme.tech/downloads.html",
		"latest_json_url":       "https://hackme.tech/dist/latest.json",
		"update_hints": map[string]string{
			"linux":     hintLinux,
			"windows":   hintWin,
			"hackme_os": hintOS,
		},
		"checked_at_utc": time.Now().UTC().Format(time.RFC3339),
	}
}

// handleUpdatesCheck compares local Version to published latest.json (L1 update channel).
// Does not download or apply updates — dashboard shows commands / downloads link.
func (a *app) handleUpdatesCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if envBool("HACKME_UPDATE_CHECK_DISABLE", false) {
		writeJSON(w, map[string]any{
			"ok":               true,
			"disabled":         true,
			"local_version":    normalizeReleaseVersion(Version),
			"update_available": false,
			"notes":            "update check disabled (HACKME_UPDATE_CHECK_DISABLE=1)",
		})
		return
	}

	force := strings.TrimSpace(r.URL.Query().Get("force")) == "1" ||
		strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("force")), "true")

	updateCheckCached.mu.Lock()
	if !force && updateCheckCached.payload != nil && time.Since(updateCheckCached.at) < 60*time.Second {
		cached := updateCheckCached.payload
		updateCheckCached.mu.Unlock()
		writeJSON(w, cached)
		return
	}
	updateCheckCached.mu.Unlock()

	client := &http.Client{Timeout: 10 * time.Second}
	doc, src, err := fetchLatestJSON(client)
	if err != nil {
		writeJSON(w, map[string]any{
			"ok":               false,
			"local_version":    normalizeReleaseVersion(Version),
			"update_available": false,
			"error":            err.Error(),
			"downloads_url":    "https://hackme.tech/downloads.html",
			"latest_json_url":  "https://hackme.tech/dist/latest.json",
		})
		return
	}
	payload := buildUpdateCheckPayload(doc, src)
	updateCheckCached.mu.Lock()
	updateCheckCached.at = time.Now()
	updateCheckCached.payload = payload
	updateCheckCached.mu.Unlock()
	writeJSON(w, payload)
}
