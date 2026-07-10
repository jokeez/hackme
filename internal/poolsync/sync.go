package poolsync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// RegisterRequest is the coordinator pool campaign upsert body.
type RegisterRequest struct {
	ID            string         `json:"id"`
	CampaignType  string         `json:"campaign_type"`
	Title         string         `json:"title"`
	Description   string         `json:"description"`
	Status        string         `json:"status"`
	BudgetRuns    int            `json:"budget_runs"`
	BudgetSeconds int            `json:"budget_seconds"`
	Config        map[string]any `json:"config"`
}

// Config from environment.
func timeoutDuration() time.Duration {
	sec := 25
	if v := strings.TrimSpace(os.Getenv("HACKME_POOL_SYNC_TIMEOUT_SEC")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 5 && n <= 180 {
			sec = n
		}
	} else if u := strings.ToLower(ResolveCoordinatorURL()); strings.Contains(u, "hackme.tech") || strings.Contains(u, "/pool/coordinator") {
		// Public coordinator POST can be slow through CDN when node is under load.
		sec = 60
	}
	return time.Duration(sec) * time.Second
}

func maxAttempts() int {
	n := 4
	if v := strings.TrimSpace(os.Getenv("HACKME_POOL_SYNC_MAX_ATTEMPTS")); v != "" {
		if x, err := strconv.Atoi(v); err == nil && x >= 1 && x <= 12 {
			n = x
		}
	}
	return n
}

func retryBackoff(attempt int) time.Duration {
	if attempt <= 1 {
		return 0
	}
	d := time.Duration(1<<(attempt-2)) * time.Second
	if d > 8*time.Second {
		d = 8 * time.Second
	}
	return d
}

// ResolveCoordinatorURL picks the best coordinator base URL for pool register.
// On the command VPS, loopback :18081 is preferred when public URL is configured but local health is OK.
func ResolveCoordinatorURL() string {
	u := strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_URL")), "/")
	if u == "" {
		u = strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_URL")), "/")
	}
	if u == "" {
		return ""
	}
	prefer := strings.TrimSpace(os.Getenv("HACKME_POOL_SYNC_PREFER_LOOPBACK"))
	if prefer == "0" || prefer == "false" {
		return u
	}
	loop := strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_POOL_SYNC_LOOPBACK_URL")), "/")
	if loop == "" {
		loop = "http://127.0.0.1:18081"
	}
	// Only substitute when configured URL looks like the public reverse-proxy path.
	low := strings.ToLower(u)
	if !strings.Contains(low, "hackme.tech") && !strings.Contains(low, "/pool/coordinator") {
		return u
	}
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loop+"/health", nil)
	if err != nil {
		return u
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return u
	}
	res.Body.Close()
	if res.StatusCode == http.StatusOK {
		return loop
	}
	return u
}

func adminToken() string {
	for _, k := range []string{
		"HACKME_COORDINATOR_ADMIN_TOKEN",
		"HACKME_POOL_COORDINATOR_ADMIN_TOKEN",
		"HACKME_POOL_COORDINATOR_TOKEN",
	} {
		if t := strings.TrimSpace(os.Getenv(k)); t != "" {
			return t
		}
	}
	return ""
}

// RegisterOnce POSTs one campaign to the coordinator pool API.
func RegisterOnce(ctx context.Context, coordURL, token string, req RegisterRequest) (latency time.Duration, err error) {
	coordURL = strings.TrimRight(strings.TrimSpace(coordURL), "/")
	if coordURL == "" {
		return 0, fmt.Errorf("pool sync: coordinator URL not set")
	}
	if token == "" {
		return 0, fmt.Errorf("pool sync: coordinator admin token not set")
	}
	if req.Config == nil {
		req.Config = map[string]any{}
	}
	req.Config["pool_distributed"] = true
	if base, pull := ResolveOrdersSettleBase(); base != "" {
		req.Config["orders_settle_base"] = base
		if pull {
			req.Config["orders_settle_pull"] = true
		}
	}
	body, _ := json.Marshal(req)
	start := time.Now()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, coordURL+"/api/fuzz/pool/campaigns", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Hackme-Admin-Token", token)
	cl := &http.Client{Timeout: timeoutDuration()}
	res, err := cl.Do(httpReq)
	latency = time.Since(start)
	if err != nil {
		return latency, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return latency, fmt.Errorf("pool register HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	return latency, nil
}

// RegisterWithRetry attempts registration with exponential backoff.
func RegisterWithRetry(ctx context.Context, req RegisterRequest) error {
	coordURL := ResolveCoordinatorURL()
	token := adminToken()
	attempts := maxAttempts()
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			RecordRetry()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryBackoff(attempt)):
			}
		}
		RecordAttempt()
		lat, err := RegisterOnce(ctx, coordURL, token, req)
		if err == nil {
			RecordOK(req.ID, lat)
			return nil
		}
		lastErr = err
	}
	if lastErr != nil {
		RecordFail(req.ID, 0, lastErr)
	}
	return lastErr
}

// AsyncEnabled returns true unless HACKME_POOL_SYNC_ASYNC=0.
func AsyncEnabled() bool {
	v := strings.TrimSpace(os.Getenv("HACKME_POOL_SYNC_ASYNC"))
	return v == "" || v == "1" || strings.EqualFold(v, "true")
}
