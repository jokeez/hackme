package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/poolfuzz"
)

type fuzzCampaign struct {
	ID            string         `json:"id"`
	CampaignType  string         `json:"campaign_type"`
	Status        string         `json:"status"`
	Title         string         `json:"title"`
	Description   string         `json:"description"`
	OwnerRef      string         `json:"owner_ref"`
	TaskID        string         `json:"task_id"`
	TargetRef     string         `json:"target_ref"`
	BudgetRuns    int            `json:"budget_runs"`
	BudgetSeconds int            `json:"budget_seconds"`
	Config        map[string]any `json:"config"`
	Summary       map[string]any `json:"summary"`
	CreatedAt     int64          `json:"created_at"`
	StartedAt     int64          `json:"started_at"`
	CompletedAt   int64          `json:"completed_at"`
}

type fuzzFinding struct {
	ID          string         `json:"id"`
	CampaignID  string         `json:"campaign_id"`
	FindingType string         `json:"finding_type"`
	Severity    string         `json:"severity"`
	Title       string         `json:"title"`
	InputSHA256 string         `json:"input_sha256"`
	Artifact    string         `json:"artifact_path"`
	ReproCmd    string         `json:"repro_cmd"`
	Detail      map[string]any `json:"detail"`
	CreatedAt   int64          `json:"created_at"`
}

type fuzzCampaignCreateRequest struct {
	ID            string         `json:"id"`
	CampaignType  string         `json:"campaign_type"`
	Type          string         `json:"type"`
	Status        string         `json:"status"`
	Title         string         `json:"title"`
	Description   string         `json:"description"`
	OwnerRef      string         `json:"owner_ref"`
	TaskID        string         `json:"task_id"`
	TargetRef     string         `json:"target_ref"`
	BudgetRuns    int            `json:"budget_runs"`
	BudgetSeconds int            `json:"budget_seconds"`
	BudgetHMC     float64        `json:"budget_hmc"`
	Config        map[string]any `json:"config"`
}

type fuzzCampaignStatusUpdateRequest struct {
	Status  string         `json:"status"`
	Summary map[string]any `json:"summary"`
}

type fuzzCampaignRuntimeUpdateRequest struct {
	Status      string         `json:"status"`
	RunsDone    int            `json:"runs_done"`
	NewEdges    int            `json:"new_edges"`
	NewPaths    int            `json:"new_paths"`
	UniqueCrash int            `json:"unique_crashes"`
	FirstCrashS int            `json:"time_to_first_crash_sec"`
	Summary     map[string]any `json:"summary"`
}

type fuzzFindingIngestRequest struct {
	Finding  *fuzzFinding  `json:"finding"`
	Findings []fuzzFinding `json:"findings"`
}

type fuzzTopIssue struct {
	ID          string `json:"id"`
	Severity    string `json:"severity"`
	FindingType string `json:"finding_type"`
	Title       string `json:"title"`
	Impact      string `json:"impact"`
	ReproCmd    string `json:"repro_cmd"`
	Artifact    string `json:"artifact_path"`
	TriageClass string `json:"triage_class"`
	TriageNote  string `json:"triage_note"`
}

type fuzzReportAccessEvent struct {
	ID         int64  `json:"id"`
	CampaignID string `json:"campaign_id"`
	ActorType  string `json:"actor_type"`
	AccessKind string `json:"access_kind"`
	RemoteIP   string `json:"remote_ip"`
	UserAgent  string `json:"user_agent"`
	AccessedAt int64  `json:"accessed_at"`
}

type fuzzDiffItem struct {
	Key          string `json:"key"`
	FindingType  string `json:"finding_type"`
	InputSHA256  string `json:"input_sha256"`
	Title        string `json:"title"`
	BaseSeverity string `json:"base_severity,omitempty"`
	HeadSeverity string `json:"head_severity,omitempty"`
}

type fuzzCorpusRow struct {
	CampaignID   string `json:"campaign_id"`
	InputSHA256  string `json:"input_sha256"`
	FirstSeenAt  int64  `json:"first_seen_at"`
	LastSeenAt   int64  `json:"last_seen_at"`
	Hits         int    `json:"hits"`
	LastFinding  string `json:"last_finding_id"`
	ArtifactPath string `json:"artifact_path"`
}

type fuzzCorpusRetentionRequest struct {
	MaxItems int `json:"max_items"`
}

type fuzzCampaignHousekeepingRequest struct {
	MaxFindings       int `json:"max_findings"`
	MaxCorpus         int `json:"max_corpus"`
	MaxRuntimeSamples int `json:"max_runtime_samples"`
}

type fuzzRuntimeSample struct {
	SampledAt     int64  `json:"sampled_at"`
	Status        string `json:"status"`
	RunsDone      int    `json:"runs_done"`
	NewEdges      int    `json:"new_edges"`
	NewPaths      int    `json:"new_paths"`
	UniqueCrashes int    `json:"unique_crashes"`
	HeartbeatAt   int64  `json:"heartbeat_at"`
}

func allowedCampaignType(v string) bool {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "fuzz", "property", "symbolic", "hunt":
		return true
	default:
		return false
	}
}

// fuzzCampaignDeliverableURLs returns relative API paths for customer report/gate/pulse.
func fuzzCampaignDeliverableURLs(campaignID string) map[string]string {
	campaignID = strings.TrimSpace(campaignID)
	return map[string]string{
		"report_url": "/api/fuzz/campaigns/" + campaignID + "/report.html",
		"gate_url":   "/api/fuzz/campaigns/" + campaignID + "/gate?max_critical=0&max_high=0",
		"pulse_url":  "/api/fuzz/campaigns/" + campaignID + "/pulse",
	}
}

func mergeDeliverableURLs(resp map[string]any, campaignID string) {
	for k, v := range fuzzCampaignDeliverableURLs(campaignID) {
		resp[k] = v
	}
}

func allowedCampaignStatus(v string) bool {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "planned", "running", "paused", "completed", "cancelled":
		return true
	default:
		return false
	}
}

func allowedFindingSeverity(v string) bool {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "info", "low", "medium", "high", "critical":
		return true
	default:
		return false
	}
}

func normalizeFindingSeverity(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "med" {
		return "medium"
	}
	return v
}

func severityRank(v string) int {
	switch normalizeFindingSeverity(v) {
	case "critical":
		return 5
	case "high":
		return 4
	case "medium":
		return 3
	case "low":
		return 2
	default:
		return 1
	}
}

func severityImpact(v string) string {
	switch normalizeFindingSeverity(v) {
	case "critical":
		return "potential remote compromise or data integrity breach"
	case "high":
		return "high likelihood of service crash or security bypass"
	case "medium":
		return "stability or correctness risk requiring prioritized fix"
	case "low":
		return "limited impact but should be corrected for hardening"
	default:
		return "informational signal for further triage"
	}
}

func cleanFuzzID(v, fallbackPrefix string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return fallbackPrefix + "-" + time.Now().UTC().Format("20060102t150405")
	}
	b := make([]rune, 0, len(v))
	lastDash := false
	for _, r := range v {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b = append(b, r)
			lastDash = false
			continue
		}
		if !lastDash {
			b = append(b, '-')
			lastDash = true
		}
	}
	out := strings.Trim(string(b), "-")
	if out == "" {
		return fallbackPrefix + "-" + time.Now().UTC().Format("20060102t150405")
	}
	if len(out) > 120 {
		out = out[:120]
		out = strings.Trim(out, "-")
	}
	return out
}

func sqliteErrKind(err error) string {
	if err == nil {
		return ""
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "unique constraint"), strings.Contains(s, "constraint failed"):
		return "unique"
	case strings.Contains(s, "database is locked"), strings.Contains(s, "database busy"):
		return "busy"
	default:
		return "other"
	}
}

func execContextRetryBusy(ctx context.Context, db execerContext, query string, args ...any) error {
	var last error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*40) * time.Millisecond)
		}
		_, last = db.ExecContext(ctx, query, args...)
		if last == nil {
			return nil
		}
		if sqliteErrKind(last) != "busy" {
			return last
		}
	}
	return last
}

type execerContext interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func marshalMapJSON(m map[string]any) string {
	if m == nil {
		return "{}"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func parseMapJSON(raw string) map[string]any {
	out := map[string]any{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func newReportToken() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fail closed — never fall back to time-based guessable IDs.
		panic("fuzz: CSPRNG failed generating report token: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

func reportTokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func extractReportToken(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Hackme-Report-Token"))
}

func hasValidAdminAuth(r *http.Request) bool {
	expected := adminTokenFromEnv()
	if expected == "" {
		return false
	}
	got := extractAdminSecret(r)
	return secretsEqualConstantTime(got, expected)
}

func (a *app) auditFuzzReportAccess(ctx context.Context, campaignID, actorType, accessKind string, r *http.Request) {
	_, _ = a.db.ExecContext(ctx,
		`INSERT INTO fuzz_report_access_log (campaign_id, actor_type, access_kind, remote_ip, user_agent, accessed_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		strings.TrimSpace(campaignID),
		strings.TrimSpace(actorType),
		strings.TrimSpace(accessKind),
		clientIP(r),
		strings.TrimSpace(r.UserAgent()),
		time.Now().Unix(),
	)
}

func (a *app) requireFuzzReportAccess(w http.ResponseWriter, r *http.Request, campaignID, accessKind string) bool {
	if hasValidAdminAuth(r) {
		a.auditFuzzReportAccess(r.Context(), campaignID, "admin", accessKind, r)
		return true
	}
	var tokenHash string
	err := a.db.QueryRowContext(r.Context(), `SELECT report_token_hash FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&tokenHash)
	if err == sql.ErrNoRows {
		writeAPIError(w, http.StatusNotFound, "campaign_not_found", "campaign not found", nil)
		return false
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "campaign_lookup_failed", "campaign lookup failed", nil)
		return false
	}
	tokenHash = strings.TrimSpace(strings.ToLower(tokenHash))
	if tokenHash == "" {
		writeAPIError(w, http.StatusUnauthorized, "report_token_required", "report token required", nil)
		return false
	}
	got := reportTokenHash(extractReportToken(r))
	if !secretsEqualConstantTime(got, tokenHash) {
		writeAPIError(w, http.StatusUnauthorized, "report_token_invalid", "invalid report token", nil)
		return false
	}
	a.auditFuzzReportAccess(r.Context(), campaignID, "customer", accessKind, r)
	return true
}

func (a *app) handleFuzzCampaigns(w http.ResponseWriter, r *http.Request) {
	if !a.allowRate("fuzz_campaigns:"+clientIP(r), 25) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "rate limited", nil)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/fuzz/campaigns")
	path = strings.Trim(path, "/")
	if path == "" {
		switch r.Method {
		case http.MethodGet:
			a.handleFuzzCampaignsList(w, r)
		case http.MethodPost:
			a.handleFuzzCampaignCreate(w, r)
		default:
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		}
		return
	}
	parts := strings.Split(path, "/")
	campaignID := cleanFuzzID(parts[0], "campaign")
	if campaignID == "" {
		writeAPIError(w, http.StatusBadRequest, "campaign_id_required", "campaign id required", nil)
		return
	}
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		a.handleFuzzCampaignGet(w, r, campaignID)
		return
	}
	switch parts[1] {
	case "status":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		a.handleFuzzCampaignStatus(w, r, campaignID)
	case "runtime":
		if len(parts) >= 3 && parts[2] == "history" {
			if r.Method != http.MethodGet {
				writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
				return
			}
			if !a.requireFuzzReportAccess(w, r, campaignID, "runtime_history") {
				return
			}
			a.handleFuzzCampaignRuntimeHistory(w, r, campaignID)
			return
		}
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		a.handleFuzzCampaignRuntime(w, r, campaignID)
	case "findings":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		a.handleFuzzCampaignIngestFindings(w, r, campaignID)
	case "corpus":
		if len(parts) >= 3 && parts[2] == "retention" {
			if r.Method != http.MethodPost {
				writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
				return
			}
			a.handleFuzzCampaignCorpusRetention(w, r, campaignID)
			return
		}
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		if !a.requireFuzzReportAccess(w, r, campaignID, "corpus") {
			return
		}
		a.handleFuzzCampaignCorpus(w, r, campaignID)
	case "crashes":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		if !a.requireFuzzReportAccess(w, r, campaignID, "crashes") {
			return
		}
		a.handleFuzzCampaignCrashes(w, r, campaignID)
	case "housekeeping":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		a.handleFuzzCampaignHousekeeping(w, r, campaignID)
	case "report":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		kind := reportAccessKindHTML(r)
		if !a.requireFuzzReportAccess(w, r, campaignID, kind) {
			return
		}
		if kind == "report_html" {
			a.handleFuzzCampaignReportHTML(w, r, campaignID)
			return
		}
		a.handleFuzzCampaignReport(w, r, campaignID)
	case "report.html":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		if !a.requireFuzzReportAccess(w, r, campaignID, "report_html") {
			return
		}
		a.handleFuzzCampaignReportHTML(w, r, campaignID)
	case "report.csv":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		if !a.requireFuzzReportAccess(w, r, campaignID, "report_csv") {
			return
		}
		a.handleFuzzCampaignReportCSV(w, r, campaignID)
	case "gate":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		if !a.requireFuzzReportAccess(w, r, campaignID, "gate") {
			return
		}
		a.handleFuzzCampaignGate(w, r, campaignID)
	case "token":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		a.handleFuzzCampaignRotateToken(w, r, campaignID)
	case "access":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		if len(parts) >= 3 && parts[2] == "summary" {
			a.handleFuzzCampaignAccessSummary(w, r, campaignID)
			return
		}
		a.handleFuzzCampaignAccessLog(w, r, campaignID)
	case "pulse":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		if !a.requireFuzzReportAccess(w, r, campaignID, "pulse") {
			return
		}
		a.handleFuzzCampaignPulse(w, r, campaignID)
	case "diff":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		a.handleFuzzCampaignDiff(w, r, campaignID)
	case "escrow":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		a.handleFuzzEscrowGet(w, r, campaignID)
	case "proof-bundle":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		if !a.requireFuzzReportAccess(w, r, campaignID, "proof_bundle") {
			return
		}
		a.handleFuzzCampaignProofBundle(w, r, campaignID)
	case "proof":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		a.handleFuzzCampaignProof(w, r, campaignID)
	case "badge.svg":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		a.handleFuzzCampaignBadgeSVG(w, r, campaignID)
	default:
		writeAPIError(w, http.StatusNotFound, "not_found", "not found", nil)
	}
}

func (a *app) handleFuzzHousekeeping(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	if !requireAdminAuthStrict(w, r) {
		return
	}
	logAdminAction(r, "fuzz_housekeeping_post")
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	req := fuzzCampaignHousekeepingRequest{
		MaxFindings:       retentionLimitFromEnv("HACKME_FUZZ_RETENTION_FINDINGS_PER_CAMPAIGN", 5000, 100000),
		MaxCorpus:         retentionLimitFromEnv("HACKME_FUZZ_RETENTION_CORPUS_PER_CAMPAIGN", 2000, 100000),
		MaxRuntimeSamples: retentionLimitFromEnv("HACKME_FUZZ_RETENTION_RUNTIME_SAMPLES_PER_CAMPAIGN", 2000, 200000),
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.MaxFindings < 0 {
		req.MaxFindings = 0
	}
	if req.MaxFindings > 100000 {
		req.MaxFindings = 100000
	}
	if req.MaxCorpus < 0 {
		req.MaxCorpus = 0
	}
	if req.MaxCorpus > 100000 {
		req.MaxCorpus = 100000
	}
	if req.MaxRuntimeSamples < 0 {
		req.MaxRuntimeSamples = 0
	}
	if req.MaxRuntimeSamples > 200000 {
		req.MaxRuntimeSamples = 200000
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT id FROM fuzz_campaigns`)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "housekeeping_list_failed", "housekeeping list failed", nil)
		return
	}
	defer rows.Close()
	campaigns := 0
	var totalDeleted int64
	for rows.Next() {
		var campaignID string
		if err := rows.Scan(&campaignID); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "housekeeping_decode_failed", "housekeeping decode failed", nil)
			return
		}
		deleted, err := a.runCampaignRetention(r.Context(), campaignID, req.MaxFindings, req.MaxCorpus, req.MaxRuntimeSamples)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "housekeeping_failed", "housekeeping failed", nil)
			return
		}
		totalDeleted += deleted
		campaigns++
	}
	artifactRes, err := a.runFuzzArtifactCleanup(r.Context(),
		fuzzArtifactTTLSeconds(7*24*3600),
		fuzzArtifactMaxBytes(512*1024*1024))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "housekeeping_artifacts_failed", "housekeeping artifacts cleanup failed", nil)
		return
	}
	writeJSON(w, map[string]any{
		"ok":                  true,
		"campaigns":           campaigns,
		"max_findings":        req.MaxFindings,
		"max_corpus":          req.MaxCorpus,
		"max_runtime_samples": req.MaxRuntimeSamples,
		"deleted":             totalDeleted,
		"artifacts":           artifactRes,
	})
}

func (a *app) handleFuzzCampaignCreate(w http.ResponseWriter, r *http.Request) {
	if !requireFuzzCampaignCreateAuth(w, r) {
		return
	}
	if hasValidAdminAuth(r) {
		logAdminAction(r, "fuzz_campaign_create")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var req fuzzCampaignCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	ctype := strings.TrimSpace(strings.ToLower(req.CampaignType))
	if ctype == "" {
		ctype = strings.TrimSpace(strings.ToLower(req.Type))
	}
	if !allowedCampaignType(ctype) {
		writeAPIError(w, http.StatusBadRequest, "invalid_campaign_type", "campaign_type must be fuzz|property|symbolic|hunt", nil)
		return
	}
	status := strings.TrimSpace(strings.ToLower(req.Status))
	if status == "" {
		status = "planned"
	}
	if !allowedCampaignStatus(status) {
		writeAPIError(w, http.StatusBadRequest, "invalid_status", "status must be planned|running|paused|completed|cancelled", nil)
		return
	}
	id := cleanFuzzID(req.ID, "campaign")
	now := time.Now().Unix()
	reportToken := newReportToken()
	reportTokenHashHex := reportTokenHash(reportToken)
	startedAt := int64(0)
	completedAt := int64(0)
	if status == "running" {
		startedAt = now
	}
	if status == "completed" || status == "cancelled" {
		completedAt = now
	}
	cfgMap := normalizeFuzzCampaignConfig(req.Config, ctype)
	stampTargetFingerprint(cfgMap)
	cfg := marshalMapJSON(cfgMap)
	err := execContextRetryBusy(r.Context(), a.db,
		`INSERT INTO fuzz_campaigns
		 (id, campaign_type, status, title, description, owner_ref, task_id, target_ref, budget_runs, budget_seconds, config_json, summary_json, report_token_hash, report_token_issued_at, created_at, started_at, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, ctype, status, strings.TrimSpace(req.Title), strings.TrimSpace(req.Description),
		strings.TrimSpace(req.OwnerRef), strings.TrimSpace(req.TaskID), strings.TrimSpace(req.TargetRef),
		req.BudgetRuns, req.BudgetSeconds, cfg, "{}", reportTokenHashHex, now, now, startedAt, completedAt)
	if err != nil {
		switch sqliteErrKind(err) {
		case "unique":
			writeAPIError(w, http.StatusConflict, "create_failed", "campaign id already exists", nil)
		case "busy":
			writeAPIError(w, http.StatusServiceUnavailable, "database_busy", "database busy; retry shortly", map[string]any{"detail": err.Error()})
		default:
			writeAPIError(w, http.StatusInternalServerError, "create_failed", "campaign create failed", map[string]any{"detail": err.Error()})
		}
		return
	}
	if req.BudgetHMC > 0 {
		if req.BudgetRuns < 8 {
			writeAPIError(w, http.StatusBadRequest, "invalid_budget_runs", "budget_runs must be >= 8 when budget_hmc is set", nil)
			return
		}
		escrow, err := a.chain.OpenFuzzEscrow(r.Context(), id, req.BudgetHMC, req.BudgetRuns)
		if err != nil {
			_, _ = a.db.ExecContext(r.Context(), `DELETE FROM fuzz_campaigns WHERE id=?`, id)
			writeAPIError(w, http.StatusPaymentRequired, "escrow_failed", err.Error(), nil)
			return
		}
		cfgMap["budget_hmc"] = req.BudgetHMC
		cfgMap["escrow_split"] = "20_80"
		cfg = marshalMapJSON(cfgMap)
		_, _ = a.db.ExecContext(r.Context(), `UPDATE fuzz_campaigns SET config_json=? WHERE id=?`, cfg, id)
		respEscrow := escrow
		c, err := a.getFuzzCampaign(r.Context(), id)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "load_failed", "campaign created but readback failed", nil)
			return
		}
		resp := map[string]any{
			"ok":                     true,
			"campaign":               c,
			"fuzz_engine":            fuzzEngineMetaFromConfig(cfgMap),
			"customer_report_token":  reportToken,
			"customer_report_header": "X-Hackme-Report-Token",
			"escrow":                 respEscrow,
		}
		if poolDistributedCampaign(cfgMap) {
			resp["pool_distributed"] = true
			fc := fuzzAutoCampaign{ID: id, BudgetRuns: req.BudgetRuns, BudgetSeconds: req.BudgetSeconds, ConfigJSON: cfg}
			a.applyPoolSyncResponse(resp, r.Context(), fc)
		}
		writeJSON(w, resp)
		return
	}
	c, err := a.getFuzzCampaign(r.Context(), id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "load_failed", "campaign created but readback failed", nil)
		return
	}
	resp := map[string]any{
		"ok":                     true,
		"campaign":               c,
		"fuzz_engine":            fuzzEngineMetaFromConfig(cfgMap),
		"customer_report_token":  reportToken,
		"customer_report_header": "X-Hackme-Report-Token",
	}
	if poolDistributedCampaign(cfgMap) {
		resp["pool_distributed"] = true
		fc := fuzzAutoCampaign{
			ID:            id,
			BudgetRuns:    req.BudgetRuns,
			BudgetSeconds: req.BudgetSeconds,
			ConfigJSON:    cfg,
		}
		a.applyPoolSyncResponse(resp, r.Context(), fc)
	}
	writeJSON(w, resp)
}

func (a *app) handleFuzzCampaignsList(w http.ResponseWriter, r *http.Request) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	limit := 50
	if s := strings.TrimSpace(r.URL.Query().Get("limit")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT id, campaign_type, status, title, description, owner_ref, task_id, target_ref, budget_runs, budget_seconds, config_json, summary_json, created_at, started_at, completed_at
		 FROM fuzz_campaigns
		 ORDER BY created_at DESC
		 LIMIT ?`, limit)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "list_failed", "campaign list failed", nil)
		return
	}
	defer rows.Close()
	showAll := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("all"))) == "1"
	out := make([]fuzzCampaign, 0, limit)
	for rows.Next() {
		var c fuzzCampaign
		var cfg, sum string
		if err := rows.Scan(&c.ID, &c.CampaignType, &c.Status, &c.Title, &c.Description, &c.OwnerRef, &c.TaskID, &c.TargetRef, &c.BudgetRuns, &c.BudgetSeconds, &cfg, &sum, &c.CreatedAt, &c.StartedAt, &c.CompletedAt); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "list_decode_failed", "campaign list decode failed", nil)
			return
		}
		c.Config = parseMapJSON(cfg)
		c.Summary = parseMapJSON(sum)
		if !showAll && poolfuzz.IsInternalGateCampaign(c.ID, c.Title, c.OwnerRef, c.Config) {
			continue
		}
		a.enrichFuzzCampaignFromCoordinator(r.Context(), &c)
		out = append(out, c)
	}
	writeJSON(w, map[string]any{"ok": true, "campaigns": out, "count": len(out)})
}

func (a *app) enrichFuzzCampaignFromCoordinator(ctx context.Context, c *fuzzCampaign) {
	if c == nil {
		return
	}
	poolDist := false
	if v, ok := c.Config["pool_distributed"]; ok {
		switch x := v.(type) {
		case bool:
			poolDist = x
		case string:
			poolDist = strings.EqualFold(strings.TrimSpace(x), "true") || strings.TrimSpace(x) == "1"
		}
	}
	if !poolDist {
		return
	}
	rc, ok := a.fetchCoordinatorPoolCampaignProgress(ctx, c.ID)
	if !ok {
		return
	}
	if rc.RunsDone > intFromAny(c.Summary["runs_done"]) {
		c.Summary["runs_done"] = rc.RunsDone
	}
	if rc.Findings > 0 {
		c.Summary["findings"] = rc.Findings
	}
	if rc.BudgetRuns > 0 {
		c.BudgetRuns = rc.BudgetRuns
	}
	st := strings.TrimSpace(strings.ToLower(rc.Status))
	if st == "completed" || (rc.BudgetRuns > 0 && rc.RunsDone >= rc.BudgetRuns) {
		c.Status = "completed"
	} else if st != "" {
		c.Status = st
	}
}

func (a *app) getFuzzCampaign(ctx context.Context, id string) (fuzzCampaign, error) {
	var c fuzzCampaign
	var cfg, sum string
	err := a.db.QueryRowContext(ctx,
		`SELECT id, campaign_type, status, title, description, owner_ref, task_id, target_ref, budget_runs, budget_seconds, config_json, summary_json, created_at, started_at, completed_at
		 FROM fuzz_campaigns WHERE id=?`, id).
		Scan(&c.ID, &c.CampaignType, &c.Status, &c.Title, &c.Description, &c.OwnerRef, &c.TaskID, &c.TargetRef, &c.BudgetRuns, &c.BudgetSeconds, &cfg, &sum, &c.CreatedAt, &c.StartedAt, &c.CompletedAt)
	if err != nil {
		return c, err
	}
	c.Config = parseMapJSON(cfg)
	c.Summary = parseMapJSON(sum)
	return c, nil
}

func (a *app) handleFuzzCampaignGet(w http.ResponseWriter, r *http.Request, campaignID string) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	c, err := a.getFuzzCampaign(r.Context(), campaignID)
	if err == sql.ErrNoRows {
		writeAPIError(w, http.StatusNotFound, "campaign_not_found", "campaign not found", nil)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "campaign_load_failed", "campaign load failed", nil)
		return
	}
	resp := map[string]any{"ok": true, "campaign": c}
	if a.chain != nil {
		if esc, err := a.chain.GetFuzzEscrow(r.Context(), campaignID); err == nil {
			resp["escrow"] = esc
		}
	}
	writeJSON(w, resp)
}

func (a *app) handleFuzzCampaignStatus(w http.ResponseWriter, r *http.Request, campaignID string) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	logAdminAction(r, "fuzz_campaign_status_post")
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var req fuzzCampaignStatusUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	status := strings.TrimSpace(strings.ToLower(req.Status))
	if !allowedCampaignStatus(status) {
		writeAPIError(w, http.StatusBadRequest, "invalid_status", "status must be planned|running|paused|completed|cancelled", nil)
		return
	}
	now := time.Now().Unix()
	res, err := a.db.ExecContext(r.Context(),
		`UPDATE fuzz_campaigns
		 SET status=?,
		     summary_json=?,
		     started_at=CASE WHEN ?='running' AND started_at=0 THEN ? ELSE started_at END,
		     completed_at=CASE WHEN ? IN ('completed','cancelled') THEN ? ELSE completed_at END
		 WHERE id=?`,
		status, marshalMapJSON(req.Summary), status, now, status, now, campaignID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "status_update_failed", "status update failed", nil)
		return
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		writeAPIError(w, http.StatusNotFound, "campaign_not_found", "campaign not found", nil)
		return
	}
	a.tryCloseFuzzEscrowForStatus(r.Context(), campaignID, status)
	a.fuzzMarketplaceInvalidate()
	c, err := a.getFuzzCampaign(r.Context(), campaignID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "campaign_load_failed", "campaign load failed", nil)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "campaign": c})
}

func (a *app) tryCloseFuzzEscrowForStatus(ctx context.Context, campaignID, status string) {
	if a.chain == nil {
		return
	}
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "cancelled":
		_, _ = a.chain.CancelFuzzEscrow(ctx, campaignID)
	case "completed":
		// Drain run/finding settles first so Finalize does not refund unpaid work
		// and the pull consumer cannot ACK those rows as "closed" no-ops.
		a.pullFuzzSettleOutbox(ctx)
		_, _ = a.chain.FinalizeFuzzEscrow(ctx, campaignID)
	}
}

func mergeRuntimeSummary(base map[string]any, req fuzzCampaignRuntimeUpdateRequest, heartbeatAt int64) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	if req.Summary != nil {
		for k, v := range req.Summary {
			out[k] = v
		}
	}
	if req.RunsDone >= 0 {
		out["runs_done"] = req.RunsDone
	}
	if req.NewEdges >= 0 {
		out["new_edges"] = req.NewEdges
	}
	if req.NewPaths >= 0 {
		out["new_paths"] = req.NewPaths
	}
	if req.UniqueCrash >= 0 {
		out["unique_crashes"] = req.UniqueCrash
	}
	if req.FirstCrashS >= 0 {
		out["time_to_first_crash_sec"] = req.FirstCrashS
	}
	out["heartbeat_at"] = heartbeatAt
	return out
}

func (a *app) handleFuzzCampaignRuntime(w http.ResponseWriter, r *http.Request, campaignID string) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	logAdminAction(r, "fuzz_campaign_runtime_post")
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var req fuzzCampaignRuntimeUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	c, err := a.getFuzzCampaign(r.Context(), campaignID)
	if err == sql.ErrNoRows {
		writeAPIError(w, http.StatusNotFound, "campaign_not_found", "campaign not found", nil)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "campaign_load_failed", "campaign load failed", nil)
		return
	}
	now := time.Now().Unix()
	status := strings.TrimSpace(strings.ToLower(req.Status))
	if status == "" {
		status = c.Status
	}
	if !allowedCampaignStatus(status) {
		writeAPIError(w, http.StatusBadRequest, "invalid_status", "status must be planned|running|paused|completed|cancelled", nil)
		return
	}
	summary := mergeRuntimeSummary(c.Summary, req, now)
	runsDone := intFromAny(summary["runs_done"])
	// Auto-progress state machine for runner updates:
	// planned/paused + runtime update -> running
	if status == "planned" || status == "paused" {
		status = "running"
	}
	// If campaign reached run budget, auto-close as completed.
	if c.BudgetRuns > 0 && runsDone >= c.BudgetRuns && status != "cancelled" {
		status = "completed"
	}
	startedAt := c.StartedAt
	if status == "running" && startedAt == 0 {
		startedAt = now
	}
	completedAt := c.CompletedAt
	if (status == "completed" || status == "cancelled") && completedAt == 0 {
		completedAt = now
	}
	res, err := a.db.ExecContext(r.Context(),
		`UPDATE fuzz_campaigns
		 SET status=?, summary_json=?, started_at=?, completed_at=?
		 WHERE id=?`,
		status, marshalMapJSON(summary), startedAt, completedAt, campaignID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "runtime_update_failed", "runtime update failed", nil)
		return
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		writeAPIError(w, http.StatusNotFound, "campaign_not_found", "campaign not found", nil)
		return
	}
	updated, err := a.getFuzzCampaign(r.Context(), campaignID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "campaign_load_failed", "campaign load failed", nil)
		return
	}
	writeJSON(w, map[string]any{
		"ok":       true,
		"campaign": updated,
		"runtime": map[string]any{
			"heartbeat_at": now,
			"runs_done":    runsDone,
		},
	})
}

func (a *app) handleFuzzCampaignRotateToken(w http.ResponseWriter, r *http.Request, campaignID string) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	logAdminAction(r, "fuzz_campaign_token_rotate")
	token := newReportToken()
	hashHex := reportTokenHash(token)
	now := time.Now().Unix()
	res, err := a.db.ExecContext(r.Context(),
		`UPDATE fuzz_campaigns
		 SET report_token_hash=?, report_token_issued_at=?
		 WHERE id=?`,
		hashHex, now, campaignID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "token_rotate_failed", "token rotate failed", nil)
		return
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		writeAPIError(w, http.StatusNotFound, "campaign_not_found", "campaign not found", nil)
		return
	}
	writeJSON(w, map[string]any{
		"ok":                     true,
		"campaign_id":            campaignID,
		"customer_report_token":  token,
		"customer_report_header": "X-Hackme-Report-Token",
		"rotated_at_unix":        now,
	})
}

func (a *app) handleFuzzCampaignAccessLog(w http.ResponseWriter, r *http.Request, campaignID string) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	limit := 100
	if s := strings.TrimSpace(r.URL.Query().Get("limit")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 2000 {
			limit = n
		}
	}
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT id, campaign_id, actor_type, access_kind, remote_ip, user_agent, accessed_at
		 FROM fuzz_report_access_log
		 WHERE campaign_id=?
		 ORDER BY accessed_at DESC, id DESC
		 LIMIT ?`, campaignID, limit)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "access_log_query_failed", "access log query failed", nil)
		return
	}
	defer rows.Close()
	events := make([]fuzzReportAccessEvent, 0, limit)
	for rows.Next() {
		var e fuzzReportAccessEvent
		if err := rows.Scan(&e.ID, &e.CampaignID, &e.ActorType, &e.AccessKind, &e.RemoteIP, &e.UserAgent, &e.AccessedAt); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "access_log_decode_failed", "access log decode failed", nil)
			return
		}
		events = append(events, e)
	}
	writeJSON(w, map[string]any{
		"ok":          true,
		"campaign_id": campaignID,
		"count":       len(events),
		"events":      events,
	})
}

func (a *app) handleFuzzCampaignAccessSummary(w http.ResponseWriter, r *http.Request, campaignID string) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	var exists int
	if err := a.db.QueryRowContext(r.Context(), `SELECT 1 FROM fuzz_campaigns WHERE id=? LIMIT 1`, campaignID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			writeAPIError(w, http.StatusNotFound, "campaign_not_found", "campaign not found", nil)
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "campaign_lookup_failed", "campaign lookup failed", nil)
		return
	}
	now := time.Now().Unix()
	windowSec := int64(24 * 3600)
	if s := strings.TrimSpace(r.URL.Query().Get("window_sec")); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 && n <= 30*24*3600 {
			windowSec = n
		}
	}
	since := now - windowSec
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT actor_type, access_kind, COUNT(*) as cnt
		 FROM fuzz_report_access_log
		 WHERE campaign_id=? AND accessed_at>=?
		 GROUP BY actor_type, access_kind`,
		campaignID, since)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "access_summary_query_failed", "access summary query failed", nil)
		return
	}
	defer rows.Close()
	byActor := map[string]int{}
	byKind := map[string]int{}
	matrix := map[string]map[string]int{}
	total := 0
	for rows.Next() {
		var actor, kind string
		var cnt int
		if err := rows.Scan(&actor, &kind, &cnt); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "access_summary_decode_failed", "access summary decode failed", nil)
			return
		}
		actor = strings.TrimSpace(strings.ToLower(actor))
		kind = strings.TrimSpace(strings.ToLower(kind))
		if actor == "" {
			actor = "unknown"
		}
		if kind == "" {
			kind = "unknown"
		}
		byActor[actor] += cnt
		byKind[kind] += cnt
		if _, ok := matrix[actor]; !ok {
			matrix[actor] = map[string]int{}
		}
		matrix[actor][kind] += cnt
		total += cnt
	}
	var lastAccessAt int64
	_ = a.db.QueryRowContext(r.Context(),
		`SELECT COALESCE(MAX(accessed_at), 0) FROM fuzz_report_access_log WHERE campaign_id=?`, campaignID).
		Scan(&lastAccessAt)
	writeJSON(w, map[string]any{
		"ok":                true,
		"campaign_id":       campaignID,
		"window_sec":        windowSec,
		"window_since":      since,
		"window_until":      now,
		"total":             total,
		"last_access_at":    lastAccessAt,
		"by_actor":          byActor,
		"by_access_kind":    byKind,
		"actor_kind_matrix": matrix,
	})
}

func (a *app) handleFuzzCampaignCorpus(w http.ResponseWriter, r *http.Request, campaignID string) {
	limit := 100
	if s := strings.TrimSpace(r.URL.Query().Get("limit")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 2000 {
			limit = n
		}
	}
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT campaign_id, input_sha256, first_seen_at, last_seen_at, hits, last_finding_id, artifact_path
		 FROM fuzz_corpus
		 WHERE campaign_id=?
		 ORDER BY hits DESC, last_seen_at DESC
		 LIMIT ?`, campaignID, limit)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "corpus_query_failed", "corpus query failed", nil)
		return
	}
	defer rows.Close()
	out := make([]fuzzCorpusRow, 0, limit)
	for rows.Next() {
		var row fuzzCorpusRow
		if err := rows.Scan(&row.CampaignID, &row.InputSHA256, &row.FirstSeenAt, &row.LastSeenAt, &row.Hits, &row.LastFinding, &row.ArtifactPath); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "corpus_decode_failed", "corpus decode failed", nil)
			return
		}
		out = append(out, row)
	}
	writeJSON(w, map[string]any{
		"ok":          true,
		"campaign_id": campaignID,
		"count":       len(out),
		"corpus":      out,
	})
}

func (a *app) handleFuzzCampaignCrashes(w http.ResponseWriter, r *http.Request, campaignID string) {
	limit := 100
	if s := strings.TrimSpace(r.URL.Query().Get("limit")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 2000 {
			limit = n
		}
	}
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT id, campaign_id, finding_type, severity, title, input_sha256, artifact_path, repro_cmd, detail_json, created_at
		 FROM fuzz_findings
		 WHERE campaign_id=?
		   AND (finding_type LIKE '%crash%' OR severity IN ('critical','high'))
		 ORDER BY created_at DESC
		 LIMIT ?`, campaignID, limit)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "crashes_query_failed", "crashes query failed", nil)
		return
	}
	defer rows.Close()
	out := make([]fuzzFinding, 0, limit)
	for rows.Next() {
		var f fuzzFinding
		var detail string
		if err := rows.Scan(&f.ID, &f.CampaignID, &f.FindingType, &f.Severity, &f.Title, &f.InputSHA256, &f.Artifact, &f.ReproCmd, &detail, &f.CreatedAt); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "crashes_decode_failed", "crashes decode failed", nil)
			return
		}
		f.Detail = parseMapJSON(detail)
		out = append(out, f)
	}
	writeJSON(w, map[string]any{
		"ok":          true,
		"campaign_id": campaignID,
		"count":       len(out),
		"crashes":     out,
	})
}

func (a *app) handleFuzzCampaignCorpusRetention(w http.ResponseWriter, r *http.Request, campaignID string) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	logAdminAction(r, "fuzz_campaign_corpus_retention_post")
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	req := fuzzCorpusRetentionRequest{MaxItems: 2000}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.MaxItems <= 0 {
		req.MaxItems = 2000
	}
	if req.MaxItems > 100000 {
		req.MaxItems = 100000
	}
	res, err := a.db.ExecContext(r.Context(),
		`DELETE FROM fuzz_corpus
		 WHERE campaign_id=?
		   AND input_sha256 IN (
		      SELECT input_sha256
		      FROM fuzz_corpus
		      WHERE campaign_id=?
		      ORDER BY hits DESC, last_seen_at DESC
		      LIMIT -1 OFFSET ?
		   )`,
		campaignID, campaignID, req.MaxItems)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "corpus_retention_failed", "corpus retention failed", nil)
		return
	}
	deleted, _ := res.RowsAffected()
	writeJSON(w, map[string]any{
		"ok":          true,
		"campaign_id": campaignID,
		"max_items":   req.MaxItems,
		"deleted":     deleted,
	})
}

func (a *app) handleFuzzCampaignHousekeeping(w http.ResponseWriter, r *http.Request, campaignID string) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	logAdminAction(r, "fuzz_campaign_housekeeping_post")
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	req := fuzzCampaignHousekeepingRequest{
		MaxFindings:       retentionLimitFromEnv("HACKME_FUZZ_RETENTION_FINDINGS_PER_CAMPAIGN", 5000, 100000),
		MaxCorpus:         retentionLimitFromEnv("HACKME_FUZZ_RETENTION_CORPUS_PER_CAMPAIGN", 2000, 100000),
		MaxRuntimeSamples: retentionLimitFromEnv("HACKME_FUZZ_RETENTION_RUNTIME_SAMPLES_PER_CAMPAIGN", 2000, 200000),
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.MaxFindings < 0 {
		req.MaxFindings = 0
	}
	if req.MaxFindings > 100000 {
		req.MaxFindings = 100000
	}
	if req.MaxCorpus < 0 {
		req.MaxCorpus = 0
	}
	if req.MaxCorpus > 100000 {
		req.MaxCorpus = 100000
	}
	if req.MaxRuntimeSamples < 0 {
		req.MaxRuntimeSamples = 0
	}
	if req.MaxRuntimeSamples > 200000 {
		req.MaxRuntimeSamples = 200000
	}
	deleted, err := a.runCampaignRetention(r.Context(), campaignID, req.MaxFindings, req.MaxCorpus, req.MaxRuntimeSamples)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "housekeeping_failed", "housekeeping failed", nil)
		return
	}
	writeJSON(w, map[string]any{
		"ok":                  true,
		"campaign_id":         campaignID,
		"max_findings":        req.MaxFindings,
		"max_corpus":          req.MaxCorpus,
		"max_runtime_samples": req.MaxRuntimeSamples,
		"deleted":             deleted,
	})
}

func (a *app) handleFuzzCampaignRuntimeHistory(w http.ResponseWriter, r *http.Request, campaignID string) {
	limit := 200
	if s := strings.TrimSpace(r.URL.Query().Get("limit")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 5000 {
			limit = n
		}
	}
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT sampled_at, status, runs_done, new_edges, new_paths, unique_crashes, heartbeat_at
		 FROM fuzz_runtime_samples
		 WHERE campaign_id=?
		 ORDER BY sampled_at DESC
		 LIMIT ?`,
		campaignID, limit)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "runtime_history_query_failed", "runtime history query failed", nil)
		return
	}
	defer rows.Close()
	out := make([]fuzzRuntimeSample, 0, limit)
	for rows.Next() {
		var s fuzzRuntimeSample
		if err := rows.Scan(&s.SampledAt, &s.Status, &s.RunsDone, &s.NewEdges, &s.NewPaths, &s.UniqueCrashes, &s.HeartbeatAt); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "runtime_history_decode_failed", "runtime history decode failed", nil)
			return
		}
		out = append(out, s)
	}
	writeJSON(w, map[string]any{
		"ok":          true,
		"campaign_id": campaignID,
		"count":       len(out),
		"samples":     out,
	})
}

func campaignFindingKey(f fuzzFinding) string {
	return strings.TrimSpace(strings.ToLower(f.FindingType)) + "|" +
		strings.TrimSpace(strings.ToLower(f.InputSHA256)) + "|" +
		strings.TrimSpace(strings.ToLower(f.Title))
}

func (a *app) loadCampaignFindings(ctx context.Context, campaignID string) ([]fuzzFinding, error) {
	rows, err := a.db.QueryContext(ctx,
		`SELECT id, campaign_id, finding_type, severity, title, input_sha256, artifact_path, repro_cmd, detail_json, created_at
		 FROM fuzz_findings
		 WHERE campaign_id=?
		 ORDER BY created_at DESC`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]fuzzFinding, 0, 64)
	for rows.Next() {
		var f fuzzFinding
		var detail string
		if err := rows.Scan(&f.ID, &f.CampaignID, &f.FindingType, &f.Severity, &f.Title, &f.InputSHA256, &f.Artifact, &f.ReproCmd, &detail, &f.CreatedAt); err != nil {
			return nil, err
		}
		f.Detail = parseMapJSON(detail)
		out = append(out, f)
	}
	return out, nil
}

func (a *app) handleFuzzCampaignPulse(w http.ResponseWriter, r *http.Request, campaignID string) {
	c, err := a.getFuzzCampaign(r.Context(), campaignID)
	if err == sql.ErrNoRows {
		writeAPIError(w, http.StatusNotFound, "campaign_not_found", "campaign not found", nil)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "campaign_load_failed", "campaign load failed", nil)
		return
	}
	if poolDistributedCampaign(c.Config) {
		_ = a.syncPoolCampaignProgressFromCoordinator(r.Context(), campaignID)
		if c2, err2 := a.getFuzzCampaign(r.Context(), campaignID); err2 == nil {
			c = c2
		}
	}
	findings, err := a.loadCampaignFindings(r.Context(), campaignID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "findings_load_failed", "findings load failed", nil)
		return
	}
	now := time.Now().Unix()
	elapsed := int64(0)
	if c.StartedAt > 0 {
		end := now
		if c.CompletedAt > 0 {
			end = c.CompletedAt
		}
		if end > c.StartedAt {
			elapsed = end - c.StartedAt
		}
	}
	bySeverity := map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0, "info": 0}
	lastFindingAt := int64(0)
	for _, f := range findings {
		sev := normalizeFindingSeverity(f.Severity)
		if _, ok := bySeverity[sev]; !ok {
			bySeverity[sev] = 0
		}
		bySeverity[sev]++
		if f.CreatedAt > lastFindingAt {
			lastFindingAt = f.CreatedAt
		}
	}
	runsDone := intFromAny(c.Summary["runs_done"])
	if runsDone == 0 {
		runsDone = intFromAny(c.Summary["executions"])
	}
	newEdges := intFromAny(c.Summary["new_edges"])
	newPaths := intFromAny(c.Summary["new_paths"])
	uniqueCrashes := intFromAny(c.Summary["unique_crashes"])
	firstCrashSec := intFromAny(c.Summary["time_to_first_crash_sec"])
	progressPct := 0.0
	if c.BudgetRuns > 0 && runsDone > 0 {
		progressPct = (float64(runsDone) / float64(c.BudgetRuns)) * 100.0
		if progressPct > 100 {
			progressPct = 100
		}
	}
	ratePerSec := 0.0
	if elapsed > 0 && runsDone > 0 {
		ratePerSec = float64(runsDone) / float64(elapsed)
	}
	heartbeatAt := int64(intFromAny(c.Summary["heartbeat_at"]))
	if heartbeatAt <= 0 {
		heartbeatAt = c.StartedAt
	}
	staleAfterSec := int64(90)
	if s := strings.TrimSpace(r.URL.Query().Get("stale_after_sec")); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 && n <= 3600 {
			staleAfterSec = n
		}
	}
	runnerStale := false
	if c.Status == "running" && heartbeatAt > 0 && now-heartbeatAt > staleAfterSec {
		runnerStale = true
	}
	queuePending := 0
	queueLeased := 0
	queueDone := 0
	queueFailed := 0
	_ = a.db.QueryRowContext(r.Context(),
		`SELECT
		   COALESCE(SUM(CASE WHEN status='pending' THEN 1 ELSE 0 END),0),
		   COALESCE(SUM(CASE WHEN status='leased' THEN 1 ELSE 0 END),0),
		   COALESCE(SUM(CASE WHEN status='done' THEN 1 ELSE 0 END),0),
		   COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0)
		 FROM fuzz_work_items
		 WHERE campaign_id=?`, campaignID).Scan(&queuePending, &queueLeased, &queueDone, &queueFailed)
	eta := estimatePulseETA(runsDone, c.BudgetRuns, elapsed, c.BudgetSeconds, c.Status)
	crashCount := 0
	noiseCount := 0
	for _, f := range findings {
		if fuzzengine.IsCrashClass(f.FindingType) {
			crashCount++
		} else if fuzzengine.IsCoverageNoise(f.FindingType) {
			noiseCount++
		}
	}
	resp := map[string]any{
		"ok":           true,
		"campaign_id":  campaignID,
		"status":       c.Status,
		"now_unix":     now,
		"started_at":   c.StartedAt,
		"completed_at": c.CompletedAt,
		"elapsed_sec":  elapsed,
		"progress": map[string]any{
			"runs_done":       runsDone,
			"budget_runs":     c.BudgetRuns,
			"progress_pct":    progressPct,
			"runs_per_sec":    ratePerSec,
			"budget_seconds":  c.BudgetSeconds,
			"eta_sec":         eta["eta_sec"],
			"eta_sec_est":     eta["eta_sec"],
			"eta_source":      eta["eta_source"],
			"remaining_runs":  eta["remaining_runs"],
			"progress_note":   eta["progress_note"],
			"honest_progress": eta["honest_progress"],
		},
		"eta": eta,
		"runner": map[string]any{
			"heartbeat_at":    heartbeatAt,
			"stale_after_sec": staleAfterSec,
			"is_stale":        runnerStale,
			"queue": map[string]any{
				"pending": queuePending,
				"leased":  queueLeased,
				"done":    queueDone,
				"failed":  queueFailed,
			},
		},
		"coverage": map[string]any{
			"new_edges": newEdges,
			"new_paths": newPaths,
			// Honest: crash-class findings only (summary unique_crashes may count detector fails).
			"unique_crashes":          crashCount,
			"failed_checks":           uniqueCrashes,
			"time_to_first_crash_sec": firstCrashSec,
		},
		"findings": map[string]any{
			"total":                len(findings),
			"by_severity":          bySeverity,
			"last_finding_at":      lastFindingAt,
			"crash_count":          crashCount,
			"coverage_noise_count": noiseCount,
		},
		"summary": c.Summary,
	}
	if a.chain != nil {
		if esc, err := a.chain.GetFuzzEscrow(r.Context(), campaignID); err == nil {
			resp["escrow"] = esc
		}
	}
	writeJSON(w, resp)
}

func (a *app) handleFuzzCampaignDiff(w http.ResponseWriter, r *http.Request, campaignID string) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	baseID := cleanFuzzID(strings.TrimSpace(r.URL.Query().Get("base_campaign_id")), "campaign")
	if baseID == "" {
		writeAPIError(w, http.StatusBadRequest, "base_campaign_required", "base_campaign_id required", nil)
		return
	}
	if _, err := a.getFuzzCampaign(r.Context(), campaignID); err != nil {
		if err == sql.ErrNoRows {
			writeAPIError(w, http.StatusNotFound, "campaign_not_found", "campaign not found", nil)
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "campaign_load_failed", "campaign load failed", nil)
		return
	}
	if _, err := a.getFuzzCampaign(r.Context(), baseID); err != nil {
		if err == sql.ErrNoRows {
			writeAPIError(w, http.StatusNotFound, "base_campaign_not_found", "base campaign not found", nil)
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "base_campaign_load_failed", "base campaign load failed", nil)
		return
	}
	headFindings, err := a.loadCampaignFindings(r.Context(), campaignID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "head_findings_load_failed", "head findings load failed", nil)
		return
	}
	baseFindings, err := a.loadCampaignFindings(r.Context(), baseID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "base_findings_load_failed", "base findings load failed", nil)
		return
	}
	headByKey := map[string]fuzzFinding{}
	for _, f := range headFindings {
		k := campaignFindingKey(f)
		if _, ok := headByKey[k]; !ok {
			headByKey[k] = f
		}
	}
	baseByKey := map[string]fuzzFinding{}
	for _, f := range baseFindings {
		k := campaignFindingKey(f)
		if _, ok := baseByKey[k]; !ok {
			baseByKey[k] = f
		}
	}
	newItems := make([]fuzzDiffItem, 0)
	fixedItems := make([]fuzzDiffItem, 0)
	regressedItems := make([]fuzzDiffItem, 0)
	for k, hf := range headByKey {
		bf, ok := baseByKey[k]
		if !ok {
			newItems = append(newItems, fuzzDiffItem{
				Key:          k,
				FindingType:  hf.FindingType,
				InputSHA256:  hf.InputSHA256,
				Title:        hf.Title,
				HeadSeverity: hf.Severity,
			})
			continue
		}
		if severityRank(hf.Severity) > severityRank(bf.Severity) {
			regressedItems = append(regressedItems, fuzzDiffItem{
				Key:          k,
				FindingType:  hf.FindingType,
				InputSHA256:  hf.InputSHA256,
				Title:        hf.Title,
				BaseSeverity: bf.Severity,
				HeadSeverity: hf.Severity,
			})
		}
	}
	for k, bf := range baseByKey {
		if _, ok := headByKey[k]; ok {
			continue
		}
		fixedItems = append(fixedItems, fuzzDiffItem{
			Key:          k,
			FindingType:  bf.FindingType,
			InputSHA256:  bf.InputSHA256,
			Title:        bf.Title,
			BaseSeverity: bf.Severity,
		})
	}
	headCov, _ := a.campaignCoverageCounts(r.Context(), campaignID)
	baseCov, _ := a.campaignCoverageCounts(r.Context(), baseID)
	coverageDelta := map[string]any{
		"head_edges": headCov["edges"],
		"head_paths": headCov["paths"],
		"base_edges": baseCov["edges"],
		"base_paths": baseCov["paths"],
	}
	if he, ok := headCov["edges"].(int); ok {
		if be, ok2 := baseCov["edges"].(int); ok2 {
			coverageDelta["new_edges"] = he - be
		}
	}
	if hp, ok := headCov["paths"].(int); ok {
		if bp, ok2 := baseCov["paths"].(int); ok2 {
			coverageDelta["new_paths"] = hp - bp
		}
	}
	writeJSON(w, map[string]any{
		"ok":               true,
		"campaign_id":      campaignID,
		"base_campaign_id": baseID,
		"fuzz_engine":      FuzzEngineVersion,
		"summary": map[string]any{
			"new_count":       len(newItems),
			"fixed_count":     len(fixedItems),
			"regressed_count": len(regressedItems),
		},
		"coverage_delta":     coverageDelta,
		"new_findings":       newItems,
		"fixed_findings":     fixedItems,
		"regressed_findings": regressedItems,
	})
}

func (a *app) campaignCoverageCounts(ctx context.Context, campaignID string) (map[string]any, error) {
	var edges, paths int
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_coverage_seen WHERE campaign_id=? AND kind='edge'`, campaignID).Scan(&edges); err != nil {
		return nil, err
	}
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_coverage_seen WHERE campaign_id=? AND kind='path'`, campaignID).Scan(&paths); err != nil {
		return nil, err
	}
	return map[string]any{"edges": edges, "paths": paths}, nil
}

func (a *app) handleFuzzCampaignIngestFindings(w http.ResponseWriter, r *http.Request, campaignID string) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	logAdminAction(r, "fuzz_campaign_findings_post")
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var req fuzzFindingIngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	items := make([]fuzzFinding, 0, len(req.Findings)+1)
	if req.Finding != nil {
		items = append(items, *req.Finding)
	}
	items = append(items, req.Findings...)
	if len(items) == 0 {
		writeAPIError(w, http.StatusBadRequest, "finding_required", "finding or findings required", nil)
		return
	}
	if len(items) > 500 {
		writeAPIError(w, http.StatusBadRequest, "too_many_findings", "too many findings in one request", nil)
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "tx_begin_failed", "tx begin failed", nil)
		return
	}
	defer func() { _ = tx.Rollback() }()
	var exists int
	if err := tx.QueryRowContext(r.Context(), `SELECT 1 FROM fuzz_campaigns WHERE id=? LIMIT 1`, campaignID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			writeAPIError(w, http.StatusNotFound, "campaign_not_found", "campaign not found", nil)
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "campaign_lookup_failed", "campaign lookup failed", nil)
		return
	}
	now := time.Now().Unix()
	inserted := 0
	dupID := 0
	dupNatural := 0
	for _, it := range items {
		ftype := strings.TrimSpace(strings.ToLower(it.FindingType))
		if ftype == "" {
			ftype = "interesting_input"
		}
		sev := normalizeFindingSeverity(it.Severity)
		if !allowedFindingSeverity(sev) {
			sev = "info"
		}
		id := cleanFuzzID(it.ID, "finding")
		title := strings.TrimSpace(it.Title)
		inputSHA := strings.TrimSpace(strings.ToLower(it.InputSHA256))
		var existsNatural int
		if err := tx.QueryRowContext(r.Context(),
			`SELECT 1 FROM fuzz_findings
			 WHERE campaign_id=? AND finding_type=? AND input_sha256=? AND title=?
			 LIMIT 1`,
			campaignID, ftype, inputSHA, title).Scan(&existsNatural); err == nil && existsNatural == 1 {
			dupNatural++
			continue
		}
		if err := execContextRetryBusy(r.Context(), tx,
			`INSERT INTO fuzz_findings
			 (id, campaign_id, finding_type, severity, title, input_sha256, artifact_path, repro_cmd, detail_json, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, campaignID, ftype, sev, title, inputSHA,
			strings.TrimSpace(it.Artifact), strings.TrimSpace(it.ReproCmd), marshalMapJSON(it.Detail), now); err != nil {
			// Continue on duplicate IDs to keep ingestion resilient.
			if sqliteErrKind(err) == "unique" {
				dupID++
				continue
			}
			if sqliteErrKind(err) == "busy" {
				writeAPIError(w, http.StatusServiceUnavailable, "database_busy", "database busy; retry shortly", map[string]any{"detail": err.Error()})
				return
			}
			writeAPIError(w, http.StatusInternalServerError, "finding_insert_failed", "finding insert failed", map[string]any{"detail": err.Error()})
			return
		}
		if inputSHA != "" {
			_, _ = tx.ExecContext(r.Context(),
				`INSERT INTO fuzz_corpus (campaign_id, input_sha256, first_seen_at, last_seen_at, hits, last_finding_id, artifact_path)
				 VALUES (?, ?, ?, ?, 1, ?, ?)
				 ON CONFLICT(campaign_id, input_sha256) DO UPDATE SET
				   last_seen_at=excluded.last_seen_at,
				   hits=fuzz_corpus.hits+1,
				   last_finding_id=excluded.last_finding_id,
				   artifact_path=excluded.artifact_path`,
				campaignID, inputSHA, now, now, id, strings.TrimSpace(it.Artifact))
		}
		inserted++
	}
	if err := tx.Commit(); err != nil {
		if sqliteErrKind(err) == "busy" {
			writeAPIError(w, http.StatusServiceUnavailable, "database_busy", "database busy; retry shortly", map[string]any{"detail": err.Error()})
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "tx_commit_failed", "tx commit failed", map[string]any{"detail": err.Error()})
		return
	}
	// Keep campaign summary in sync with latest findings snapshot.
	summary := map[string]any{
		"findings_total":          0,
		"critical_count":          0,
		"high_count":              0,
		"medium_count":            0,
		"low_count":               0,
		"info_count":              0,
		"updated_by":              "findings_ingest",
		"updated_at":              now,
		"last_ingest_received":    len(items),
		"last_ingest_accepted":    inserted,
		"last_ingest_dup_id":      dupID,
		"last_ingest_dup_natural": dupNatural,
	}
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT severity, COUNT(*) FROM fuzz_findings WHERE campaign_id=? GROUP BY severity`, campaignID)
	if err == nil {
		defer rows.Close()
		total := 0
		for rows.Next() {
			var sev string
			var cnt int
			if err := rows.Scan(&sev, &cnt); err != nil {
				break
			}
			sev = normalizeFindingSeverity(sev)
			switch sev {
			case "critical":
				summary["critical_count"] = cnt
			case "high":
				summary["high_count"] = cnt
			case "medium":
				summary["medium_count"] = cnt
			case "low":
				summary["low_count"] = cnt
			default:
				summary["info_count"] = cnt
			}
			total += cnt
		}
		summary["findings_total"] = total
	}
	_, _ = a.db.ExecContext(r.Context(),
		`UPDATE fuzz_campaigns SET summary_json=? WHERE id=?`,
		marshalMapJSON(summary), campaignID)
	writeJSON(w, map[string]any{
		"ok":                 true,
		"campaign_id":        campaignID,
		"accepted":           inserted,
		"received":           len(items),
		"duplicates_id":      dupID,
		"duplicates_natural": dupNatural,
		"summary":            summary,
	})
}

func parseReportLimit(r *http.Request) int {
	limit := 500
	if s := strings.TrimSpace(r.URL.Query().Get("limit")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 5000 {
			limit = n
		}
	}
	return limit
}

func (a *app) buildReportBaselineDiff(ctx context.Context, campaignID string, cfg map[string]any) map[string]any {
	baseID := baselineCampaignID(cfg)
	if baseID == "" {
		return stubBaselineDiff("set config.base_campaign_id (or baseline_campaign_id) to enable baseline diff")
	}
	if baseID == campaignID {
		return stubBaselineDiff("base_campaign_id must differ from current campaign")
	}
	if _, err := a.getFuzzCampaign(ctx, baseID); err != nil {
		return stubBaselineDiff("base campaign not found: " + baseID)
	}
	headFindings, err := a.loadCampaignFindings(ctx, campaignID)
	if err != nil {
		return stubBaselineDiff("head findings load failed")
	}
	baseFindings, err := a.loadCampaignFindings(ctx, baseID)
	if err != nil {
		return stubBaselineDiff("base findings load failed")
	}
	headByKey := map[string]fuzzFinding{}
	for _, f := range headFindings {
		k := campaignFindingKey(f)
		if _, ok := headByKey[k]; !ok {
			headByKey[k] = f
		}
	}
	baseByKey := map[string]fuzzFinding{}
	for _, f := range baseFindings {
		k := campaignFindingKey(f)
		if _, ok := baseByKey[k]; !ok {
			baseByKey[k] = f
		}
	}
	newItems := make([]fuzzDiffItem, 0)
	closedItems := make([]fuzzDiffItem, 0)
	for k, hf := range headByKey {
		if _, ok := baseByKey[k]; ok {
			continue
		}
		newItems = append(newItems, fuzzDiffItem{
			Key: k, FindingType: hf.FindingType, InputSHA256: hf.InputSHA256,
			Title: hf.Title, HeadSeverity: hf.Severity,
		})
	}
	for k, bf := range baseByKey {
		if _, ok := headByKey[k]; ok {
			continue
		}
		closedItems = append(closedItems, fuzzDiffItem{
			Key: k, FindingType: bf.FindingType, InputSHA256: bf.InputSHA256,
			Title: bf.Title, BaseSeverity: bf.Severity,
		})
	}
	headCov, _ := a.campaignCoverageCounts(ctx, campaignID)
	baseCov, _ := a.campaignCoverageCounts(ctx, baseID)
	coverageDelta := map[string]any{
		"head_edges": headCov["edges"],
		"head_paths": headCov["paths"],
		"base_edges": baseCov["edges"],
		"base_paths": baseCov["paths"],
	}
	if he, ok := headCov["edges"].(int); ok {
		if be, ok2 := baseCov["edges"].(int); ok2 {
			coverageDelta["new_edges"] = he - be
		}
	}
	if hp, ok := headCov["paths"].(int); ok {
		if bp, ok2 := baseCov["paths"].(int); ok2 {
			coverageDelta["new_paths"] = hp - bp
		}
	}
	return map[string]any{
		"available":        true,
		"stub":             false,
		"base_campaign_id": baseID,
		"coverage_delta":   coverageDelta,
		"new_issues":       newItems,
		"closed_issues":    closedItems,
		"new_count":        len(newItems),
		"closed_count":     len(closedItems),
		"note":             "baseline diff vs config.base_campaign_id",
	}
}

func (a *app) buildFuzzReport(ctx context.Context, campaignID string, limit int) (map[string]any, error) {
	c, err := a.getFuzzCampaign(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	fullFindingsTotal := 0
	_ = a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, campaignID).Scan(&fullFindingsTotal)
	rows, err := a.db.QueryContext(ctx,
		`SELECT id, campaign_id, finding_type, severity, title, input_sha256, artifact_path, repro_cmd, detail_json, created_at
		 FROM fuzz_findings
		 WHERE campaign_id=?
		 ORDER BY created_at DESC
		 LIMIT ?`, campaignID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	findings := make([]fuzzFinding, 0, limit)
	bySeverity := map[string]int{"info": 0, "low": 0, "medium": 0, "high": 0, "critical": 0}
	byType := map[string]int{}
	for rows.Next() {
		var f fuzzFinding
		var detail string
		if err := rows.Scan(&f.ID, &f.CampaignID, &f.FindingType, &f.Severity, &f.Title, &f.InputSHA256, &f.Artifact, &f.ReproCmd, &detail, &f.CreatedAt); err != nil {
			return nil, err
		}
		f.Detail = parseMapJSON(detail)
		findings = append(findings, f)
		bySeverity[normalizeFindingSeverity(f.Severity)]++
		byType[f.FindingType]++
	}
	// Severity-desc sort; within same severity prefer crash-class, then newer.
	for i := 1; i < len(findings); i++ {
		cur := findings[i]
		j := i - 1
		for ; j >= 0; j-- {
			curRank := severityRank(cur.Severity)
			prevRank := severityRank(findings[j].Severity)
			if curRank > prevRank {
				findings[j+1] = findings[j]
				continue
			}
			if curRank == prevRank {
				curCrash := fuzzengine.IsCrashClass(cur.FindingType)
				prevCrash := fuzzengine.IsCrashClass(findings[j].FindingType)
				if curCrash && !prevCrash {
					findings[j+1] = findings[j]
					continue
				}
				if curCrash == prevCrash && cur.CreatedAt > findings[j].CreatedAt {
					findings[j+1] = findings[j]
					continue
				}
			}
			break
		}
		findings[j+1] = cur
	}
	displayFindings, crashUnique, crashDup := collapseCrashFindingsForReport(findings)
	topIssues, sanitizerHygiene, coverageNoise, crashCount, hygieneCount, noiseCount := partitionFindingsCrashFirst(displayFindings, fuzzTopIssueLimit, fuzzCoverageNoiseLimit)
	sanitizerSummary := buildSanitizerHygieneSummary(displayFindings)
	crashCrit, crashHigh, crashMed, crashLow, crashInfo := crashClassSeverityCounts(findings)
	crashScore := crashClassSeverityScore(crashCrit, crashHigh, crashMed, crashLow, crashInfo)

	critical := bySeverity["critical"]
	high := bySeverity["high"]
	medium := bySeverity["medium"]
	low := bySeverity["low"]
	info := bySeverity["info"]
	// Product verdict is crash-first (detector noise does not fail the customer card).
	verdict := "clean"
	if crashCrit > 0 {
		verdict = "fail_critical"
	} else if crashHigh > 0 {
		verdict = "fail_high"
	} else if crashMed > 0 {
		verdict = "warn_medium"
	} else if crashCount > 0 {
		verdict = "warn_crash"
	}
	exploitableCount := crashCrit + crashHigh
	vulnerabilitiesFound := crashCrit + crashHigh + crashMed
	recommendations := []string{}
	if exploitableCount > 0 {
		recommendations = append(recommendations, "Patch crash-class critical/high findings before release.")
		recommendations = append(recommendations, "Add deterministic repro tests for every crash-class finding ID (input → command → same crash).")
	}
	if crashCount == 0 {
		recommendations = append(recommendations, "No crash/hang/ASan/memory findings in sample; detector signals (if any) are appendix coverage noise.")
	}
	if noiseCount > 0 && crashCount == 0 && hygieneCount == 0 {
		recommendations = append(recommendations, "Review coverage-noise appendix only if hardening detector semantics; do not treat as CVE claims.")
	}
	if hygieneCount > 0 {
		recommendations = append(recommendations, "Review sanitizer hygiene appendix (UBSan/LSan subtypes) — quality signals, not bounty-eligible.")
	}
	if len(recommendations) == 0 {
		recommendations = append(recommendations, "Maintain campaign cadence and keep CI gate green before each release.")
	}
	confidence := "low"
	runsDone := intFromAny(c.Summary["runs_done"])
	if runsDone == 0 {
		runsDone = intFromAny(c.Summary["executions"])
	}
	if runsDone >= 10_000 {
		confidence = "high"
	} else if runsDone >= 500 {
		confidence = "medium"
	} else if len(findings) >= 20 {
		confidence = "medium"
	}
	edges := intFromAny(c.Summary["new_edges"])
	paths := intFromAny(c.Summary["new_paths"])
	if cov, err := a.campaignCoverageCounts(ctx, campaignID); err == nil {
		if e, ok := cov["edges"].(int); ok && e > edges {
			edges = e
		}
		if p, ok := cov["paths"].(int); ok && p > paths {
			paths = p
		}
	}
	assuranceNote := buildAssuranceNote(runsDone, crashCrit, crashHigh, "crash/hang/ASan/memory")
	humanSummary := buildHumanSummaryLine(runsDone, edges, paths, crashCount, crashCrit)
	if strings.EqualFold(strings.TrimSpace(c.CampaignType), "hunt") {
		critNote := "no ASAN crash-class"
		if crashCrit > 0 {
			critNote = fmt.Sprintf("%d critical ASAN", crashCrit)
		} else if crashCount > 0 {
			critNote = fmt.Sprintf("%d ASAN crash-class", crashCount)
		}
		humanSummary = fmt.Sprintf("%d shards verified · %s · 50/50 Hunt escrow", runsDone, critNote)
		if hygieneCount > 0 {
			humanSummary += fmt.Sprintf(" · %d sanitizer hygiene", hygieneCount)
		}
		assuranceNote = "Hunt report: pool-verified ASAN+UBSan+LSan shards on native harness. CLEAN means no qualifying native_crash in sample — not a CVE guarantee. UBSan/LSan rows are informational hygiene."
	}
	moneySpent := moneySpentFromCampaign(c)
	if a.chain != nil {
		if esc, err := a.chain.GetFuzzEscrow(ctx, campaignID); err == nil && esc != nil {
			// Prefer live escrow spend over config/summary guesses (never budget lock).
			moneySpent = moneySpentFromEscrow(esc.RunsPaidHMC, esc.BountyPaidHMC, esc.CrashBonusPaidHMC)
		}
	}
	// Default CI thresholds for embedded gate card (same as /gate defaults).
	gatePass := crashCrit <= 0 && crashHigh <= 0 && crashScore <= 0
	gateReasons := []string{"all thresholds satisfied (crash-class)"}
	if crashCrit > 0 {
		gatePass = false
		gateReasons = []string{"crash critical_count exceeds threshold"}
	} else if crashHigh > 0 {
		gatePass = false
		gateReasons = []string{"crash high_count exceeds threshold"}
	} else if crashScore > 0 {
		gatePass = false
		gateReasons = []string{"crash severity_score exceeds threshold"}
	}
	verdictCard := buildVerdictCard(runsDone, crashCount, crashCrit, gatePass, moneySpent)
	fingerprint := buildTargetFingerprint(c.Config)
	baseline := a.buildReportBaselineDiff(ctx, campaignID, c.Config)
	engineMeta := fuzzEngineMetaFromConfig(c.Config)
	sampleN := len(findings)
	groupedRowsVisible := len(topIssues) + len(sanitizerHygiene) + len(coverageNoise)
	groupedRowsHidden := (crashCount - len(topIssues)) + (hygieneCount - len(sanitizerHygiene)) + (noiseCount - len(coverageNoise))
	if groupedRowsHidden < 0 {
		groupedRowsHidden = 0
	}
	evidenceWindow := map[string]any{
		"query_limit":            limit,
		"fetched_findings":       len(findings),
		"full_campaign_findings": fullFindingsTotal,
		"history_truncated":      fullFindingsTotal > len(findings),
	}
	return map[string]any{
		"ok":                 true,
		"report_version":     "fuzz_report_v2",
		"fuzz_engine":        engineMeta,
		"generated_at_unix":  time.Now().Unix(),
		"campaign":           c,
		"evidence_window":    evidenceWindow,
		"assurance_note":     assuranceNote,
		"human_summary":      humanSummary,
		"verdict_card":       verdictCard,
		"target_fingerprint": fingerprint,
		"baseline_diff":      baseline,
		"gate": map[string]any{
			"pass":           gatePass,
			"reasons":        gateReasons,
			"assurance_note": assuranceNote,
			"primary":        true,
			"observed": map[string]any{
				"critical_count":         crashCrit,
				"high_count":             crashHigh,
				"severity_score":         crashScore,
				"crash_count":            crashCount,
				"crash_unique_count":     crashUnique,
				"crash_duplicate_count":  crashDup,
				"coverage_noise_count":   noiseCount,
				"sanitizer_hygiene_count": hygieneCount,
				"raw_findings_total":     len(findings),
				"grouped_rows_visible":   groupedRowsVisible,
				"grouped_rows_hidden":    groupedRowsHidden,
				"fetched_findings":       len(findings),
				"full_campaign_findings": fullFindingsTotal,
				"history_truncated":      fullFindingsTotal > len(findings),
				"runs_done":              runsDone,
			},
		},
		"security_summary": map[string]any{
			"vulnerabilities_found":  vulnerabilitiesFound,
			"exploitable_count":      exploitableCount,
			"critical_count":         crashCrit,
			"high_count":             crashHigh,
			"medium_count":           crashMed,
			"low_count":              crashLow,
			"info_count":             crashInfo,
			"all_critical_count":     critical,
			"all_high_count":         high,
			"all_medium_count":       medium,
			"all_low_count":          low,
			"all_info_count":         info,
			"crash_count":            crashCount,
			"crash_unique_count":     crashUnique,
			"crash_duplicate_count":  crashDup,
			"coverage_noise_count":   noiseCount,
			"sanitizer_hygiene_count": hygieneCount,
			"no_critical":            crashCrit == 0,
			"sample_size":            sampleN,
			"sample_size_unit":       "findings",
			"sample_size_basis":      "fetched_window",
			"raw_findings_total":     len(findings),
			"grouped_rows_visible":   groupedRowsVisible,
			"grouped_rows_hidden":    groupedRowsHidden,
			"fetched_findings":       len(findings),
			"full_campaign_findings": fullFindingsTotal,
			"history_truncated":      fullFindingsTotal > len(findings),
			"runs_done":              runsDone,
			"coverage_edges":         edges,
			"coverage_paths":         paths,
			"confidence":             confidence,
			"assurance_note":         assuranceNote,
			"human_summary":          humanSummary,
			"triage_policy":          "crash_first",
		},
		"verdict":         verdict,
		"top_issues":          topIssues,
		"sanitizer_hygiene":   sanitizerHygiene,
		"sanitizer_summary":   sanitizerSummary,
		"coverage_noise":      coverageNoise,
		"recommendations": recommendations,
		"totals": map[string]any{
			"findings_total":         len(findings),
			"by_severity":            bySeverity,
			"by_type":                byType,
			"severity_score":         crashScore,
			"all_severity_score":     critical*100 + high*40 + medium*10 + low*3 + info,
			"crash_count":            crashCount,
			"crash_unique_count":     crashUnique,
			"crash_duplicate_count":  crashDup,
			"coverage_noise_count":   noiseCount,
			"sanitizer_hygiene_count": hygieneCount,
			"grouped_rows_visible":   groupedRowsVisible,
			"grouped_rows_hidden":    groupedRowsHidden,
			"fetched_findings":       len(findings),
			"full_campaign_findings": fullFindingsTotal,
			"history_truncated":      fullFindingsTotal > len(findings),
			"runs_done":              runsDone,
		},
		"findings": findings,
	}, nil
}

func (a *app) handleFuzzCampaignReport(w http.ResponseWriter, r *http.Request, campaignID string) {
	report, err := a.buildFuzzReport(r.Context(), campaignID, parseReportLimit(r))
	if err == sql.ErrNoRows {
		writeAPIError(w, http.StatusNotFound, "campaign_not_found", "campaign not found", nil)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "report_build_failed", "report build failed", nil)
		return
	}
	writeJSON(w, report)
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func intFromAny(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	default:
		return 0
	}
}

func (a *app) handleFuzzCampaignReportCSV(w http.ResponseWriter, r *http.Request, campaignID string) {
	report, err := a.buildFuzzReport(r.Context(), campaignID, parseReportLimit(r))
	if err == sql.ErrNoRows {
		writeAPIError(w, http.StatusNotFound, "campaign_not_found", "campaign not found", nil)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "report_build_failed", "report build failed", nil)
		return
	}
	campaign, _ := report["campaign"].(fuzzCampaign)
	summary, _ := report["security_summary"].(map[string]any)
	totals, _ := report["totals"].(map[string]any)
	verdict := strings.TrimSpace(strings.ToLower(toString(report["verdict"])))
	if verdict == "" {
		verdict = "unknown"
	}
	topIssues := make([]fuzzProductTopIssue, 0, 5)
	if rows, ok := report["top_issues"].([]fuzzProductTopIssue); ok {
		topIssues = rows
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+campaign.ID+".fuzz_report_v2.csv\"")
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{"section", "key", "value"})
	_ = cw.Write([]string{"campaign", "id", campaign.ID})
	_ = cw.Write([]string{"campaign", "type", campaign.CampaignType})
	_ = cw.Write([]string{"campaign", "status", campaign.Status})
	_ = cw.Write([]string{"campaign", "title", campaign.Title})
	_ = cw.Write([]string{"campaign", "owner_ref", campaign.OwnerRef})
	_ = cw.Write([]string{"summary", "verdict", verdict})
	_ = cw.Write([]string{"summary", "human_summary", toString(report["human_summary"])})
	_ = cw.Write([]string{"summary", "assurance_note", toString(report["assurance_note"])})
	if gate, ok := report["gate"].(map[string]any); ok {
		_ = cw.Write([]string{"gate", "pass", toString(gate["pass"])})
		_ = cw.Write([]string{"gate", "reasons", strings.Join(toStringSlice(gate["reasons"]), "; ")})
		if observed, ok := gate["observed"].(map[string]any); ok {
			_ = cw.Write([]string{"gate_observed", "raw_findings_total", toString(observed["raw_findings_total"])})
			_ = cw.Write([]string{"gate_observed", "grouped_rows_visible", toString(observed["grouped_rows_visible"])})
			_ = cw.Write([]string{"gate_observed", "grouped_rows_hidden", toString(observed["grouped_rows_hidden"])})
			_ = cw.Write([]string{"gate_observed", "fetched_findings", toString(observed["fetched_findings"])})
			_ = cw.Write([]string{"gate_observed", "full_campaign_findings", toString(observed["full_campaign_findings"])})
			_ = cw.Write([]string{"gate_observed", "history_truncated", toString(observed["history_truncated"])})
		}
	}
	_ = cw.Write([]string{"summary", "vulnerabilities_found", toString(summary["vulnerabilities_found"])})
	_ = cw.Write([]string{"summary", "exploitable_count", toString(summary["exploitable_count"])})
	_ = cw.Write([]string{"summary", "critical_count", toString(summary["critical_count"])})
	_ = cw.Write([]string{"summary", "high_count", toString(summary["high_count"])})
	_ = cw.Write([]string{"summary", "medium_count", toString(summary["medium_count"])})
	_ = cw.Write([]string{"summary", "low_count", toString(summary["low_count"])})
	_ = cw.Write([]string{"summary", "info_count", toString(summary["info_count"])})
	_ = cw.Write([]string{"summary", "crash_count", toString(summary["crash_count"])})
	_ = cw.Write([]string{"summary", "coverage_noise_count", toString(summary["coverage_noise_count"])})
	_ = cw.Write([]string{"summary", "raw_findings_total", toString(summary["raw_findings_total"])})
	_ = cw.Write([]string{"summary", "grouped_rows_visible", toString(summary["grouped_rows_visible"])})
	_ = cw.Write([]string{"summary", "grouped_rows_hidden", toString(summary["grouped_rows_hidden"])})
	_ = cw.Write([]string{"summary", "sample_size", toString(summary["sample_size"])})
	_ = cw.Write([]string{"summary", "sample_size_basis", toString(summary["sample_size_basis"])})
	_ = cw.Write([]string{"summary", "fetched_findings", toString(summary["fetched_findings"])})
	_ = cw.Write([]string{"summary", "full_campaign_findings", toString(summary["full_campaign_findings"])})
	_ = cw.Write([]string{"summary", "history_truncated", toString(summary["history_truncated"])})
	_ = cw.Write([]string{"summary", "confidence", toString(summary["confidence"])})
	_ = cw.Write([]string{"totals", "findings_total", toString(totals["findings_total"])})
	_ = cw.Write([]string{"totals", "severity_score", toString(totals["severity_score"])})
	_ = cw.Write([]string{"totals", "grouped_rows_visible", toString(totals["grouped_rows_visible"])})
	_ = cw.Write([]string{"totals", "grouped_rows_hidden", toString(totals["grouped_rows_hidden"])})
	_ = cw.Write([]string{"totals", "fetched_findings", toString(totals["fetched_findings"])})
	_ = cw.Write([]string{"totals", "full_campaign_findings", toString(totals["full_campaign_findings"])})
	_ = cw.Write([]string{"totals", "history_truncated", toString(totals["history_truncated"])})
	if window, ok := report["evidence_window"].(map[string]any); ok {
		_ = cw.Write([]string{"evidence_window", "query_limit", toString(window["query_limit"])})
		_ = cw.Write([]string{"evidence_window", "fetched_findings", toString(window["fetched_findings"])})
		_ = cw.Write([]string{"evidence_window", "full_campaign_findings", toString(window["full_campaign_findings"])})
		_ = cw.Write([]string{"evidence_window", "history_truncated", toString(window["history_truncated"])})
	}
	if fp, ok := report["target_fingerprint"].(map[string]any); ok {
		_ = cw.Write([]string{"fingerprint", "wasm_sha256", toString(fp["wasm_sha256"])})
		_ = cw.Write([]string{"fingerprint", "binary_sha256", toString(fp["binary_sha256"])})
		_ = cw.Write([]string{"fingerprint", "source", toString(fp["source"])})
	}
	_ = cw.Write([]string{})
	_ = cw.Write([]string{"top_issues", "id", "severity", "finding_type", "title", "impact", "repro_cmd", "artifact_path", "input_sha256", "repro_ready"})
	for _, it := range topIssues {
		_ = cw.Write([]string{"top_issues", it.ID, it.Severity, it.FindingType, it.Title, it.Impact, it.ReproCmd, it.Artifact, it.InputSHA256, toString(it.Repro.Ready)})
	}
	if noise, ok := report["coverage_noise"].([]fuzzProductTopIssue); ok && len(noise) > 0 {
		_ = cw.Write([]string{})
		_ = cw.Write([]string{"coverage_noise", "id", "severity", "finding_type", "title"})
		for _, it := range noise {
			_ = cw.Write([]string{"coverage_noise", it.ID, it.Severity, it.FindingType, it.Title})
		}
	}
}

func toStringSlice(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, it := range x {
			out = append(out, toString(it))
		}
		return out
	default:
		return nil
	}
}

func (a *app) handleFuzzCampaignGate(w http.ResponseWriter, r *http.Request, campaignID string) {
	report, err := a.buildFuzzReport(r.Context(), campaignID, parseReportLimit(r))
	if err == sql.ErrNoRows {
		writeAPIError(w, http.StatusNotFound, "campaign_not_found", "campaign not found", nil)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "report_build_failed", "report build failed", nil)
		return
	}
	summary, _ := report["security_summary"].(map[string]any)
	totals, _ := report["totals"].(map[string]any)
	maxCritical := 0
	maxHigh := 0
	maxSeverityScore := 0
	// Default 0: clean campaigns (zero findings) must be able to PASS.
	// Callers that want a minimum finding corpus set ?min_sample_size=N explicitly.
	minSampleSize := 0
	minRunsDone := 0
	if s := strings.TrimSpace(r.URL.Query().Get("max_critical")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			maxCritical = n
		}
	}
	if s := strings.TrimSpace(r.URL.Query().Get("max_high")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			maxHigh = n
		}
	}
	if s := strings.TrimSpace(r.URL.Query().Get("max_severity_score")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			maxSeverityScore = n
		}
	}
	if s := strings.TrimSpace(r.URL.Query().Get("min_sample_size")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			minSampleSize = n
		}
	}
	if s := strings.TrimSpace(r.URL.Query().Get("min_runs_done")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			minRunsDone = n
		}
	}
	// Gate thresholds apply to crash-class findings only (detector → coverage noise).
	critical := intFromAny(summary["critical_count"])
	high := intFromAny(summary["high_count"])
	sampleSize := intFromAny(summary["sample_size"])
	runsDone := intFromAny(summary["runs_done"])
	if runsDone <= 0 {
		runsDone = intFromAny(totals["runs_done"])
	}
	severityScore := intFromAny(totals["severity_score"])
	crashCount := intFromAny(summary["crash_count"])
	noiseCount := intFromAny(summary["coverage_noise_count"])
	rawFindingsTotal := intFromAny(summary["raw_findings_total"])
	groupedRowsVisible := intFromAny(summary["grouped_rows_visible"])
	groupedRowsHidden := intFromAny(summary["grouped_rows_hidden"])
	fetchedFindings := intFromAny(summary["fetched_findings"])
	fullCampaignFindings := intFromAny(summary["full_campaign_findings"])
	historyTruncated := false
	if v, ok := summary["history_truncated"].(bool); ok {
		historyTruncated = v
	}
	pass := true
	reasons := make([]string, 0, 4)
	if critical > maxCritical {
		pass = false
		reasons = append(reasons, "crash critical_count exceeds threshold")
	}
	if high > maxHigh {
		pass = false
		reasons = append(reasons, "crash high_count exceeds threshold")
	}
	if severityScore > maxSeverityScore {
		pass = false
		reasons = append(reasons, "crash severity_score exceeds threshold")
	}
	if sampleSize < minSampleSize {
		pass = false
		reasons = append(reasons, "sample_size (finding count) below required minimum")
	}
	if runsDone < minRunsDone {
		pass = false
		reasons = append(reasons, "runs_done below required minimum")
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "all thresholds satisfied")
	}
	assurance, _ := report["assurance_note"].(string)
	if assurance == "" {
		assurance = "pass ≠ proven secure; gate uses crash-class counts (detector noise excluded)"
	}
	packMeta := map[string]any{}
	if camp, ok := report["campaign"].(fuzzCampaign); ok && camp.Config != nil {
		if gp := strings.TrimSpace(toString(camp.Config["guard_pack"])); gp != "" {
			packMeta["id"] = gp
			packMeta["input_mode"] = toString(camp.Config["input_mode"])
		}
	}
	samples := make([]map[string]any, 0, 5)
	if noise, ok := report["coverage_noise"].([]fuzzProductTopIssue); ok {
		for i, it := range noise {
			if i >= 5 {
				break
			}
			if strings.TrimSpace(it.Explain) == "" && strings.TrimSpace(it.GuardPack) == "" {
				continue
			}
			samples = append(samples, map[string]any{
				"title":      it.Title,
				"guard_pack": it.GuardPack,
				"explain":    it.Explain,
				"severity":   it.Severity,
			})
		}
	}
	out := map[string]any{
		"ok":             true,
		"campaign_id":    campaignID,
		"pass":           pass,
		"reasons":        reasons,
		"assurance_note": assurance,
		"triage_policy":  "crash_first",
		"thresholds": map[string]any{
			"max_critical":       maxCritical,
			"max_high":           maxHigh,
			"max_severity_score": maxSeverityScore,
			"min_sample_size":    minSampleSize,
			"min_runs_done":      minRunsDone,
			"sample_size_unit":   "findings",
			"severity_basis":     "crash_class",
		},
		"observed": map[string]any{
			"critical_count":         critical,
			"high_count":             high,
			"severity_score":         severityScore,
			"sample_size":            sampleSize,
			"runs_done":              runsDone,
			"crash_count":            crashCount,
			"coverage_noise_count":   noiseCount,
			"raw_findings_total":     rawFindingsTotal,
			"grouped_rows_visible":   groupedRowsVisible,
			"grouped_rows_hidden":    groupedRowsHidden,
			"fetched_findings":       fetchedFindings,
			"full_campaign_findings": fullCampaignFindings,
			"history_truncated":      historyTruncated,
		},
	}
	if len(packMeta) > 0 {
		out["guard_pack"] = packMeta
	}
	if len(samples) > 0 {
		out["pack_explain_samples"] = samples
	}
	writeJSON(w, out)
}
