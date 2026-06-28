package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// minerNotice is a remote announcement shown on the ecosystem tab without rebuilding the desktop binary.
type minerNotice struct {
	ID                 string `json:"id"`
	Severity           string `json:"severity"` // info | warn | critical
	Title              string `json:"title"`
	Body               string `json:"body"`
	LinkURL            string `json:"link_url,omitempty"`
	LinkLabel          string `json:"link_label,omitempty"`
	DismissKey         string `json:"dismiss_key,omitempty"`
	RecommendedVersion string `json:"recommended_version,omitempty"`
	ExpiresUnix        int64  `json:"expires_unix,omitempty"`
}

type minerNoticesDoc struct {
	Version     int           `json:"version"`
	UpdatedUnix int64         `json:"updated_unix"`
	Notices     []minerNotice `json:"notices"`
}

var (
	minerNoticesCacheMu sync.Mutex
	minerNoticesCache   minerNoticesDoc
	minerNoticesCached  time.Time
)

func minerNoticesURL() string {
	if u := strings.TrimSpace(os.Getenv("HACKME_MINER_NOTICES_URL")); u != "" {
		return u
	}
	return "https://hackme.tech/assets/miner-notices.json"
}

func minerNoticesLocalFile() string {
	for _, key := range []string{"HACKME_MINER_NOTICES_FILE", "MINER_NOTICES_JSON"} {
		if p := strings.TrimSpace(os.Getenv(key)); p != "" {
			return p
		}
	}
	for _, candidate := range []string{
		filepath.Join("web", "site", "assets", "miner-notices.json"),
		filepath.Join("data", "miner-notices.json"),
	} {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
	}
	return ""
}

func readMinerNoticesFile(path string) (minerNoticesDoc, error) {
	var out minerNoticesDoc
	raw, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	if out.Notices == nil {
		out.Notices = []minerNotice{}
	}
	return out, nil
}

var minerNoticesHTTPOnce sync.Once
var minerNoticesHTTP *http.Client

func minerNoticesHTTPClient() *http.Client {
	minerNoticesHTTPOnce.Do(func() {
		minerNoticesHTTP = &http.Client{
			Timeout: 3 * time.Second,
			Transport: &http.Transport{
				Proxy:                 nil,
				ForceAttemptHTTP2:     false,
				TLSHandshakeTimeout:   3 * time.Second,
				ResponseHeaderTimeout: 3 * time.Second,
			},
		}
	})
	return minerNoticesHTTP
}

func fetchMinerNotices(ctx context.Context) (minerNoticesDoc, error) {
	if p := minerNoticesLocalFile(); p != "" {
		if out, err := readMinerNoticesFile(p); err == nil {
			return out, nil
		}
	}
	minerNoticesCacheMu.Lock()
	if !minerNoticesCached.IsZero() && time.Since(minerNoticesCached) < 60*time.Second {
		out := minerNoticesCache
		minerNoticesCacheMu.Unlock()
		return out, nil
	}
	minerNoticesCacheMu.Unlock()

	var out minerNoticesDoc
	url := minerNoticesURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return out, err
	}
	resp, err := minerNoticesHTTPClient().Do(req)
	if err != nil {
		minerNoticesCacheMu.Lock()
		if !minerNoticesCached.IsZero() {
			out = minerNoticesCache
			minerNoticesCacheMu.Unlock()
			return out, nil
		}
		minerNoticesCacheMu.Unlock()
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("miner notices http %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	if out.Notices == nil {
		out.Notices = []minerNotice{}
	}
	minerNoticesCacheMu.Lock()
	minerNoticesCache = out
	minerNoticesCached = time.Now()
	minerNoticesCacheMu.Unlock()
	return out, nil
}

func filterActiveMinerNotices(doc minerNoticesDoc, nodeVersion string) []minerNotice {
	now := time.Now().Unix()
	out := make([]minerNotice, 0, len(doc.Notices))
	for _, n := range doc.Notices {
		if n.ExpiresUnix > 0 && now > n.ExpiresUnix {
			continue
		}
		if rv := strings.TrimSpace(n.RecommendedVersion); rv != "" && versionAtLeast(nodeVersion, rv) {
			continue
		}
		if strings.TrimSpace(n.Title) == "" && strings.TrimSpace(n.Body) == "" {
			continue
		}
		out = append(out, n)
	}
	return out
}

// versionAtLeast compares HackMe rc tags (0.1.0-rc11p style). Returns true when have >= want.
func versionAtLeast(have, want string) bool {
	return releaseChannelOrdinal(have) >= releaseChannelOrdinal(want)
}

func releaseChannelOrdinal(v string) int {
	v = strings.TrimSpace(strings.ToLower(v))
	if i := strings.LastIndex(v, "-rc"); i >= 0 {
		v = v[i+1:]
	} else if strings.HasPrefix(v, "rc") {
		// already rc11p
	} else {
		return 0
	}
	if !strings.HasPrefix(v, "rc") {
		return 0
	}
	rest := strings.TrimPrefix(v, "rc")
	num := 0
	letter := 0
	for i, ch := range rest {
		if ch >= '0' && ch <= '9' {
			num = num*10 + int(ch-'0')
			continue
		}
		if ch >= 'a' && ch <= 'z' && i == len(rest)-1 {
			letter = int(ch - 'a' + 1)
		}
		break
	}
	if num <= 0 {
		return 0
	}
	return num*32 + letter
}

func (a *app) handleMinerNotices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	doc, err := fetchMinerNotices(ctx)
	if err != nil {
		writeJSON(w, map[string]any{
			"ok":      false,
			"reason":  "fetch_failed",
			"message": err.Error(),
			"source":  minerNoticesURL(),
		})
		return
	}
	nodeVer := strings.TrimSpace(Version)
	if a != nil {
		nodeVer = strings.TrimSpace(Version)
	}
	active := filterActiveMinerNotices(doc, nodeVer)
	writeJSON(w, map[string]any{
		"ok":                  true,
		"source":              minerNoticesURL(),
		"version":             doc.Version,
		"updated_unix":        doc.UpdatedUnix,
		"node_version":        nodeVer,
		"notices":             active,
		"notices_all":         len(doc.Notices),
		"upgrade_recommended": len(active) > 0,
	})
}
