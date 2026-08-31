package hms

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// AbuseGuard rate-limits hot endpoints per IP and worker id.
type AbuseGuard struct {
	mu     sync.Mutex
	byKey  map[string][]int64
	limit  int
	window time.Duration
}

func NewAbuseGuard(limit int, window time.Duration) *AbuseGuard {
	if limit < 1 {
		limit = 60
	}
	if window < time.Second {
		window = time.Minute
	}
	return &AbuseGuard{
		byKey:  make(map[string][]int64),
		limit:  limit,
		window: window,
	}
}

func (g *AbuseGuard) Allow(key string) bool {
	now := time.Now().UnixNano()
	cut := now - g.window.Nanoseconds()
	g.mu.Lock()
	defer g.mu.Unlock()
	arr := g.byKey[key]
	var kept []int64
	for _, t := range arr {
		if t >= cut {
			kept = append(kept, t)
		}
	}
	if len(kept) >= g.limit {
		g.byKey[key] = kept
		return false
	}
	kept = append(kept, now)
	g.byKey[key] = kept
	return true
}

func (g *AbuseGuard) AllowHTTP(r *http.Request, workerID string) bool {
	ip := clientIP(r)
	key := ip + "|" + strings.TrimSpace(workerID)
	if !g.Allow(key) {
		return false
	}
	if workerID != "" {
		return g.Allow("w:" + workerID)
	}
	return true
}

func ValidateQuota(cfg Config, quotaGB int) error {
	if quotaGB < cfg.MinQuotaGB {
		return errf("quota below minimum %d GB", cfg.MinQuotaGB)
	}
	if quotaGB > cfg.MaxQuotaGB {
		return errf("quota above maximum %d GB", cfg.MaxQuotaGB)
	}
	return nil
}

type simpleError string

func (e simpleError) Error() string { return string(e) }

func errf(format string, args ...any) error {
	return simpleError(fmt.Sprintf(format, args...))
}
