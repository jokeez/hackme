package main

import (
	"database/sql"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"hackme/internal/fuzzengine"
)

func reportAccessKindHTML(r *http.Request) string {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format"))) {
	case "json":
		return "report_json"
	case "html":
		return "report_html"
	}
	accept := strings.ToLower(r.Header.Get("Accept"))
	if strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/html") {
		return "report_json"
	}
	return "report_html"
}

func (a *app) handleFuzzCampaignReportHTML(w http.ResponseWriter, r *http.Request, campaignID string) {
	report, err := a.buildFuzzReport(r.Context(), campaignID, parseReportLimit(r))
	if err == sql.ErrNoRows {
		writeAPIError(w, http.StatusNotFound, "campaign_not_found", "campaign not found", nil)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "report_build_failed", "report build failed", nil)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(renderFuzzReportHTML(report)))
}

func renderFuzzReportHTML(report map[string]any) string {
	c := map[string]any{}
	if raw, ok := report["campaign"].(fuzzCampaign); ok {
		c["id"] = raw.ID
		c["title"] = raw.Title
		c["campaign_type"] = raw.CampaignType
		c["status"] = raw.Status
		c["task_id"] = raw.TaskID
		c["owner_ref"] = raw.OwnerRef
		c["budget_runs"] = raw.BudgetRuns
	} else if m, ok := report["campaign"].(map[string]any); ok {
		c = m
	}
	sum := map[string]any{}
	if m, ok := report["security_summary"].(map[string]any); ok {
		sum = m
	}
	gate := map[string]any{}
	if m, ok := report["gate"].(map[string]any); ok {
		gate = m
	}
	verdictCard := map[string]any{}
	if m, ok := report["verdict_card"].(map[string]any); ok {
		verdictCard = m
	}
	fp := map[string]any{}
	if m, ok := report["target_fingerprint"].(map[string]any); ok {
		fp = m
	}
	baseline := map[string]any{}
	if m, ok := report["baseline_diff"].(map[string]any); ok {
		baseline = m
	}
	window := map[string]any{}
	if m, ok := report["evidence_window"].(map[string]any); ok {
		window = m
	}

	ver := strings.ToLower(strings.TrimSpace(toString(report["verdict"])))
	if ver == "" {
		ver = "unknown"
	}
	gatePass := false
	if v, ok := gate["pass"].(bool); ok {
		gatePass = v
	}
	gateLabel := "FAIL"
	gateColor := "#ff6060"
	if gatePass {
		gateLabel = "PASS"
		gateColor = "#39ff14"
	}
	verColor := "#00d1ff"
	switch {
	case ver == "clean":
		verColor = "#39ff14"
	case strings.HasPrefix(ver, "fail"), ver == "failed":
		verColor = "#ff6060"
	case strings.HasPrefix(ver, "warn"):
		verColor = "#ffb020"
	}
	gen := "—"
	if ts := intFromAny(report["generated_at_unix"]); ts > 0 {
		gen = time.Unix(int64(ts), 0).UTC().Format("2006-01-02 15:04:05 MST")
	}
	title := toString(c["title"])
	if title == "" {
		title = toString(c["id"])
	}
	taskID := strings.TrimSpace(toString(c["task_id"]))
	taskBlock := ""
	if taskID != "" {
		taskBlock = fmt.Sprintf(`<p><span class="lbl">Linked order</span> <code>%s</code> · useful-PoW WASM guard on HackMe</p>`, html.EscapeString(taskID))
	}
	recs := []string{}
	if arr, ok := report["recommendations"].([]string); ok {
		recs = arr
	} else if arr, ok := report["recommendations"].([]any); ok {
		for _, it := range arr {
			recs = append(recs, toString(it))
		}
	}
	recLi := ""
	if len(recs) == 0 {
		recLi = `<li class="muted">No additional recommendations.</li>`
	} else {
		for _, r := range recs {
			recLi += "<li>" + html.EscapeString(r) + "</li>"
		}
	}
	issueRows := renderFuzzIssueRows(report)
	noiseRows := renderFuzzNoiseRows(report)
	reproSection := renderFuzzReproSection(report)
	engineNote := renderFuzzEngineNote(report)
	humanSummary := html.EscapeString(toString(report["human_summary"]))
	if humanSummary == "" {
		humanSummary = html.EscapeString(toString(sum["human_summary"]))
	}
	assurance := html.EscapeString(toString(report["assurance_note"]))
	gateReasons := toStringSlice(gate["reasons"])
	gateReasonHTML := ""
	for _, r := range gateReasons {
		gateReasonHTML += "<li>" + html.EscapeString(r) + "</li>"
	}
	if gateReasonHTML == "" {
		gateReasonHTML = `<li class="muted">—</li>`
	}
	verdictLines := toStringSlice(verdictCard["lines"])
	verdictHTML := ""
	for _, line := range verdictLines {
		verdictHTML += "<li>" + html.EscapeString(line) + "</li>"
	}
	if verdictHTML == "" {
		verdictHTML = fmt.Sprintf(
			`<li>Runs: %s</li><li>Crashes: %s</li><li>Critical: %s</li><li>Gate: %s</li><li>Money spent: %s HMC</li>`,
			html.EscapeString(toString(verdictCard["runs"])),
			html.EscapeString(toString(verdictCard["crashes"])),
			html.EscapeString(toString(verdictCard["critical"])),
			html.EscapeString(toString(verdictCard["gate"])),
			html.EscapeString(toString(verdictCard["money_spent_hmc"])),
		)
	}
	fpBlock := `<p class="muted">Fingerprint unavailable — no WASM/binary hash in campaign config.</p>`
	if toString(fp["available"]) == "true" || fp["available"] == true {
		parts := []string{}
		if v := toString(fp["wasm_sha256"]); v != "" {
			parts = append(parts, "WASM sha256 <code>"+html.EscapeString(v)+"</code>")
		}
		if v := toString(fp["binary_sha256"]); v != "" {
			parts = append(parts, "binary sha256 <code>"+html.EscapeString(v)+"</code>")
		}
		if v := toString(fp["source"]); v != "" {
			parts = append(parts, "source "+html.EscapeString(v))
		}
		fpBlock = `<p>` + strings.Join(parts, " · ") + `</p>`
	}
	baseBlock := `<p class="muted">` + html.EscapeString(toString(baseline["note"])) + `</p>`
	if baseline["available"] == true {
		cd, _ := baseline["coverage_delta"].(map[string]any)
		baseBlock = fmt.Sprintf(
			`<p>Base <code>%s</code> · new issues %s · closed %s</p>
<p class="muted">coverage_delta edges %s · paths %s</p>`,
			html.EscapeString(toString(baseline["base_campaign_id"])),
			html.EscapeString(toString(baseline["new_count"])),
			html.EscapeString(toString(baseline["closed_count"])),
			html.EscapeString(toString(cd["new_edges"])),
			html.EscapeString(toString(cd["new_paths"])),
		)
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>HackMe Fuzz Report · %s</title>
<style>
*{box-sizing:border-box}
body{margin:0;font-family:ui-monospace,Menlo,Consolas,monospace;background:#070b12;color:#c8d6e5;line-height:1.55}
.wrap{max-width:960px;margin:0 auto;padding:2.5rem 1.5rem 4rem}
h1{font-size:1.4rem;font-weight:700;letter-spacing:.14em;text-transform:uppercase;color:#00d1ff;margin:0}
.sub{color:#6b7c93;font-size:.78rem;margin:.5rem 0 2rem}
.badge{display:inline-block;margin-top:.35rem;padding:.45rem 1.15rem;border-radius:999px;border:2px solid %s;color:%s;font-size:1.05rem;font-weight:700;text-transform:uppercase;letter-spacing:.18em}
.gate-badge{display:inline-block;padding:.55rem 1.35rem;border-radius:999px;border:2px solid %s;color:%s;font-size:1.25rem;font-weight:800;letter-spacing:.2em}
.card{border:1px solid rgba(0,209,255,.22);border-radius:14px;background:linear-gradient(145deg,rgba(0,0,0,.55),rgba(0,209,255,.07));padding:1.35rem;margin:1.1rem 0}
.card.gate{border-color:%s;background:linear-gradient(145deg,rgba(0,0,0,.6),rgba(57,255,20,.08));box-shadow:0 0 0 1px %s22}
.card.scope{border-color:rgba(255,176,32,.35);background:linear-gradient(145deg,rgba(0,0,0,.5),rgba(255,176,32,.06))}
.card.noise{border-color:rgba(107,124,147,.35);opacity:.95}
.title{font-size:1.15rem;color:#fff;margin:.4rem 0}
.human{font-size:1.02rem;color:#e8f1ff;margin:.6rem 0 0}
.muted{color:#6b7c93;font-size:.78rem}
.lbl{color:#00d1ff;font-size:.68rem;text-transform:uppercase;letter-spacing:.1em}
code{color:#39ff14;font-size:.82rem;word-break:break-all}
pre.repro{margin:.5rem 0 0;padding:.75rem 1rem;background:rgba(0,0,0,.45);border-radius:8px;border:1px solid rgba(57,255,20,.2);overflow-x:auto;font-size:.75rem;color:#a8e6cf}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(130px,1fr));gap:.7rem;margin-top:1.1rem}
.stat{border:1px solid rgba(57,255,20,.18);border-radius:10px;padding:.7rem .9rem;background:rgba(0,0,0,.38)}
.stat b{display:block;font-size:.62rem;text-transform:uppercase;letter-spacing:.1em;color:#6b7c93;margin-bottom:.25rem}
.tag{display:inline-block;padding:.15rem .5rem;border-radius:4px;font-size:.65rem;text-transform:uppercase;letter-spacing:.05em}
.tag.expected,.tag.coverage_noise{background:rgba(0,209,255,.15);color:#00d1ff}
.tag.needs{background:rgba(255,96,96,.2);color:#ff9090}
.tag.sandbox{background:rgba(255,176,32,.15);color:#ffb020}
.tag.guard{background:rgba(57,255,20,.12);color:#7dffb8}
table{width:100%%;border-collapse:collapse;font-size:.78rem;margin-top:.65rem}
th,td{border-bottom:1px solid rgba(255,255,255,.08);padding:.5rem .55rem;text-align:left;vertical-align:top}
th{color:#6b7c93;text-transform:uppercase;font-size:.62rem;letter-spacing:.06em}
ul{margin:.4rem 0 0;padding-left:1.2rem}
ul.verdict{list-style:none;padding-left:0;font-size:.95rem;color:#fff}
ul.verdict li{padding:.2rem 0;border-bottom:1px solid rgba(255,255,255,.06)}
footer{margin-top:2.5rem;padding-top:1rem;border-top:1px solid rgba(255,255,255,.08);font-size:.72rem;color:#6b7c93}
a{color:#00d1ff}
@media print{body{background:#fff;color:#111}.card{border-color:#ccc;background:#f8f8f8}code{color:#060}}
</style>
</head>
<body>
<div class="wrap">
<h1>HackMe Security Report</h1>
<p class="sub">fuzz_report_v2 · crash-first · %s · <a href="https://hackme.tech" target="_blank" rel="noopener">hackme.tech</a></p>
%s
<div class="card gate">
<p class="lbl">CI gate (primary deliverable)</p>
<span class="gate-badge">%s</span>
<ul>%s</ul>
<p class="muted">Machine check: <code>GET …/gate</code> · pass ≠ proven secure</p>
</div>
<div class="card">
<p class="lbl">Verdict card</p>
<ul class="verdict">%s</ul>
</div>
<div class="card">
<p class="lbl">Human summary</p>
<p class="human">%s</p>
<p class="muted">%s</p>
<p class="lbl" style="margin-top:1rem">Campaign</p>
<p class="title">%s</p>
<p class="muted">%s · %s · status %s</p>
%s
<span class="badge">%s</span>
<div class="grid">
<div class="stat"><b>Runs</b>%s</div>
<div class="stat"><b>Fetched window</b>%s fetched / %s history / truncated %s</div>
<div class="stat"><b>Shown rows</b>%s raw / %s shown / %s hidden</div>
<div class="stat"><b>Crash-class</b>%s</div>
<div class="stat"><b>Crash unique</b>%s</div>
<div class="stat"><b>Crash dup</b>%s</div>
<div class="stat"><b>Critical</b>%s</div>
<div class="stat"><b>Coverage noise</b>%s</div>
<div class="stat"><b>Edges / paths</b>%s / %s</div>
<div class="stat"><b>Budget runs</b>%s</div>
</div>
</div>
<div class="card"><p class="lbl">Evidence window</p><p class="muted">This report request fetched %s findings (limit %s) versus %s total findings in campaign history. Grouped rows, shown rows, and gate/sample counters reflect the fetched evidence window, not necessarily the full history.</p></div>
<div class="card">
<p class="lbl">Top issues (crash / hang / ASan / memory only)</p>
<table><thead><tr><th>Severity</th><th>Type</th><th>Triage</th><th>Title</th><th>Repro</th></tr></thead><tbody>%s</tbody></table>
</div>
%s
<div class="card noise">
<p class="lbl">Appendix · coverage noise (detector / property)</p>
<table><thead><tr><th>Severity</th><th>Type</th><th>Title</th><th>Explain</th></tr></thead><tbody>%s</tbody></table>
</div>
<div class="card">
<p class="lbl">Target fingerprint</p>
%s
</div>
<div class="card">
<p class="lbl">Baseline diff</p>
%s
</div>
<div class="card">
<p class="lbl">Recommendations</p>
<ul>%s</ul>
</div>
<footer>HackMe Network · fuzz_report_v2 · JSON: <code>?format=json</code> · CSV: <code>report.csv</code> · Gate: <code>…/gate</code> · Generated %s</footer>
</div>
</body>
</html>`,
		html.EscapeString(title),
		verColor, verColor,
		gateColor, gateColor,
		gateColor, gateColor,
		html.EscapeString(gen),
		engineNote,
		html.EscapeString(gateLabel),
		gateReasonHTML,
		verdictHTML,
		humanSummary,
		assurance,
		html.EscapeString(title),
		html.EscapeString(toString(c["id"])),
		html.EscapeString(toString(c["campaign_type"])),
		html.EscapeString(toString(c["status"])),
		taskBlock,
		html.EscapeString(ver),
		html.EscapeString(toString(sum["runs_done"])),
		html.EscapeString(toString(window["fetched_findings"])),
		html.EscapeString(toString(window["full_campaign_findings"])),
		html.EscapeString(toString(window["history_truncated"])),
		html.EscapeString(toString(sum["raw_findings_total"])),
		html.EscapeString(toString(sum["grouped_rows_visible"])),
		html.EscapeString(toString(sum["grouped_rows_hidden"])),
		html.EscapeString(toString(sum["crash_count"])),
		html.EscapeString(toString(sum["crash_unique_count"])),
		html.EscapeString(toString(sum["crash_duplicate_count"])),
		html.EscapeString(toString(sum["critical_count"])),
		html.EscapeString(toString(sum["coverage_noise_count"])),
		html.EscapeString(toString(sum["coverage_edges"])),
		html.EscapeString(toString(sum["coverage_paths"])),
		html.EscapeString(toString(c["budget_runs"])),
		html.EscapeString(toString(window["fetched_findings"])),
		html.EscapeString(toString(window["query_limit"])),
		html.EscapeString(toString(window["full_campaign_findings"])),
		issueRows,
		reproSection,
		noiseRows,
		fpBlock,
		baseBlock,
		recLi,
		html.EscapeString(gen),
	)
}

func productTopIssues(report map[string]any) []fuzzProductTopIssue {
	if arr, ok := report["top_issues"].([]fuzzProductTopIssue); ok {
		return arr
	}
	return nil
}

func renderFuzzIssueRows(report map[string]any) string {
	arr := productTopIssues(report)
	if len(arr) == 0 {
		return `<tr><td colspan="5" class="muted">No crash-class findings (crash/hang/ASan/memory). Detector signals are in the coverage-noise appendix.</td></tr>`
	}
	var b strings.Builder
	for _, i := range arr {
		tagClass := "review"
		switch i.TriageClass {
		case "expected_signal", "coverage_noise":
			tagClass = "expected"
		case "needs_triage":
			tagClass = "needs"
		case "sandbox":
			tagClass = "sandbox"
		case "guard_signal":
			tagClass = "guard"
		}
		reproCell := `<span class="muted">—</span>`
		if strings.TrimSpace(i.Repro.Command) != "" {
			reproCell = fmt.Sprintf(`<code>%s</code>`, html.EscapeString(i.Repro.Command))
			if !i.Repro.Ready {
				reproCell += `<br/><span class="muted">` + html.EscapeString(i.Repro.Gap) + `</span>`
			}
		} else if strings.TrimSpace(i.ReproCmd) != "" {
			reproCell = fmt.Sprintf(`<code>%s</code>`, html.EscapeString(i.ReproCmd))
		}
		note := i.TriageNote
		if note == "" {
			note = i.TriageClass
		}
		b.WriteString(fmt.Sprintf(
			`<tr><td>%s</td><td>%s</td><td><span class="tag %s">%s</span><br/><span class="muted">%s</span></td><td>%s</td><td>%s</td></tr>`,
			html.EscapeString(i.Severity),
			html.EscapeString(i.FindingType),
			tagClass,
			html.EscapeString(i.TriageClass),
			html.EscapeString(note),
			html.EscapeString(i.Title),
			reproCell,
		))
	}
	return b.String()
}

func renderFuzzNoiseRows(report map[string]any) string {
	arr, ok := report["coverage_noise"].([]fuzzProductTopIssue)
	if !ok || len(arr) == 0 {
		return `<tr><td colspan="4" class="muted">No detector/property coverage noise in this sample.</td></tr>`
	}
	var b strings.Builder
	for _, i := range arr {
		explain := strings.TrimSpace(i.Explain)
		if explain == "" {
			explain = "—"
		}
		b.WriteString(fmt.Sprintf(
			`<tr><td>%s</td><td>%s</td><td>%s</td><td class="muted">%s</td></tr>`,
			html.EscapeString(i.Severity),
			html.EscapeString(i.FindingType),
			html.EscapeString(i.Title),
			html.EscapeString(explain),
		))
	}
	return b.String()
}

func renderFuzzReproSection(report map[string]any) string {
	arr := productTopIssues(report)
	var blocks []string
	for _, i := range arr {
		cmd := strings.TrimSpace(i.Repro.Command)
		if cmd == "" {
			cmd = strings.TrimSpace(i.ReproCmd)
		}
		if cmd == "" && strings.TrimSpace(i.Artifact) == "" && strings.TrimSpace(i.InputSHA256) == "" {
			continue
		}
		inputLine := ""
		if i.Repro.InputSHA256 != "" {
			inputLine += fmt.Sprintf(`<p class="muted">input_sha256: <code>%s</code></p>`, html.EscapeString(i.Repro.InputSHA256))
		}
		if i.Repro.InputHex != "" {
			inputLine += fmt.Sprintf(`<p class="muted">input_hex: <code>%s</code></p>`, html.EscapeString(i.Repro.InputHex))
		} else if i.Repro.InputN != "" {
			inputLine += fmt.Sprintf(`<p class="muted">input: <code>%s</code></p>`, html.EscapeString(i.Repro.InputN))
		}
		art := ""
		if strings.TrimSpace(i.Artifact) != "" {
			art = fmt.Sprintf(`<p class="muted">Artifact: <code>%s</code></p>`, html.EscapeString(i.Artifact))
		}
		cmdHTML := html.EscapeString(cmd)
		if cmdHTML == "" {
			cmdHTML = `<span class="muted">(no repro command — re-run with WASM guard linked)</span>`
		}
		readyNote := ""
		if fuzzengine.IsCrashClass(i.FindingType) && !i.Repro.Ready {
			readyNote = `<p class="muted">` + html.EscapeString(i.Repro.Gap) + `</p>`
		}
		blocks = append(blocks, fmt.Sprintf(
			`<div class="repro-block"><p class="lbl">%s · %s · 1-click repro</p>%s%s%s<pre class="repro">%s</pre></div>`,
			html.EscapeString(i.Severity), html.EscapeString(i.FindingType), inputLine, art, readyNote, cmdHTML,
		))
	}
	if len(blocks) == 0 {
		return ""
	}
	return `<div class="card"><p class="lbl">Reproduction (input → command → same crash)</p>` + strings.Join(blocks, "") + `</div>`
}

func renderFuzzEngineNote(report map[string]any) string {
	scopeBlock := `<div class="card scope"><p class="lbl">Scope &amp; honesty</p>
<p>This report covers <strong>WASM sandbox</strong> execution of your linked guard module
(<code>check(i64)→i32</code> or <code>check_bytes(ptr,len)→i32</code> when <code>input_mode=bytes</code>), not a full upstream node audit.
<strong>Top issues are crash-first</strong> (crash / hang / ASan / memory). Detector and property signals live in the coverage-noise appendix and are not CVE claims.
This report is derived from the fetched evidence window for this request (<code>?limit=...</code>), so shown rows may represent only part of the full campaign history.
Use <code>repro</code> (input → command) locally, then validate crash-class issues against native builds before claiming 0-day.
Public L1 research (qa-assets corpus) lives at <a href="https://hackme.tech/reports/l1-crypto-stack-v3.html">l1-crypto-stack-v3</a> and is separate from this token-gated campaign.</p></div>`
	meta := ""
	if m, ok := report["fuzz_engine"].(map[string]any); ok {
		parts := []string{}
		for _, k := range []string{"semantics", "sandbox", "worker", "check_semantics", "depth_tier", "input_mode", "max_input_bytes", "guard_pack", "version"} {
			if v := strings.TrimSpace(toString(m[k])); v != "" {
				parts = append(parts, k+"="+v)
			}
		}
		if len(parts) > 0 {
			meta = fmt.Sprintf(`<p class="muted">Engine: %s</p>`, html.EscapeString(strings.Join(parts, " · ")))
		}
	}
	return scopeBlock + meta
}
