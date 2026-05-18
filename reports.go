package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (a *app) fetchRemoteReportsBlocksJSON(ctx context.Context, base string, limit int) (map[string]any, error) {
	u := strings.TrimRight(base, "/") + "/api/reports/blocks?limit=" + strconv.Itoa(limit)
	httpSec := 8
	curlSec := 12
	if envBool("HACKME_DESKTOP_MODE", false) {
		httpSec = 4
		curlSec = 14
	}
	tryHTTP := func() (map[string]any, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		cl := &http.Client{Timeout: time.Duration(httpSec) * time.Second}
		resp, err := cl.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
			return nil, fmt.Errorf("remote status=%d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
		}
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return nil, err
		}
		return out, nil
	}
	tryCurl := func() (map[string]any, error) {
		if coordinatorURLIsLoopback(base) {
			return nil, fmt.Errorf("canonical base is loopback")
		}
		curlCtx, cancel := context.WithTimeout(context.Background(), time.Duration(curlSec+2)*time.Second)
		defer cancel()
		return fetchJSONViaCurlMax(curlCtx, u, nil, curlSec)
	}
	if envBool("HACKME_DESKTOP_MODE", false) && !coordinatorURLIsLoopback(base) {
		if out, err := tryCurl(); err == nil && out != nil {
			return out, nil
		}
		return tryHTTP()
	}
	if out, err := tryHTTP(); err == nil {
		return out, nil
	}
	return tryCurl()
}

func (a *app) handleReportsBlocks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := 30
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 500 {
		limit = 500
	}
	ctx := r.Context()
	source := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("source")))
	if source == "" {
		source = "auto"
	}
	localNote := "PoH reward history uses ~0.01 HMC per block in metrics; on-chain wallet is authoritative."

	localH, _, tipErr := a.chain.Tip(ctx)
	if tipErr != nil {
		http.Error(w, tipErr.Error(), http.StatusInternalServerError)
		return
	}

	canonBase := strings.TrimRight(strings.TrimSpace(a.canonicalChainBaseURL()), "/")
	writeCanon := func(rm map[string]any) bool {
		bl, ok := rm["blocks"]
		if !ok || bl == nil {
			return false
		}
		note := "Rows proxied from the canonical command node so this table matches public chain height (local SQLite may lag until P2P sync)."
		if n, ok := rm["note"].(string); ok && strings.TrimSpace(n) != "" {
			note = note + " " + strings.TrimSpace(n)
		}
		out := map[string]any{
			"blocks":           bl,
			"note":             note,
			"blocks_source":    "canonical_peer",
			"canonical_base":   canonBase,
			"local_tip_height": localH,
		}
		tctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		if _, hCanon, _, okC := a.fetchCanonicalStatusTip(tctx); okC {
			out["canonical_tip_height"] = hCanon
		}
		cancel()
		writeJSON(w, out)
		return true
	}

	if source == "canonical" {
		if canonBase == "" {
			writeJSON(w, map[string]any{
				"ok":     false,
				"reason": "canonical_not_configured",
				"note":   "Set HACKME_CANONICAL_CHAIN_URL or network-mode peer inference for canonical block history.",
			})
			return
		}
		rctx, cancel := context.WithTimeout(ctx, 6*time.Second)
		rm, err := a.fetchRemoteReportsBlocksJSON(rctx, canonBase, limit)
		cancel()
		if err == nil && writeCanon(rm) {
			return
		}
		rows, err2 := a.chain.ListRecentBlockSummaries(ctx, limit)
		if err2 != nil {
			http.Error(w, err2.Error(), http.StatusInternalServerError)
			return
		}
		msg := localNote + " Canonical peer blocks unavailable; showing local ledger."
		if err != nil {
			msg = msg + " (" + err.Error() + ")"
		}
		writeJSON(w, map[string]any{
			"blocks":             rows,
			"note":               msg,
			"blocks_source":      "local_ledger",
			"local_tip_height":   localH,
			"canonical_fetch_ok": false,
		})
		return
	}

	if source == "auto" && a.networkModeActive() && canonBase != "" {
		tctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, hCanon, _, okCanon := a.fetchCanonicalStatusTip(tctx)
		cancel()
		if okCanon && hCanon != localH {
			rctx, cancel2 := context.WithTimeout(ctx, 6*time.Second)
			rm, err := a.fetchRemoteReportsBlocksJSON(rctx, canonBase, limit)
			cancel2()
			if err == nil && writeCanon(rm) {
				return
			}
		}
	}

	rows, err := a.chain.ListRecentBlockSummaries(ctx, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"blocks":           rows,
		"note":             localNote,
		"blocks_source":    "local_ledger",
		"local_tip_height": localH,
	})
}

func (a *app) handleReportsBlockLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s := strings.TrimSpace(r.URL.Query().Get("index"))
	if s == "" {
		http.Error(w, "missing index", http.StatusBadRequest)
		return
	}
	idx, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		http.Error(w, "invalid index", http.StatusBadRequest)
		return
	}
	row, ok, err := a.chain.GetBlockSummaryByIndex(r.Context(), idx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "block not found", http.StatusNotFound)
		return
	}
	writeJSON(w, row)
}

func (a *app) handleReportsHardware(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s := collector.snapshot()
	ms := a.miner.Stats()
	profile := a.loadMiningProfile(r.Context())
	cpuAlias := strings.TrimSpace(a.loadGPUAlias(r.Context(), "cpu", 0))
	cpuName := strings.TrimSpace(s.CPUModel)
	if cpuName == "" {
		cpuName = "unknown"
	}

	type row struct {
		Type         string  `json:"type"`
		Backend      string  `json:"backend"`
		Index        int     `json:"index"`
		Name         string  `json:"name"`
		Alias        string  `json:"alias"`
		DisplayName  string  `json:"display_name"`
		Enabled      bool    `json:"enabled"`
		Priority     int     `json:"priority"`
		HashrateGHS  float64 `json:"hashrate_gh_s,omitempty"`
		TempC        float64 `json:"temp_c,omitempty"`
		WorkedSecEst float64 `json:"worked_seconds_estimate"`
	}

	rows := make([]row, 0, len(ms.GPUPoHDevices)+1)
	rows = append(rows, row{
		Type:         "cpu",
		Backend:      "cpu",
		Index:        0,
		Name:         cpuName,
		Alias:        cpuAlias,
		DisplayName:  chooseAlias(cpuAlias, cpuName),
		Enabled:      a.loadGPUEnabled(r.Context(), "cpu", 0),
		Priority:     a.loadGPUPriority(r.Context(), "cpu", 0),
		WorkedSecEst: ms.SessionSeconds,
	})

	for _, d := range ms.GPUPoHDevices {
		alias := strings.TrimSpace(a.loadGPUAlias(r.Context(), d.Backend, d.Index))
		name := strings.TrimSpace(d.Name)
		if name == "" {
			name = strings.TrimSpace(d.Label)
		}
		workSec := 0.0
		if d.HashrateGHS > 0 {
			workSec = ms.SessionSeconds
		}
		rows = append(rows, row{
			Type:         "gpu",
			Backend:      strings.TrimSpace(d.Backend),
			Index:        d.Index,
			Name:         name,
			Alias:        alias,
			DisplayName:  chooseAlias(alias, name),
			Enabled:      a.loadGPUEnabled(r.Context(), d.Backend, d.Index),
			Priority:     a.loadGPUPriority(r.Context(), d.Backend, d.Index),
			HashrateGHS:  d.HashrateGHS,
			TempC:        d.TempC,
			WorkedSecEst: workSec,
		})
	}

	generatedAt := time.Now().Unix()
	report := map[string]any{
		"generated_at_unix": generatedAt,
		"node_address":      a.nodeID,
		"profile_mode":      profile,
		"session_seconds":   ms.SessionSeconds,
		"attempts_total":    ms.AttemptsTotal,
		"attempts_per_sec":  ms.AttemptsPerSec,
		"devices":           rows,
	}

	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" || format == "json" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "hardware_report.json"))
		writeJSON(w, report)
		return
	}
	if format != "csv" {
		http.Error(w, "unsupported format", http.StatusBadRequest)
		return
	}

	buf := &bytes.Buffer{}
	cw := csv.NewWriter(buf)
	_ = cw.Write([]string{"generated_at_unix", "node_address", "profile_mode", "session_seconds", "attempts_total", "attempts_per_sec"})
	_ = cw.Write([]string{
		strconv.FormatInt(generatedAt, 10),
		a.nodeID,
		profile,
		fmt.Sprintf("%.2f", ms.SessionSeconds),
		strconv.FormatUint(ms.AttemptsTotal, 10),
		fmt.Sprintf("%.2f", ms.AttemptsPerSec),
	})
	_ = cw.Write([]string{})
	_ = cw.Write([]string{"type", "backend", "index", "name", "alias", "display_name", "enabled", "priority", "hashrate_gh_s", "temp_c", "worked_seconds_estimate"})
	for _, d := range rows {
		_ = cw.Write([]string{
			d.Type,
			d.Backend,
			strconv.Itoa(d.Index),
			d.Name,
			d.Alias,
			d.DisplayName,
			strconv.FormatBool(d.Enabled),
			strconv.Itoa(d.Priority),
			fmt.Sprintf("%.3f", d.HashrateGHS),
			fmt.Sprintf("%.2f", d.TempC),
			fmt.Sprintf("%.2f", d.WorkedSecEst),
		})
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "hardware_report.csv"))
	_, _ = w.Write(buf.Bytes())
}

func chooseAlias(alias, fallback string) string {
	alias = strings.TrimSpace(alias)
	if alias != "" {
		return alias
	}
	return fallback
}
