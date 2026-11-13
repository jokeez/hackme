package main

// Scoped CI token for hackme-fuzz-action (phase 2, see issue #86).
//
// HACKME_CI_TOKEN can do exactly one thing: create a Dig order through the
// same path as /api/security-audit (serveSecurityAudit). The server picks the
// package budget; the client only chooses a guard (dig_preset, guard_pack or
// its own wasm_check_hex) and a package from an allowlist. Creation is capped
// per hour / per day / HMC per day, counted from the campaigns table so the
// caps survive a restart. Results are read with the per-campaign report token
// returned in the response — the CI token itself reads nothing.
//
// Not reachable with this token: Hunt, settle, wasm_path / code compile,
// public proof, SUP burn, client budgets, any admin route. prepaid hubv1 +
// escrow_fingerprint still apply on the pool publish path (Orders attaches
// them); the CI token only answers "who may ask this Orders node".
//
// Env:
//
//	HACKME_CI_TOKEN            ≥32 chars, must differ from admin/developer tokens; unset = route off
//	HACKME_CI_MAX_PER_HOUR     default 4
//	HACKME_CI_MAX_PER_DAY      default 12
//	HACKME_CI_MAX_HMC_PER_DAY  default 10
//	HACKME_CI_ALLOW_DEEP       default off (deep is heavier; opt in separately)

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzingcli"
	"hackme/internal/logsafe"
)

const (
	ciOwnerPrefix    = "ci:"
	ciMinTokenLen    = 32
	ciMaxBodyBytes   = 256 << 10 // 64 KiB wasm cap → 128 KiB hex + room
	ciMaxTitleRunes  = 120
	ciDefaultPerHour = 4
	ciDefaultPerDay  = 12
	ciDefaultHMCDay  = 10.0
)

var ciLabelRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/#:@-]{0,99}$`)

// ciDigMu serialises cap check + create so two parallel jobs can't both slip
// under the cap.
var ciDigMu sync.Mutex

func ciTokenFromEnv() string { return strings.TrimSpace(os.Getenv("HACKME_CI_TOKEN")) }

// ciTokenConfig returns the configured token, "" when the route is off, or an
// error when it is set but unsafe to use.
func ciTokenConfig() (string, error) {
	tok := ciTokenFromEnv()
	if tok == "" {
		return "", nil
	}
	if len(tok) < ciMinTokenLen {
		return "", fmt.Errorf("HACKME_CI_TOKEN shorter than %d chars", ciMinTokenLen)
	}
	if adm := adminTokenFromEnv(); adm != "" && secretsEqualConstantTime(tok, adm) {
		return "", errors.New("HACKME_CI_TOKEN must differ from HACKME_ADMIN_TOKEN")
	}
	if dev := developerTokenFromEnv(); dev != "" && secretsEqualConstantTime(tok, dev) {
		return "", errors.New("HACKME_CI_TOKEN must differ from HACKME_DEVELOPER_TOKEN")
	}
	return tok, nil
}

func envIntOr(key string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key))); err == nil && n >= 0 {
		return n
	}
	return def
}

func envFloatOr(key string, def float64) float64 {
	if f, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(key)), 64); err == nil && f >= 0 {
		return f
	}
	return def
}

// ciDigRequest is the whole surface. Unknown fields are rejected.
type ciDigRequest struct {
	DigPreset    string `json:"dig_preset"`
	GuardPack    string `json:"guard_pack"`
	WasmCheckHex string `json:"wasm_check_hex"`
	DigPackage   string `json:"dig_package"`
	Title        string `json:"title"`
	Label        string `json:"label"` // e.g. "owner/repo#12" → owner_ref "ci:owner/repo#12"
}

func ciCleanTitle(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range strings.TrimSpace(s) {
		if n >= ciMaxTitleRunes {
			break
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			r = ' '
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}

// ciBuildAuditRequest maps the narrow CI request onto securityAuditRequest
// with every budget / lifecycle knob decided here, never by the client.
func ciBuildAuditRequest(in ciDigRequest, allowDeep bool) (securityAuditRequest, error) {
	preset := strings.TrimSpace(in.DigPreset)
	packName := strings.TrimSpace(in.GuardPack)
	hexStr := strings.TrimSpace(in.WasmCheckHex)
	if preset != "" && hexStr != "" {
		return securityAuditRequest{}, errors.New("dig_preset uses a shipped guard; send wasm_check_hex (+ guard_pack) instead")
	}
	if preset == "" && packName == "" && hexStr == "" {
		return securityAuditRequest{}, errors.New("dig_preset, guard_pack or wasm_check_hex required")
	}

	var pack fuzzingcli.GuardPack
	havePack := false
	defaultPkg := ""
	if preset != "" {
		p, err := fuzzingcli.DigPresetFor(preset)
		if err != nil {
			return securityAuditRequest{}, err
		}
		if packName != "" {
			gp, err := fuzzingcli.GuardPackFor(packName)
			if err != nil {
				return securityAuditRequest{}, err
			}
			if gp.ID != p.GuardPack {
				return securityAuditRequest{}, fmt.Errorf("dig_preset %q uses pack %q, not %q", p.ID, p.GuardPack, gp.ID)
			}
		}
		preset, packName, defaultPkg = p.ID, p.GuardPack, p.DigPackage
	}
	if packName != "" {
		gp, err := fuzzingcli.GuardPackFor(packName)
		if err != nil {
			return securityAuditRequest{}, err
		}
		pack, havePack, packName = gp, true, gp.ID
		if defaultPkg == "" {
			defaultPkg = gp.DefaultPackage
		}
	}

	pkgName := strings.TrimSpace(strings.ToLower(in.DigPackage))
	if pkgName == "" {
		pkgName = defaultPkg
	}
	if pkgName == "" {
		pkgName = "scan"
	}
	switch pkgName {
	case "scan", "audit":
	case "deep":
		if !allowDeep {
			return securityAuditRequest{}, errors.New("dig_package deep is not enabled for CI on this node (HACKME_CI_ALLOW_DEEP)")
		}
	default:
		return securityAuditRequest{}, fmt.Errorf("dig_package %q not allowed for CI (scan|audit)", pkgName)
	}
	pkg, err := fuzzingcli.B2BPackageFor(pkgName)
	if err != nil {
		return securityAuditRequest{}, err
	}
	if havePack {
		pkg = fuzzingcli.AdjustPackageForPack(pkg, pack)
	}

	label := strings.TrimSpace(in.Label)
	if label == "" {
		label = "ci"
	}
	if !ciLabelRE.MatchString(label) || strings.Contains(label, "..") {
		return securityAuditRequest{}, errors.New("label: use [A-Za-z0-9._/#:@-], max 100")
	}

	title := ciCleanTitle(in.Title)
	if lt := strings.ToLower(title); lt == "pool-sync-gate" || strings.Contains(lt, "pool sync") {
		// serveSecurityAudit treats these as internal pool-sync gates (auto_runner off):
		// the escrow would lock and the campaign never run.
		return securityAuditRequest{}, errors.New("title is reserved for internal pool-sync gates")
	}

	depth := pkg.DepthTier
	if havePack && pack.InputMode == "bytes" {
		depth = fuzzengine.DepthBytesCorpus
	}
	no := false
	pool := pkg.PoolDistributed
	poh := pkg.CreatePoHOrder
	return securityAuditRequest{
		Title:           title,
		PayerRef:        ciOwnerPrefix + label,
		DigPreset:       preset,
		GuardPack:       packName,
		WasmCheckHex:    hexStr,
		DigPackage:      pkg.Name,
		DepthTier:       string(depth),
		BudgetHMC:       pkg.BudgetHMC,
		BudgetRuns:      pkg.BudgetRuns,
		BudgetSeconds:   pkg.BudgetSeconds,
		MutationRounds:  pkg.MutationRounds,
		PoolDistributed: &pool,
		CreatePoHOrder:  &poh,
		PublicProof:     &no,
	}, nil
}

// ciUsage counts CI-created campaigns (owner_ref "ci:…") in the last day.
func (a *app) ciUsage(ctx context.Context, now time.Time) (hour, day int, hmcDay float64, err error) {
	rows, err := a.db.QueryContext(ctx,
		`SELECT created_at, config_json FROM fuzz_campaigns WHERE owner_ref LIKE ? ESCAPE '\' AND created_at >= ?`,
		`ci:%`, now.Add(-24*time.Hour).Unix())
	if err != nil {
		return 0, 0, 0, err
	}
	defer rows.Close()
	hourAgo := now.Add(-time.Hour).Unix()
	for rows.Next() {
		var created int64
		var cfg string
		if err := rows.Scan(&created, &cfg); err != nil {
			return 0, 0, 0, err
		}
		day++
		if created >= hourAgo {
			hour++
		}
		var m struct {
			BudgetHMC float64 `json:"budget_hmc"`
		}
		if json.Unmarshal([]byte(cfg), &m) == nil && m.BudgetHMC > 0 {
			hmcDay += m.BudgetHMC
		}
	}
	return hour, day, hmcDay, rows.Err()
}

func (a *app) handleCIDig(w http.ResponseWriter, r *http.Request) {
	tok, cfgErr := ciTokenConfig()
	if cfgErr == nil && tok == "" {
		// Route is off unless the operator opts in.
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	if !a.allowRate("ci_dig:"+clientIP(r), 2) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "rate limited", nil)
		return
	}
	if cfgErr != nil {
		log.Printf("ci_dig: refusing requests: %v", cfgErr)
		writeAPIError(w, http.StatusServiceUnavailable, "ci_token_misconfigured", "CI token is misconfigured on this node", nil)
		return
	}
	// Dedicated header only: a CI token must never be mistaken for an admin
	// Bearer, and an admin token is not accepted here either.
	if !secretsEqualConstantTime(strings.TrimSpace(r.Header.Get("X-Hackme-CI-Token")), tok) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-ci"`)
		http.Error(w, "ci authentication required", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, ciMaxBodyBytes)
	defer r.Body.Close()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body too large", nil)
		return
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var in ciDigRequest
	if err := dec.Decode(&in); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json or field not allowed for CI: "+err.Error(), nil)
		return
	}
	if dec.More() {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "trailing data after JSON object", nil)
		return
	}
	req, err := ciBuildAuditRequest(in, envBool("HACKME_CI_ALLOW_DEEP", false))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "ci_request_invalid", err.Error(), nil)
		return
	}

	ciDigMu.Lock()
	defer ciDigMu.Unlock()
	hour, day, hmcDay, err := a.ciUsage(r.Context(), time.Now())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "ci_usage_failed", "could not read CI usage", nil)
		return
	}
	maxHour := envIntOr("HACKME_CI_MAX_PER_HOUR", ciDefaultPerHour)
	maxDay := envIntOr("HACKME_CI_MAX_PER_DAY", ciDefaultPerDay)
	maxHMC := envFloatOr("HACKME_CI_MAX_HMC_PER_DAY", ciDefaultHMCDay)
	caps := map[string]any{
		"used_hour": hour, "max_per_hour": maxHour,
		"used_day": day, "max_per_day": maxDay,
		"hmc_day": hmcDay, "max_hmc_per_day": maxHMC, "requested_hmc": req.BudgetHMC,
	}
	switch {
	case hour >= maxHour:
		writeAPIError(w, http.StatusTooManyRequests, "ci_cap_exceeded", "CI hourly order cap reached", caps)
		return
	case day >= maxDay:
		writeAPIError(w, http.StatusTooManyRequests, "ci_cap_exceeded", "CI daily order cap reached", caps)
		return
	case hmcDay+req.BudgetHMC > maxHMC+1e-9:
		writeAPIError(w, http.StatusTooManyRequests, "ci_cap_exceeded", "CI daily HMC cap reached", caps)
		return
	}
	log.Printf("ci action=dig_create ip=%s owner=%s package=%s preset=%s pack=%s custom_wasm=%t",
		clientIP(r), logsafe.ID(req.PayerRef), logsafe.ID(req.DigPackage), logsafe.ID(req.DigPreset),
		logsafe.ID(req.GuardPack), req.WasmCheckHex != "")
	a.serveSecurityAudit(w, r, req)
}
