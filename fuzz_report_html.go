package main

import (
	"database/sql"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"
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
	ver := strings.ToLower(strings.TrimSpace(toString(report["verdict"])))
	if ver == "" {
		ver = "unknown"
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
	reproSection := renderFuzzReproSection(report)
	engineNote := renderFuzzEngineNote(report)

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
.badge{display:inline-block;margin-top:1rem;padding:.45rem 1.15rem;border-radius:999px;border:2px solid %s;color:%s;font-size:1.05rem;font-weight:700;text-transform:uppercase;letter-spacing:.18em}
.card{border:1px solid rgba(0,209,255,.22);border-radius:14px;background:linear-gradient(145deg,rgba(0,0,0,.55),rgba(0,209,255,.07));padding:1.35rem;margin:1.1rem 0}
.card.scope{border-color:rgba(255,176,32,.35);background:linear-gradient(145deg,rgba(0,0,0,.5),rgba(255,176,32,.06))}
.title{font-size:1.15rem;color:#fff;margin:.4rem 0}
.muted{color:#6b7c93;font-size:.78rem}
.lbl{color:#00d1ff;font-size:.68rem;text-transform:uppercase;letter-spacing:.1em}
code{color:#39ff14;font-size:.82rem;word-break:break-all}
pre.repro{margin:.5rem 0 0;padding:.75rem 1rem;background:rgba(0,0,0,.45);border-radius:8px;border:1px solid rgba(57,255,20,.2);overflow-x:auto;font-size:.75rem;color:#a8e6cf}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(130px,1fr));gap:.7rem;margin-top:1.1rem}
.stat{border:1px solid rgba(57,255,20,.18);border-radius:10px;padding:.7rem .9rem;background:rgba(0,0,0,.38)}
.stat b{display:block;font-size:.62rem;text-transform:uppercase;letter-spacing:.1em;color:#6b7c93;margin-bottom:.25rem}
.tag{display:inline-block;padding:.15rem .5rem;border-radius:4px;font-size:.65rem;text-transform:uppercase;letter-spacing:.05em}
.tag.expected{background:rgba(0,209,255,.15);color:#00d1ff}
.tag.needs{background:rgba(255,96,96,.2);color:#ff9090}
.tag.sandbox{background:rgba(255,176,32,.15);color:#ffb020}
.tag.guard{background:rgba(57,255,20,.12);color:#7dffb8}
table{width:100%%;border-collapse:collapse;font-size:.78rem;margin-top:.65rem}
th,td{border-bottom:1px solid rgba(255,255,255,.08);padding:.5rem .55rem;text-align:left;vertical-align:top}
th{color:#6b7c93;text-transform:uppercase;font-size:.62rem;letter-spacing:.06em}
ul{margin:.4rem 0 0;padding-left:1.2rem}
footer{margin-top:2.5rem;padding-top:1rem;border-top:1px solid rgba(255,255,255,.08);font-size:.72rem;color:#6b7c93}
a{color:#00d1ff}
@media print{body{background:#fff;color:#111}.card{border-color:#ccc;background:#f8f8f8}code{color:#060}}
</style>
</head>
<body>
<div class="wrap">
<h1>HackMe Security Report</h1>
<p class="sub">fuzz_report_v2 · %s · <a href="https://hackme.tech" target="_blank" rel="noopener">hackme.tech</a></p>
%s
<div class="card">
<p class="lbl">Campaign</p>
<p class="title">%s</p>
<p class="muted">%s · %s · status %s</p>
%s
<span class="badge">%s</span>
<div class="grid">
<div class="stat"><b>Confidence</b>%s</div>
<div class="stat"><b>Vulnerabilities</b>%s</div>
<div class="stat"><b>Critical</b>%s</div>
<div class="stat"><b>High</b>%s</div>
<div class="stat"><b>Medium</b>%s</div>
<div class="stat"><b>Sample size</b>%s</div>
<div class="stat"><b>Budget runs</b>%s</div>
</div>
</div>
<div class="card">
<p class="lbl">Top issues (with triage)</p>
<table><thead><tr><th>Severity</th><th>Type</th><th>Triage</th><th>Title</th><th>Repro</th></tr></thead><tbody>%s</tbody></table>
</div>
%s
<div class="card">
<p class="lbl">Recommendations</p>
<ul>%s</ul>
</div>
<footer>HackMe Network · fuzz_report_v2 · JSON: <code>?format=json</code> · CSV: <code>report.csv</code> · Generated %s</footer>
</div>
</body>
</html>`,
		html.EscapeString(title),
		verColor, verColor,
		html.EscapeString(gen),
		engineNote,
		html.EscapeString(title),
		html.EscapeString(toString(c["id"])),
		html.EscapeString(toString(c["campaign_type"])),
		html.EscapeString(toString(c["status"])),
		taskBlock,
		html.EscapeString(ver),
		html.EscapeString(toString(sum["confidence"])),
		html.EscapeString(toString(sum["vulnerabilities_found"])),
		html.EscapeString(toString(sum["critical_count"])),
		html.EscapeString(toString(sum["high_count"])),
		html.EscapeString(toString(sum["medium_count"])),
		html.EscapeString(toString(sum["sample_size"])),
		html.EscapeString(toString(c["budget_runs"])),
		issueRows,
		reproSection,
		recLi,
		html.EscapeString(gen),
	)
}

func renderFuzzIssueRows(report map[string]any) string {
	arr, ok := report["top_issues"].([]fuzzTopIssue)
	if !ok || len(arr) == 0 {
		return `<tr><td colspan="5" class="muted">No findings in this sample window.</td></tr>`
	}
	var b strings.Builder
	for _, i := range arr {
		tagClass := "review"
		switch i.TriageClass {
		case "expected_signal":
			tagClass = "expected"
		case "needs_triage":
			tagClass = "needs"
		case "sandbox":
			tagClass = "sandbox"
		case "guard_signal":
			tagClass = "guard"
		}
		reproCell := `<span class="muted">—</span>`
		if strings.TrimSpace(i.ReproCmd) != "" {
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

func renderFuzzReproSection(report map[string]any) string {
	arr, _ := report["top_issues"].([]fuzzTopIssue)
	var blocks []string
	for _, i := range arr {
		if strings.TrimSpace(i.ReproCmd) == "" && strings.TrimSpace(i.Artifact) == "" {
			continue
		}
		art := ""
		if strings.TrimSpace(i.Artifact) != "" {
			art = fmt.Sprintf(`<p class="muted">Artifact: <code>%s</code></p>`, html.EscapeString(i.Artifact))
		}
		cmd := html.EscapeString(i.ReproCmd)
		if cmd == "" {
			cmd = `<span class="muted">(no repro_cmd — re-run campaign with WASM guard linked)</span>`
		}
		blocks = append(blocks, fmt.Sprintf(
			`<div class="repro-block"><p class="lbl">%s · %s</p>%s<pre class="repro">%s</pre></div>`,
			html.EscapeString(i.Severity), html.EscapeString(i.FindingType), art, cmd,
		))
	}
	if len(blocks) == 0 {
		return ""
	}
	return `<div class="card"><p class="lbl">Reproduction commands</p>` + strings.Join(blocks, "") + `</div>`
}

func renderFuzzEngineNote(report map[string]any) string {
	scopeBlock := `<div class="card scope"><p class="lbl">Scope &amp; honesty</p>
<p>This report covers <strong>WASM sandbox</strong> execution of your linked guard module (<code>check(i64)→i32</code>), not a full upstream node audit.
Property signals are often expected detector noise — use <code>repro_cmd</code> locally, then validate any crash-class issue against native Bitcoin/Ethereum builds before claiming CVE or 0-day.
Public L1 research (qa-assets corpus) lives at <a href="https://hackme.tech/reports/l1-crypto-stack-v3.html">l1-crypto-stack-v3</a> and is separate from this token-gated campaign.</p></div>`
	meta := ""
	if m, ok := report["fuzz_engine"].(map[string]any); ok {
		parts := []string{}
		for _, k := range []string{"semantics", "sandbox", "worker"} {
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
