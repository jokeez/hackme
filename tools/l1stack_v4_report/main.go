// l1stack_v4_report — L1 v4 HTML: offline v3 corpus + live production fuzz ledger.
package main

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	root := findRoot()
	v3Path := filepath.Join(root, "reports", "l1-crypto-stack-v3", "latest.json")
	livePath := filepath.Join(root, "reports", "l1-crypto-stack-v4", "live", "summary.json")

	var v3, live map[string]any
	_ = readJSON(v3Path, &v3)
	if err := readJSON(livePath, &live); err != nil {
		fmt.Fprintf(os.Stderr, "missing live summary — run PHASE=live first: %v\n", err)
		os.Exit(1)
	}

	outJSON := filepath.Join(root, "reports", "l1-crypto-stack-v4", "latest.json")
	outHTML := filepath.Join(root, "docs", "reports", "l1-crypto-stack-v4-report.html")
	siteHTML := filepath.Join(root, "web", "site", "reports", "l1-crypto-stack-v4.html")

	combined := map[string]any{
		"title":     "L1 Crypto Stack v4 · Live useful-PoW fuzz on upstream guards",
		"version":   "v4",
		"offline":   v3,
		"live":      live,
		"timestamp": live["timestamp"],
	}
	_ = os.MkdirAll(filepath.Dir(outJSON), 0o755)
	writeJSON(outJSON, combined)

	page := renderV4HTML(v3, live)
	_ = os.MkdirAll(filepath.Dir(siteHTML), 0o755)
	_ = os.WriteFile(outHTML, []byte(page), 0o644)
	_ = os.WriteFile(siteHTML, []byte(page), 0o644)
	fmt.Fprintf(os.Stderr, "wrote %s\nwrote %s\n", outJSON, siteHTML)
	fmt.Printf("file://%s\n", outHTML)
}

func renderV4HTML(v3, live map[string]any) string {
	ts := esc(live["timestamp"])
	base := esc(live["base"])
	spent := fmt.Sprintf("%.6f", toFloat(live["wallet_spent_hmc"]))
	wb := asMap(live["wallet_before"])
	wa := asMap(live["wallet_after"])
	cb := asMap(live["chain_before"])
	ca := asMap(live["chain_after"])
	blockDelta := fmt.Sprintf("%v", live["blocks_mined_delta"])
	totalRuns := fmt.Sprintf("%v", live["total_runs_done"])
	runsPaid := fmt.Sprintf("%.6f", toFloat(live["total_runs_paid_hmc"]))
	bountyPaid := fmt.Sprintf("%.6f", toFloat(live["total_bounty_paid_hmc"]))

	corpusRows := ""
	if v3 != nil {
		for _, raw := range asSlice(v3["corpora"]) {
			c := asMap(raw)
			name := esc(c["corpus_name"])
			probed := fmt.Sprintf("%v", c["files_probed"])
			signals := fmt.Sprintf("%v", c["signals"])
			traps := fmt.Sprintf("%v", c["traps"])
			corpusRows += fmt.Sprintf("<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>\n",
				name, probed, signals, traps)
		}
	}
	if corpusRows == "" {
		corpusRows = `<tr><td colspan="4">Run offline phase first.</td></tr>`
	}

	campRows := ""
	for _, raw := range asSlice(live["campaigns"]) {
		c := asMap(raw)
		verdict := strings.ToLower(fmt.Sprint(c["verdict"]))
		color := "#39ff14"
		if strings.Contains(verdict, "fail") {
			color = "#ff6060"
		} else if verdict == "?" || verdict == "" {
			color = "#ffb020"
		}
		reportURL := fmt.Sprint(c["report_url"])
		link := esc(reportURL)
		if reportURL != "" {
			link = fmt.Sprintf(`<a href="%s" target="_blank" rel="noopener">report.html</a>`, esc(reportURL))
		}
		campRows += fmt.Sprintf(`<tr>
<td>%s</td><td><code>%s</code></td><td>%s</td><td>%s</td>
<td>%v / %v</td><td style="color:%s;font-weight:700">%s</td><td>%v</td>
<td>%.4f</td><td>%.4f</td><td><code>%s</code></td><td>%s</td></tr>`,
			esc(c["chain"]), esc(c["guard"]), esc(c["campaign_id"]), esc(c["status"]),
			c["runs_done"], c["budget_runs"], color, esc(c["verdict"]), c["findings"],
			toFloat(c["runs_paid_hmc"]), toFloat(c["bounty_paid_hmc"]), esc(c["finding_winner"]),
			link)
	}

	fidBadge := "OFFLINE CORPUS OK"
	fidColor := "#39ff14"
	if v3 == nil || v3["traps_total"] != float64(0) && fmt.Sprint(v3["traps_total"]) != "0" {
		if v3 == nil {
			fidBadge = "OFFLINE PENDING"
			fidColor = "#ffb020"
		}
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>HackMe · L1 Crypto Stack v4 — Live Production Fuzz</title>
<link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;600;700&family=Space+Grotesk:wght@500;700&display=swap" rel="stylesheet"/>
<style>
:root{--neon:#00d1ff;--matrix:#39ff14;--warn:#ffb020;--bg:#05080f;--card:rgba(0,0,0,.45)}
body{margin:0;font-family:"JetBrains Mono",monospace;background:var(--bg);color:#c5d4e8;line-height:1.6}
.wrap{max-width:1020px;margin:0 auto;padding:2rem 1.25rem 4rem}
.hero{text-align:center;padding:2rem;border:1px solid rgba(0,209,255,.35);border-radius:20px;background:linear-gradient(160deg,rgba(0,209,255,.12),transparent);margin-bottom:1.5rem}
.hero h1{font-family:"Space Grotesk",sans-serif;color:var(--neon);font-size:1.55rem;margin:0}
.hero .tag{font-size:.7rem;color:#6d8099;letter-spacing:.15em;text-transform:uppercase}
.badge{display:inline-block;margin:.8rem .4rem 0;padding:.45rem 1rem;border-radius:999px;border:2px solid %s;color:%s;font-weight:700;font-size:.78rem}
.live-badge{border-color:var(--matrix);color:var(--matrix)}
section{margin:1.5rem 0;padding:1.2rem;border:1px solid rgba(255,255,255,.1);border-radius:14px;background:var(--card)}
section h2{font-family:"Space Grotesk",sans-serif;color:var(--neon);font-size:1.1rem;margin:0 0 .8rem}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:.8rem;margin:.8rem 0}
.stat{border:1px solid rgba(0,209,255,.25);border-radius:10px;padding:.8rem;text-align:center}
.stat b{display:block;font-size:1.2rem;color:var(--matrix)}
table{width:100%%;border-collapse:collapse;font-size:.75rem;margin-top:.6rem}
th,td{border:1px solid #2a3544;padding:.45rem .5rem;text-align:left;vertical-align:top}
th{background:#141820;color:var(--neon)}
a{color:var(--neon)}
footer{margin-top:2rem;font-size:.7rem;color:#6d8099;text-align:center}
</style>
</head>
<body>
<div class="wrap">
<header class="hero">
<p class="tag">HackMe Network · L1 Research · %s UTC</p>
<h1>L1 Crypto Stack v4</h1>
<p>Official Bitcoin Core corpus (offline) + <b>live useful-PoW fuzz</b> on upstream WASM guards at <a href="%s">%s</a></p>
<span class="badge live-badge">LIVE PRODUCTION FUZZ</span>
<span class="badge" style="border-color:%s;color:%s">%s</span>
</header>

<section>
<h2>Wallet &amp; chain ledger</h2>
<div class="grid">
<div class="stat"><span>Operator wallet</span><b>%s</b><small>HMC before → after</small></div>
<div class="stat"><span>Spent (escrow)</span><b>%s HMC</b><small>%.4f → %.4f</small></div>
<div class="stat"><span>Blocks mined Δ</span><b>%s</b><small>tip %v → %v</small></div>
<div class="stat"><span>Fuzz runs paid</span><b>%s HMC</b><small>bounty %s HMC</small></div>
</div>
<p class="tag">Payouts: 20%% runs pool to workers · 80%% bounty pool · escrow debited from operator node wallet on canonical hub.</p>
</section>

<section>
<h2>Live campaigns (5 chains · production pool)</h2>
<p>Total runs executed: <b>%s</b> across <b>%v</b> campaigns. Each row links to auto-generated <code>fuzz_report_v2</code> HTML on the hub.</p>
<table>
<thead><tr><th>Chain</th><th>Guard</th><th>Campaign</th><th>Status</th><th>Runs</th><th>Verdict</th><th>Findings</th><th>Runs paid</th><th>Bounty paid</th><th>Winner</th><th>Report</th></tr></thead>
<tbody>%s</tbody>
</table>
</section>

<section>
<h2>Offline layer (v3 · official qa-assets corpus)</h2>
<table>
<thead><tr><th>Corpus</th><th>Seeds probed</th><th>Violation signals</th><th>WASM traps</th></tr></thead>
<tbody>%s</tbody>
</table>
<p><a href="l1-crypto-stack-v3.html">Full v3 report</a> · <a href="bitcoin-core-5module.html">BC5 deep-dive</a></p>
</section>

<section>
<h2>Honest scope</h2>
<ul>
<li>Live phase debits real HMC from the operator wallet and pays pool workers via fuzz escrow — verifiable on-chain / in wallet API.</li>
<li>Offline phase uses <code>bitcoin-core/qa-assets</code> seeds through WASM ports — not a claim of running full <code>bitcoind</code>.</li>
<li>Native libFuzzer differential vs Core binary is the planned v4.1 follow-up.</li>
</ul>
</section>

<footer>
HackMe Network · L1 v4 · Reproduce: <code>bash scripts/ops/run_l1_crypto_stack_v4_live.sh</code> · %s
</footer>
</div>
</body>
</html>`,
		fidColor, fidColor, ts, base, base, fidColor, fidColor, fidBadge,
		esc(wb["address"]), spent, toFloat(wb["balance_hmc"]), toFloat(wa["balance_hmc"]),
		blockDelta, cb["tip_height"], ca["tip_height"],
		runsPaid, bountyPaid,
		totalRuns, live["total_campaigns"], campRows, corpusRows, ts)
}

func findRoot() string {
	cwd, _ := os.Getwd()
	for d := cwd; d != "/"; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
	}
	return cwd
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func writeJSON(path string, v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	_ = os.WriteFile(path, b, 0o644)
}

func esc(v any) string {
	return html.EscapeString(strings.TrimSpace(fmt.Sprint(v)))
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case json.Number:
		f, _ := x.Float64()
		return f
	default:
		var f float64
		fmt.Sscanf(fmt.Sprint(v), "%f", &f)
		return f
	}
}
