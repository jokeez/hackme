package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"hackme/internal/fuzzescrow"
	"hackme/internal/poolfuzz"
	"hackme/internal/poolsync"
)

// Orders publish token: campaign register + settle-pull + Hunt harness upload (with caps).
// Never elevates to cleanup/replay/cancel/admin. Env:
//
//	HACKME_COORDINATOR_ORDERS_PUBLISH_TOKEN
//
// Caps (publish path only; full admin bypasses):
//
//	HACKME_ORDERS_PUBLISH_MAX_BUDGET_RUNS     default 16384 (covers hunt_heavy 12000)
//	HACKME_ORDERS_PUBLISH_MAX_BUDGET_SECONDS  default 86400
//	HACKME_ORDERS_PUBLISH_MAX_PER_HOUR        default 10
//	HACKME_ORDERS_PUBLISH_MAX_PER_DAY         default 40
//	HACKME_ORDERS_PUBLISH_MAX_BUDGET_HMC      default 500
//
// Harness upload (POST /api/fuzz/pool/hunt/harness) shares the same hourly/daily
// publish rate limiter so customer nodes can sync custom ASAN binaries without
// the full coordinator admin token.

type publishCampaignReq struct {
	ID            string
	Status        string
	Title         string
	Description   string
	OwnerRef      string
	BudgetRuns    int
	BudgetSeconds int
	Config        map[string]any
}

type publishRateLimiter struct {
	mu   sync.Mutex
	hour map[string][]time.Time
	day  map[string][]time.Time
}

var globalPublishRate = &publishRateLimiter{
	hour: make(map[string][]time.Time),
	day:  make(map[string][]time.Time),
}

func ordersPublishTokenFromEnv() string {
	return strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_ORDERS_PUBLISH_TOKEN"))
}

func envIntDefault(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}

// envIntDefaultPositive treats 0 / negative / garbage as def (rate limits must not disable via 0).
func envIntDefaultPositive(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func publishMaxBudgetRuns() int {
	// Keep default aligned with poolsync.DefaultOrdersPublishMaxBudgetRuns (hunt_heavy=12000).
	return envIntDefault("HACKME_ORDERS_PUBLISH_MAX_BUDGET_RUNS", 16384)
}

func publishMaxBudgetSeconds() int {
	return envIntDefault("HACKME_ORDERS_PUBLISH_MAX_BUDGET_SECONDS", 86400)
}

func publishMaxPerHour() int {
	return envIntDefaultPositive("HACKME_ORDERS_PUBLISH_MAX_PER_HOUR", 10)
}

func publishMaxPerDay() int {
	return envIntDefaultPositive("HACKME_ORDERS_PUBLISH_MAX_PER_DAY", 40)
}

func publishMaxBudgetHMC() float64 {
	v := strings.TrimSpace(os.Getenv("HACKME_ORDERS_PUBLISH_MAX_BUDGET_HMC"))
	if v == "" {
		return 500
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 {
		return 500
	}
	return f
}

func publishMinBudgetHMC(cfg map[string]any) float64 {
	if cfg == nil {
		return fuzzescrow.MinCampaignBudgetHMC
	}
	if hunt := strings.ToLower(strings.TrimSpace(configString(cfg, "hunt_package"))); hunt != "" {
		return fuzzescrow.MinBudgetHMCForHuntPackage(hunt)
	}
	dig := strings.ToLower(strings.TrimSpace(configString(cfg, "dig_package")))
	return fuzzescrow.MinBudgetHMCForDigPackage(dig)
}

func publishBudgetHMCFromReq(budgetHMC float64, cfg map[string]any) float64 {
	if budgetHMC > 0 {
		return budgetHMC
	}
	if cfg == nil {
		return 0
	}
	switch v := cfg["budget_hmc"].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f
	default:
		return 0
	}
}

func newOrdersOwnerSecret() (secret, hash string, err error) {
	var b [32]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", "", err
	}
	secret = hex.EncodeToString(b[:])
	hash = poolsync.OwnerSecretHash(secret)
	return secret, hash, nil
}

func coordPublishOK(r *http.Request, publishToken string) bool {
	return publishToken != "" && coordAdminOK(r, publishToken)
}

func campaignRegisterAuthed(r *http.Request, adminToken, publishToken string, allowInsecure bool) (isAdmin, isPublish bool, ok bool) {
	if adminToken != "" && coordAdminOK(r, adminToken) {
		return true, false, true
	}
	if publishToken != "" && coordAdminOK(r, publishToken) {
		return false, true, true
	}
	if adminToken == "" && publishToken == "" && allowInsecure {
		return true, false, true // loopback insecure = admin powers
	}
	return false, false, false
}

func publishPackageAllowed(cfg map[string]any) (pkg string, ok bool) {
	dig := strings.ToLower(strings.TrimSpace(configString(cfg, "dig_package")))
	hunt := strings.ToLower(strings.TrimSpace(configString(cfg, "hunt_package")))
	switch dig {
	case "scan", "starter", "audit", "pro", "deep", "enterprise":
		return dig, true
	}
	switch hunt {
	case "hunt_lite", "lite", "hunt_standard", "standard", "hunt_heavy", "heavy":
		return hunt, true
	}
	// Allow pack-only Dig campaigns that FinalizeDig will classify.
	if pack := strings.TrimSpace(configString(cfg, "guard_pack", "guard_name")); pack != "" && hunt == "" {
		return "pack:" + pack, true
	}
	return "", false
}

func validatePublishCampaignID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("id required")
	}
	if len(id) > 128 {
		return fmt.Errorf("id too long (max 128)")
	}
	// ASCII-only: block unicode lookalikes / path smuggling via letters outside [A-Za-z0-9._-].
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' {
			continue
		}
		return fmt.Errorf("id has invalid characters")
	}
	if strings.Contains(id, "..") {
		return fmt.Errorf("id has invalid characters")
	}
	return nil
}

// publishStatusAllowed restricts lifecycle via register upsert (cannot cancel/complete via publish).
func publishStatusAllowed(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "running", "planned":
		return true
	default:
		return false
	}
}

// stripPublishEscalationConfig removes flags that would elevate beyond register+settle-pull.
// Also strips client-supplied budget_hmc / prepaid markers — handler re-stamps verified values.
func stripPublishEscalationConfig(cfg map[string]any) {
	if cfg == nil {
		return
	}
	for _, k := range []string{
		"attach_poh_order", "create_poh_order",
		"admin", "admin_token", "coordinator_admin",
		"settle_replay", "force_settle", "bypass_budget",
		"budget_hmc", "prepaid_attested", "prepaid_attestation",
		"escrow_fingerprint",
		"orders_owner_secret", "orders_owner_secret_hash",
	} {
		delete(cfg, k)
	}
	for _, k := range []string{"attach_poh_order", "create_poh_order"} {
		if v, ok := cfg[k]; ok {
			_ = v
			delete(cfg, k)
		}
	}
}

func validatePublishCampaignCaps(req publishCampaignReq) error {
	if err := validatePublishCampaignID(req.ID); err != nil {
		return err
	}
	if !publishStatusAllowed(req.Status) {
		return fmt.Errorf("publish token may only set status running|planned")
	}
	maxRuns := publishMaxBudgetRuns()
	maxSec := publishMaxBudgetSeconds()
	if req.BudgetRuns < 0 || req.BudgetRuns > maxRuns {
		return fmt.Errorf("budget_runs out of range for publish token (0..%d)", maxRuns)
	}
	if req.BudgetSeconds < 0 || req.BudgetSeconds > maxSec {
		return fmt.Errorf("budget_seconds out of range for publish token (0..%d)", maxSec)
	}
	owner := strings.TrimSpace(req.OwnerRef)
	if owner == "" {
		return fmt.Errorf("owner_ref required for publish token")
	}
	if len(owner) > 256 {
		return fmt.Errorf("owner_ref too long for publish token")
	}
	if len(req.Title) > 512 || len(req.Description) > 4096 {
		return fmt.Errorf("title/description too long for publish token")
	}
	if req.Config == nil {
		return fmt.Errorf("config required for publish token")
	}
	if _, ok := publishPackageAllowed(req.Config); !ok {
		return fmt.Errorf("publish token requires dig_package (scan|audit|deep) or hunt_package (hunt_lite|hunt_standard|hunt_heavy) or guard_pack")
	}
	return nil
}

func (p *publishRateLimiter) allow(key string, now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if key == "" {
		key = "unknown"
	}
	maxH := publishMaxPerHour()
	maxD := publishMaxPerDay()
	hourCut := now.Add(-time.Hour)
	dayCut := now.Add(-24 * time.Hour)
	p.hour[key] = filterTimes(p.hour[key], hourCut)
	p.day[key] = filterTimes(p.day[key], dayCut)
	if maxH > 0 && len(p.hour[key]) >= maxH {
		return false
	}
	if maxD > 0 && len(p.day[key]) >= maxD {
		return false
	}
	p.hour[key] = append(p.hour[key], now)
	p.day[key] = append(p.day[key], now)
	return true
}

func filterTimes(in []time.Time, after time.Time) []time.Time {
	out := in[:0]
	for _, t := range in {
		if t.After(after) {
			out = append(out, t)
		}
	}
	return out
}

func publishRateAllow(r *http.Request) bool {
	return globalPublishRate.allow(clientIPKey(r), time.Now())
}

func writePublishAuthError(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
	http.Error(w, "orders publish or admin authentication required", http.StatusUnauthorized)
}

// publishOwnerRefFromRequest reads the Orders owner claim (header preferred, query fallback).
func publishOwnerRefFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if v := strings.TrimSpace(r.Header.Get("X-Hackme-Orders-Owner-Ref")); v != "" {
		return v
	}
	return strings.TrimSpace(r.URL.Query().Get("owner_ref"))
}

// publishOwnerSecretFromRequest reads the per-publish tenant secret (header preferred).
func publishOwnerSecretFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if v := strings.TrimSpace(r.Header.Get("X-Hackme-Orders-Owner-Secret")); v != "" {
		return v
	}
	return strings.TrimSpace(r.URL.Query().Get("owner_secret"))
}

// publishOwnerAccessOK gates shared publish-token reads/ACKs to the campaign's owner_ref
// and per-publish owner secret. Legacy rows with empty owner_ref and no secret hash keep
// flag-only access (grandfathered); new campaigns always stamp both.
func publishOwnerAccessOK(r *http.Request, storedOwner, storedSecretHash string) bool {
	storedOwner = strings.TrimSpace(storedOwner)
	storedSecretHash = strings.TrimSpace(storedSecretHash)
	if storedOwner == "" && storedSecretHash == "" {
		return true
	}
	if storedOwner != "" && publishOwnerRefFromRequest(r) != storedOwner {
		return false
	}
	if storedSecretHash != "" {
		return poolsync.OwnerSecretHashEqual(publishOwnerSecretFromRequest(r), storedSecretHash)
	}
	return publishOwnerRefFromRequest(r) == storedOwner
}

// loadPublishOwnerGate reads owner_ref + secret hash for publish-token tenant checks.
func loadPublishOwnerGate(r *http.Request, pf *poolfuzz.Service, campaignID string) (ok bool, err error) {
	if pf == nil {
		return false, fmt.Errorf("no pool service")
	}
	storedOwner, err := pf.CampaignOwnerRef(r.Context(), campaignID)
	if err != nil {
		return false, err
	}
	secretHash, err := pf.CampaignOwnerSecretHash(r.Context(), campaignID)
	if err != nil {
		return false, err
	}
	return publishOwnerAccessOK(r, storedOwner, secretHash), nil
}
