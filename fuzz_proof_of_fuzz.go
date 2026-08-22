package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"hackme/internal/fuzzengine"
)

// Proof of Fuzz — public facts + badge (opt-in). No finding payloads / secrets.

func publicProofEnabled(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	for _, k := range []string{"public_proof", "allow_public_proof", "proof_of_fuzz_public"} {
		if v, ok := cfg[k]; ok {
			switch t := v.(type) {
			case bool:
				return t
			case string:
				s := strings.TrimSpace(strings.ToLower(t))
				return s == "1" || s == "true" || s == "yes" || s == "on"
			case float64:
				return t != 0
			}
		}
	}
	return false
}

func (a *app) allowPublicProofAccess(w http.ResponseWriter, r *http.Request, campaignID, accessKind string) (fuzzCampaign, bool) {
	c, err := a.getFuzzCampaign(r.Context(), campaignID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "campaign_not_found", "campaign not found", nil)
		return fuzzCampaign{}, false
	}
	if publicProofEnabled(c.Config) {
		a.auditFuzzReportAccess(r.Context(), campaignID, "public", accessKind, r)
		return c, true
	}
	// Private campaigns: admin or report token (same as gate).
	if !a.requireFuzzReportAccess(w, r, campaignID, accessKind) {
		return fuzzCampaign{}, false
	}
	return c, true
}

func buildProofOfFuzz(c fuzzCampaign, report map[string]any) map[string]any {
	sum, _ := report["security_summary"].(map[string]any)
	gate, _ := report["gate"].(map[string]any)
	totals, _ := report["totals"].(map[string]any)
	engine, _ := report["fuzz_engine"].(map[string]any)

	runsDone := intFromAny(sum["runs_done"])
	if runsDone <= 0 {
		runsDone = intFromAny(c.Summary["runs_done"])
	}
	crashCount := intFromAny(sum["crash_count"])
	noiseCount := intFromAny(sum["coverage_noise_count"])
	gatePass := false
	if gate != nil {
		if v, ok := gate["pass"].(bool); ok {
			gatePass = v
		}
	}
	label := "FAIL"
	if gatePass {
		label = "CLEAN"
	}

	pack := strings.TrimSpace(toString(c.Config["guard_pack"]))
	depth := strings.TrimSpace(toString(c.Config["depth_tier"]))
	inputMode := strings.TrimSpace(toString(c.Config["input_mode"]))
	if engine != nil {
		if pack == "" {
			pack = strings.TrimSpace(toString(engine["guard_pack"]))
		}
		if depth == "" {
			depth = strings.TrimSpace(toString(engine["depth_tier"]))
		}
		if inputMode == "" {
			inputMode = strings.TrimSpace(toString(engine["input_mode"]))
		}
	}

	execPerUnit := 0
	coverageKind := ""
	if engine != nil {
		execPerUnit = intFromAny(engine["exec_per_unit"])
		coverageKind = strings.TrimSpace(toString(engine["coverage_kind"]))
	}
	if execPerUnit <= 0 && c.Config != nil {
		if v := intFromAny(c.Config["exec_per_unit"]); v > 0 {
			execPerUnit = v
		}
	}
	if coverageKind == "" && c.Config != nil {
		coverageKind = strings.TrimSpace(toString(c.Config["coverage_kind"]))
	}

	wasmHash := strings.TrimSpace(toString(c.Config["wasm_sha256"]))
	if wasmHash == "" {
		wasmHash = strings.TrimSpace(toString(c.Config["artifact_hash"]))
	}
	reportHash := proofReportHash(c, runsDone, crashCount, gatePass)

	corpusItems := 0
	if totals != nil {
		corpusItems = intFromAny(totals["corpus_items"])
	}
	if corpusItems <= 0 {
		corpusItems = intFromAny(c.Summary["corpus_items"])
	}

	completed := c.CompletedAt
	if completed <= 0 {
		completed = c.StartedAt
	}
	if completed <= 0 {
		completed = c.CreatedAt
	}

	asanNote := "native/ASAN not included in this proof surface"
	var asan any = nil
	if c.Config != nil {
		if fuzzengineNativeHint(c.Config) {
			asanNote = "native_repro enabled on campaign; confirm status is in private report"
		}
	}

	isPublic := publicProofEnabled(c.Config)
	title := strings.TrimSpace(c.Title)
	if isPublic {
		// Public surface: never echo secret-shaped campaign titles.
		title = fuzzengine.RedactSensitiveString(title)
	}
	return map[string]any{
		"ok":           true,
		"proof_v":      "proof_of_fuzz_v1",
		"public":       isPublic,
		"campaign_id":  c.ID,
		"title":        title,
		"status":       c.Status,
		"completed_at": time.Unix(completed, 0).UTC().Format(time.RFC3339),
		"gate": map[string]any{
			"pass":   gatePass,
			"label":  label,
			"policy": "crash_first",
			"note":   "pass ≠ proven secure; crash-class thresholds only",
		},
		"facts": func() map[string]any {
			f := map[string]any{
				"runs_done":            runsDone,
				"budget_runs":          c.BudgetRuns,
				"pack":                 pack,
				"depth_tier":           depth,
				"input_mode":           inputMode,
				"crash_class_count":    crashCount,
				"coverage_noise_count": noiseCount,
				"asan_confirmed":       asan,
				"asan_note":            asanNote,
				"corpus_items":         corpusItems,
			}
			if execPerUnit > 0 {
				f["exec_per_unit"] = execPerUnit
			}
			if coverageKind != "" {
				f["coverage_kind"] = coverageKind
			}
			return f
		}(),
		"integrity": map[string]any{
			"proof_sha256":  reportHash,
			"wasm_sha256":   wasmHash,
			"proof_path":    "/proof/" + c.ID,
			"badge_path":    "/proof/" + c.ID + "/badge.svg",
			"gate_path":     "/api/fuzz/campaigns/" + c.ID + "/gate",
			"report_path":   "/api/fuzz/campaigns/" + c.ID + "/report.html",
			"report_access": "token_required",
		},
		"disclaimer": "Proof of Fuzz shows budgeted campaign facts only. Not a full code audit. Not a CVE warranty. Detector findings are omitted from the public page.",
	}
}

func fuzzengineNativeHint(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	for _, k := range []string{"native_repro_enabled", "native_repro"} {
		if v, ok := cfg[k]; ok {
			switch t := v.(type) {
			case bool:
				return t
			case string:
				s := strings.TrimSpace(strings.ToLower(t))
				return s == "1" || s == "true" || s == "yes"
			}
		}
	}
	return false
}

func proofReportHash(c fuzzCampaign, runs, crashes int, pass bool) string {
	raw := fmt.Sprintf("%s|%s|%d|%d|%d|%v|%s",
		c.ID, c.Status, runs, crashes, c.BudgetRuns, pass, toString(c.Config["wasm_sha256"]))
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func setProofCacheHeaders(w http.ResponseWriter, public bool) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if public {
		// Opt-in public facts only — short TTL, never for token-gated views.
		w.Header().Set("Cache-Control", "public, max-age=60")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
}

func (a *app) handleFuzzCampaignProof(w http.ResponseWriter, r *http.Request, campaignID string) {
	c, ok := a.allowPublicProofAccess(w, r, campaignID, "proof")
	if !ok {
		return
	}
	report, err := a.buildFuzzReport(r.Context(), campaignID, 50)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "report_build_failed", "report build failed", nil)
		return
	}
	proof := buildProofOfFuzz(c, report)
	isPublic := publicProofEnabled(c.Config)
	format := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("format")))
	if format == "" {
		accept := strings.ToLower(r.Header.Get("Accept"))
		if strings.Contains(accept, "text/html") && !strings.Contains(accept, "application/json") {
			format = "html"
		} else {
			format = "json"
		}
	}
	setProofCacheHeaders(w, isPublic)
	if format == "html" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(renderProofOfFuzzHTML(proof)))
		return
	}
	// Avoid writeJSON: it forces Cache-Control: no-store (wrong for opt-in public proof).
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(proof)
}

func (a *app) handleFuzzCampaignBadgeSVG(w http.ResponseWriter, r *http.Request, campaignID string) {
	c, ok := a.allowPublicProofAccess(w, r, campaignID, "proof_badge")
	if !ok {
		return
	}
	report, err := a.buildFuzzReport(r.Context(), campaignID, 20)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "report_build_failed", "report build failed", nil)
		return
	}
	proof := buildProofOfFuzz(c, report)
	setProofCacheHeaders(w, publicProofEnabled(c.Config))
	if publicProofEnabled(c.Config) {
		w.Header().Set("Cache-Control", "public, max-age=120")
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	_, _ = w.Write([]byte(renderProofOfFuzzBadgeSVG(proof)))
}

func (a *app) handleProofPretty(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	if !a.allowRate("proof:"+clientIP(r), 30) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "rate limited", nil)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/proof/")
	path = strings.Trim(path, "/")
	if path == "" {
		writeAPIError(w, http.StatusNotFound, "not_found", "proof id required", nil)
		return
	}
	parts := strings.Split(path, "/")
	campaignID := cleanFuzzID(parts[0], "campaign")
	if campaignID == "" {
		writeAPIError(w, http.StatusBadRequest, "campaign_id_required", "campaign id required", nil)
		return
	}
	if len(parts) >= 2 && (parts[1] == "badge.svg" || parts[1] == "badge") {
		a.handleFuzzCampaignBadgeSVG(w, r, campaignID)
		return
	}
	// default HTML for browsers
	if r.URL.Query().Get("format") == "" {
		q := r.URL.Query()
		q.Set("format", "html")
		r.URL.RawQuery = q.Encode()
	}
	a.handleFuzzCampaignProof(w, r, campaignID)
}

func renderProofOfFuzzBadgeSVG(proof map[string]any) string {
	gate, _ := proof["gate"].(map[string]any)
	facts, _ := proof["facts"].(map[string]any)
	pass, _ := gate["pass"].(bool)
	runs := intFromAny(facts["runs_done"])
	if pass {
		right := fmt.Sprintf("CLEAN · %d", runs)
		return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="220" height="28" viewBox="0 0 220 28" role="img" aria-label="Proof of Fuzz CLEAN">
<title>Proof of Fuzz · CLEAN · %d runs · pass ≠ proven secure</title>
<rect width="220" height="28" rx="6" fill="#0a121c" stroke="#2a6b45"/>
<rect width="108" height="28" rx="6" fill="#0e1f18"/>
<line x1="108" y1="0" x2="108" y2="28" stroke="#2a6b45"/>
<text x="54" y="18" text-anchor="middle" fill="#3dff9a" font-family="DejaVu Sans Mono,Consolas,monospace" font-size="9" font-weight="700">PROOF OF FUZZ</text>
<text x="164" y="18" text-anchor="middle" fill="#e8fff3" font-family="DejaVu Sans Mono,Consolas,monospace" font-size="10" font-weight="700">%s</text>
</svg>`, runs, html.EscapeString(right))
	}
	return `<svg xmlns="http://www.w3.org/2000/svg" width="236" height="28" viewBox="0 0 236 28" role="img" aria-label="Proof of Fuzz FAIL">
<title>Proof of Fuzz · FAIL · crash-class</title>
<rect width="236" height="28" rx="6" fill="#1a0c0c" stroke="#7a3030"/>
<rect width="108" height="28" rx="6" fill="#241010"/>
<line x1="108" y1="0" x2="108" y2="28" stroke="#7a3030"/>
<text x="54" y="18" text-anchor="middle" fill="#ff8a8a" font-family="DejaVu Sans Mono,Consolas,monospace" font-size="9" font-weight="700">PROOF OF FUZZ</text>
<text x="172" y="18" text-anchor="middle" fill="#ffe8e8" font-family="DejaVu Sans Mono,Consolas,monospace" font-size="10" font-weight="700">FAIL · CRASH</text>
</svg>`
}

func renderProofOfFuzzHTML(proof map[string]any) string {
	gate, _ := proof["gate"].(map[string]any)
	facts, _ := proof["facts"].(map[string]any)
	integ, _ := proof["integrity"].(map[string]any)
	pass, _ := gate["pass"].(bool)
	label := toString(gate["label"])
	gateColor := "#ff6060"
	if pass {
		gateColor = "#39ff14"
	}
	cid := html.EscapeString(toString(proof["campaign_id"]))
	title := html.EscapeString(toString(proof["title"]))
	disc := html.EscapeString(toString(proof["disclaimer"]))
	pub := ""
	if proof["public"] == true {
		pub = `<span style="display:inline-block;padding:.2rem .55rem;border:1px solid rgba(57,255,20,.45);color:#39ff14;border-radius:4px;font-size:.65rem;letter-spacing:.08em;text-transform:uppercase">Public</span>`
	} else {
		pub = `<span style="display:inline-block;padding:.2rem .55rem;border:1px solid rgba(255,176,32,.5);color:#ffb020;border-radius:4px;font-size:.65rem;letter-spacing:.08em;text-transform:uppercase">Token view</span>`
	}
	badge := renderProofOfFuzzBadgeSVG(proof)
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"/><meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>Proof of Fuzz · %s</title>
<style>
*{box-sizing:border-box}
body{margin:0;font-family:ui-monospace,Menlo,Consolas,monospace;background:#070b12;color:#c8d6e5;line-height:1.55}
.wrap{max-width:720px;margin:0 auto;padding:2.5rem 1.25rem 4rem}
h1{font-size:1.15rem;letter-spacing:.14em;text-transform:uppercase;color:#00d1ff;margin:0}
.sub{color:#6b7c93;font-size:.78rem;margin:.5rem 0 1.5rem}
.card{border:1px solid rgba(0,209,255,.22);border-radius:14px;background:linear-gradient(145deg,rgba(0,0,0,.55),rgba(0,209,255,.07));padding:1.25rem;margin:1rem 0}
.lbl{color:#00d1ff;font-size:.68rem;text-transform:uppercase;letter-spacing:.1em;margin:0 0 .5rem}
.gate{display:inline-block;padding:.5rem 1.2rem;border-radius:999px;border:2px solid %s;color:%s;font-weight:800;letter-spacing:.18em;font-size:1.05rem}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(140px,1fr));gap:.65rem;margin-top:1rem}
.stat{border:1px solid rgba(57,255,20,.18);border-radius:10px;padding:.65rem .8rem;background:rgba(0,0,0,.38)}
.stat b{display:block;font-size:.62rem;text-transform:uppercase;letter-spacing:.08em;color:#6b7c93;margin-bottom:.2rem}
.muted{color:#6b7c93;font-size:.78rem}
.mono{color:#39ff14;font-size:.78rem;word-break:break-all}
.warn{border-color:rgba(255,176,32,.4);background:linear-gradient(145deg,rgba(0,0,0,.5),rgba(255,176,32,.06))}
a{color:#00d1ff}
footer{margin-top:2rem;padding-top:1rem;border-top:1px solid rgba(255,255,255,.08);font-size:.72rem;color:#6b7c93}
</style></head><body><div class="wrap">
%s
<h1>Proof of Fuzz</h1>
<p class="sub">%s · %s · pass ≠ proven secure</p>
<div class="card">
<p class="lbl">Crash gate</p>
<span class="gate">%s</span>
<p class="muted" style="margin-top:.75rem">%s</p>
<div style="margin-top:1rem">%s</div>
</div>
<div class="card">
<p class="lbl">Campaign</p>
<p style="color:#fff;margin:.2rem 0">%s</p>
<p class="muted">%s</p>
<div class="grid">
<div class="stat"><b>Runs</b>%d / %d</div>
<div class="stat"><b>Pack</b>%s</div>
<div class="stat"><b>Depth</b>%s</div>
<div class="stat"><b>Input</b>%s</div>
<div class="stat"><b>Crash-class</b>%d</div>
<div class="stat"><b>Detector noise</b>%d</div>
<div class="stat"><b>Corpus</b>%d</div>
<div class="stat"><b>ASAN</b>n/a*</div>
</div>
<p class="muted" style="margin-top:.75rem">* %s</p>
</div>
<div class="card">
<p class="lbl">Integrity</p>
<p class="muted">proof_sha256</p><p class="mono">%s</p>
<p class="muted">wasm_sha256</p><p class="mono">%s</p>
<p class="muted" style="margin-top:.75rem">Private report (token): <a href="%s">report.html</a> · CI <a href="%s">gate</a> · <a href="%s?format=json">proof.json</a></p>
</div>
<div class="card warn"><p class="lbl">Honesty</p><p class="muted" style="margin:0">%s</p></div>
<footer>HackMe Network · proof_of_fuzz_v1 · badge: <span class="mono">%s</span></footer>
</div></body></html>`,
		cid,
		gateColor, gateColor,
		pub,
		cid, html.EscapeString(toString(proof["completed_at"])),
		html.EscapeString(label),
		html.EscapeString(toString(gate["note"])),
		badge,
		title,
		cid,
		intFromAny(facts["runs_done"]), intFromAny(facts["budget_runs"]),
		html.EscapeString(emptyDash(toString(facts["pack"]))),
		html.EscapeString(emptyDash(toString(facts["depth_tier"]))),
		html.EscapeString(emptyDash(toString(facts["input_mode"]))),
		intFromAny(facts["crash_class_count"]),
		intFromAny(facts["coverage_noise_count"]),
		intFromAny(facts["corpus_items"]),
		html.EscapeString(toString(facts["asan_note"])),
		html.EscapeString(toString(integ["proof_sha256"])),
		html.EscapeString(emptyDash(toString(integ["wasm_sha256"]))),
		html.EscapeString(toString(integ["report_path"])),
		html.EscapeString(toString(integ["gate_path"])),
		html.EscapeString(toString(integ["proof_path"])),
		disc,
		html.EscapeString(toString(integ["badge_path"])),
	)
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
