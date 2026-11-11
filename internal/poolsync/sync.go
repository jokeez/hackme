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

	"hackme/internal/netutil"
)

// RegisterRequest is the coordinator pool campaign upsert body.
type RegisterRequest struct {
	ID                 string         `json:"id"`
	CampaignType       string         `json:"campaign_type"`
	Title              string         `json:"title"`
	Description        string         `json:"description"`
	OwnerRef           string         `json:"owner_ref,omitempty"`
	Status             string         `json:"status"`
	BudgetRuns         int            `json:"budget_runs"`
	BudgetSeconds      int            `json:"budget_seconds"`
	BudgetHMC          float64        `json:"budget_hmc,omitempty"`
	PrepaidAttestation string         `json:"prepaid_attestation,omitempty"`
	EscrowFingerprint  string         `json:"escrow_fingerprint,omitempty"`
	Config             map[string]any `json:"config"`
}

// RegisterResult captures one-time secrets returned by the coordinator.
type RegisterResult struct {
	OrdersOwnerSecret string
}

// Config from environment.
func timeoutDuration() time.Duration {
	sec := 25
	if v := strings.TrimSpace(os.Getenv("HACKME_POOL_SYNC_TIMEOUT_SEC")); v != "" {
		// Hunt ASAN harness uploads are multi-MB; allow up to 5m when operators opt in.
		if n, err := strconv.Atoi(v); err == nil && n >= 5 && n <= 300 {
			sec = n
		}
	} else if u := strings.ToLower(ResolveCoordinatorURL()); strings.Contains(u, "hackme.tech") || strings.Contains(u, "/pool/coordinator") {
		// Public coordinator POST can be slow through CDN when node is under load.
		// Origin dial (HACKME_POOL_ORIGIN_DIAL) still benefits from headroom for large harnesses.
		sec = 90
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
// Desktop/fleet nodes may prefer HACKME_POOL_DIRECT_URL when the env URL is the public CF proxy.
func ResolveCoordinatorURL() string {
	u := strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_URL")), "/")
	if u == "" {
		u = strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_URL")), "/")
	}
	if u == "" {
		return ""
	}
	if preferPoolSyncDirect() && coordLooksPublic(u) {
		if direct := strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_POOL_DIRECT_URL")), "/"); direct != "" {
			if coordHealthOK(direct) {
				return direct
			}
		}
	}
	prefer := strings.TrimSpace(os.Getenv("HACKME_POOL_SYNC_PREFER_LOOPBACK"))
	if prefer == "0" || prefer == "false" {
		return u
	}
	loop := strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_POOL_SYNC_LOOPBACK_URL")), "/")
	if loop == "" {
		loop = "http://127.0.0.1:18081"
	}
	// Substitute when configured URL is a reverse-proxy front (public CF path or local :18083 nginx).
	// Admin routes like POST /api/fuzz/pool/hunt/harness are not on the miner allowlist for :18083.
	low := strings.ToLower(u)
	proxyFront := strings.Contains(low, "hackme.tech") ||
		strings.Contains(low, "/pool/coordinator") ||
		strings.Contains(low, ":18083")
	if !proxyFront {
		return u
	}
	if coordHealthOK(loop) {
		return loop
	}
	return u
}

func coordHealthOK(base string) bool {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
	if err != nil {
		return false
	}
	res, err := netutil.HTTPClientPoolOriginDial(800 * time.Millisecond).Do(req)
	if err != nil {
		return false
	}
	res.Body.Close()
	return res.StatusCode == http.StatusOK
}

func adminToken() string {
	// Prefer scoped Orders publish token for campaign register; never use worker token.
	if t := OrdersPublishToken(); t != "" {
		return t
	}
	return CoordinatorAdminToken()
}

// OrdersPublishToken is the scoped credential for campaign register, settle-pull,
// and Hunt harness upload to the pool coordinator.
// Set via Orders installer pool.orders.token → HACKME_POOL_ORDERS_PUBLISH_TOKEN / …
func OrdersPublishToken() string {
	for _, k := range []string{
		"HACKME_COORDINATOR_ORDERS_PUBLISH_TOKEN",
		"HACKME_POOL_ORDERS_PUBLISH_TOKEN",
		"HACKME_ORDERS_PUBLISH_TOKEN",
	} {
		if t := strings.TrimSpace(os.Getenv(k)); t != "" {
			return t
		}
	}
	return ""
}

// OrdersCustomerToken prefers scoped publish, then full admin (never worker).
func OrdersCustomerToken() string {
	if t := OrdersPublishToken(); t != "" {
		return t
	}
	return CoordinatorAdminToken()
}

// CoordinatorAdminToken is the hub admin secret for privileged pool APIs (settle outbox).
// Never falls back to HACKME_POOL_COORDINATOR_TOKEN — that is worker-scoped and returns HTTP 401
// on public pool settle routes (desktop miners were spamming 401 every 15s).
func CoordinatorAdminToken() string {
	for _, k := range []string{
		"HACKME_COORDINATOR_ADMIN_TOKEN",
		"HACKME_POOL_COORDINATOR_ADMIN_TOKEN",
	} {
		if t := strings.TrimSpace(os.Getenv(k)); t != "" {
			return t
		}
	}
	return ""
}

// RegisterOnce POSTs one campaign to the coordinator pool API.
func RegisterOnce(ctx context.Context, coordURL, token string, req RegisterRequest) (latency time.Duration, result RegisterResult, err error) {
	coordURL = strings.TrimRight(strings.TrimSpace(coordURL), "/")
	if coordURL == "" {
		return 0, result, fmt.Errorf("pool sync: coordinator URL not set")
	}
	if token == "" {
		return 0, result, fmt.Errorf("pool sync: orders publish or coordinator admin token not set")
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
	// Prepaid attestation for Orders publish (mirror Dig budget_hmc_required).
	if OrdersPublishToken() != "" {
		owner := strings.TrimSpace(req.OwnerRef)
		if owner == "" {
			return 0, result, fmt.Errorf("pool sync: owner_ref required for orders publish")
		}
		if req.BudgetHMC <= 0 {
			if v, ok := req.Config["budget_hmc"].(float64); ok {
				req.BudgetHMC = v
			}
		}
		if req.BudgetHMC <= 0 {
			return 0, result, fmt.Errorf("pool sync: budget_hmc required for orders publish")
		}
		if strings.TrimSpace(req.PrepaidAttestation) == "" {
			hubSec := OrdersPrepaidHubSecret()
			escrowFP := strings.TrimSpace(req.EscrowFingerprint)
			if escrowFP == "" {
				if v, ok := req.Config["escrow_fingerprint"].(string); ok {
					escrowFP = strings.TrimSpace(v)
				}
			}
			if RequireHubPrepaidTicket() || hubSec != "" {
				if !PrepaidLocalSignAllowed() {
					return 0, result, fmt.Errorf("pool sync: prepaid_attestation required (HACKME_ORDERS_PREPAID_LOCAL_SIGN=0 — use hub admin mint; signing key stays hub-only)")
				}
				if hubSec == "" {
					return 0, result, fmt.Errorf("pool sync: HACKME_ORDERS_PREPAID_HUB_SECRET required for hub prepaid tickets")
				}
				if escrowFP == "" {
					return 0, result, fmt.Errorf("pool sync: open escrow fingerprint required before hub prepaid ticket")
				}
				exp := time.Now().Unix() + DefaultHubPrepaidTicketTTL
				req.PrepaidAttestation = SignHubPrepaidTicket(hubSec, owner, req.ID, req.BudgetHMC, req.BudgetRuns, escrowFP, exp)
				if req.PrepaidAttestation == "" {
					return 0, result, fmt.Errorf("pool sync: hub prepaid ticket sign failed")
				}
				req.EscrowFingerprint = escrowFP
			} else {
				// Lab only: publish-token self-MAC (forgeable; refused when minting hub).
				req.PrepaidAttestation = SignPrepaidAttestation(OrdersPublishToken(), owner, req.ID, req.BudgetHMC, req.BudgetRuns)
			}
		}
		if fp := strings.TrimSpace(req.EscrowFingerprint); fp != "" {
			req.Config["escrow_fingerprint"] = fp
		}
	}
	body, _ := json.Marshal(req)
	start := time.Now()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, coordURL+"/api/fuzz/pool/campaigns", bytes.NewReader(body))
	if err != nil {
		return 0, result, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Hackme-Admin-Token", token)
	if owner := strings.TrimSpace(req.OwnerRef); owner != "" {
		httpReq.Header.Set("X-Hackme-Orders-Owner-Ref", owner)
	}
	cl := netutil.HTTPClientPoolOriginDial(timeoutDuration())
	res, err := cl.Do(httpReq)
	latency = time.Since(start)
	if err != nil {
		return latency, result, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return latency, result, fmt.Errorf("pool register HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	var resp struct {
		OrdersOwnerSecret string `json:"orders_owner_secret"`
	}
	_ = json.Unmarshal(b, &resp)
	result.OrdersOwnerSecret = strings.TrimSpace(resp.OrdersOwnerSecret)
	return latency, result, nil
}

// RegisterWithRetry attempts registration with exponential backoff.
func RegisterWithRetry(ctx context.Context, req RegisterRequest) error {
	_, err := RegisterWithRetryResult(ctx, req)
	return err
}

// RegisterWithRetryResult is RegisterWithRetry plus the one-time owner secret.
func RegisterWithRetryResult(ctx context.Context, req RegisterRequest) (RegisterResult, error) {
	coordURL := ResolveCoordinatorURL()
	token := adminToken()
	attempts := maxAttempts()
	var lastErr error
	var result RegisterResult
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			RecordRetry()
			select {
			case <-ctx.Done():
				return result, ctx.Err()
			case <-time.After(retryBackoff(attempt)):
			}
		}
		RecordAttempt()
		lat, res, err := RegisterOnce(ctx, coordURL, token, req)
		if err == nil {
			RecordOK(req.ID, lat)
			return res, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		RecordFail(req.ID, 0, lastErr)
	}
	return result, lastErr
}

// AsyncEnabled returns true unless HACKME_POOL_SYNC_ASYNC=0.
func AsyncEnabled() bool {
	v := strings.TrimSpace(os.Getenv("HACKME_POOL_SYNC_ASYNC"))
	return v == "" || v == "1" || strings.EqualFold(v, "true")
}
