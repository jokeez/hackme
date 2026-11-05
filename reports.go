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

	"hackme/internal/chain"
)

func (a *app) fetchRemoteJSONMap(ctx context.Context, base, path string) (map[string]any, error) {
	u := strings.TrimRight(base, "/") + path
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

func (a *app) fetchRemoteReportsBlocksJSON(ctx context.Context, base string, limit int, before uint64) (map[string]any, error) {
	path := "/api/reports/blocks?limit=" + strconv.Itoa(limit)
	if before > 0 {
		path += "&before=" + strconv.FormatUint(before, 10)
	}
	out, err := a.fetchRemoteJSONMap(ctx, base, path)
	if err != nil {
		return nil, err
	}
	a.enrichReportsBlocksWithFullHash(ctx, base, limit, out)
	return out, nil
}

// enrichReportsBlocksWithFullHash merges full block hashes from /api/chain when
// reports rows only expose truncated hash_prefix (pre-upgrade peers).
func (a *app) enrichReportsBlocksWithFullHash(ctx context.Context, base string, limit int, reports map[string]any) {
	if reports == nil {
		return
	}
	rawBlocks, ok := reports["blocks"].([]any)
	if !ok || len(rawBlocks) == 0 {
		return
	}
	need := false
	for _, row := range rawBlocks {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		h := strings.TrimSpace(fmt.Sprint(m["hash"]))
		if h == "" || h == "<nil>" || strings.Contains(h, "…") || strings.HasSuffix(h, "...") {
			need = true
			break
		}
	}
	if !need {
		return
	}
	// Own path: avoid curl-semaphore contention with the just-finished reports fetch,
	// and avoid net/http 403s some edges see against the public hub.
	u := strings.TrimRight(base, "/") + "/api/chain?limit=" + strconv.Itoa(limit)
	chain, err := fetchJSONViaCurlDesktop(u, 8)
	if err != nil || chain == nil {
		ectx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		chain, err = a.fetchRemoteJSONMap(ectx, base, "/api/chain?limit="+strconv.Itoa(limit))
		cancel()
		if err != nil || chain == nil {
			return
		}
	}
	byIndex := map[uint64]string{}
	switch rows := chain["blocks"].(type) {
	case []any:
		for _, row := range rows {
			m, ok := row.(map[string]any)
			if !ok {
				continue
			}
			idx := jsonUint64(m["index"])
			h := strings.TrimSpace(fmt.Sprint(m["hash"]))
			if idx > 0 && h != "" && h != "<nil>" && !strings.Contains(h, "…") {
				byIndex[idx] = h
			}
		}
	}
	if len(byIndex) == 0 {
		return
	}
	for _, row := range rawBlocks {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		idx := jsonUint64(m["index"])
		if h, ok := byIndex[idx]; ok {
			m["hash"] = h
		}
	}
}

func jsonUint64(v any) uint64 {
	switch t := v.(type) {
	case float64:
		if t < 0 {
			return 0
		}
		return uint64(t)
	case json.Number:
		n, _ := t.Int64()
		if n < 0 {
			return 0
		}
		return uint64(n)
	case string:
		n, _ := strconv.ParseUint(strings.TrimSpace(t), 10, 64)
		return n
	case int:
		if t < 0 {
			return 0
		}
		return uint64(t)
	case int64:
		if t < 0 {
			return 0
		}
		return uint64(t)
	case uint64:
		return t
	default:
		return 0
	}
}

func (a *app) handleReportsBlocks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// q=… resolves one block over the whole local chain (task id / hash / prefix / index).
	// Lives on /api/reports/blocks because public nginx allowlists only this reports path.
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		row, ok, err := a.chain.FindBlockSummary(r.Context(), q)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		blocks := []chain.BlockSummary{}
		if ok {
			blocks = append(blocks, row)
		}
		writeJSON(w, map[string]any{
			"lookup":        true,
			"found":         ok,
			"blocks":        blocks,
			"blocks_source": "local_ledger",
		})
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
	// before=N pages back toward genesis: blocks with index < N, newest first.
	var before uint64
	if s := strings.TrimSpace(r.URL.Query().Get("before")); s != "" {
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			http.Error(w, "bad before", http.StatusBadRequest)
			return
		}
		before = n
	}
	ctx := r.Context()
	source := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("source")))
	if source == "" {
		source = "auto"
	}
	localH, _, tipErr := a.chain.Tip(ctx)
	if tipErr != nil {
		http.Error(w, tipErr.Error(), http.StatusInternalServerError)
		return
	}
	// Reward follows the live schedule (v1 0.01 → v2 0.1 HMC at the activation height).
	localNote := fmt.Sprintf("PoH reward history uses ~%g HMC per block in metrics; on-chain wallet is authoritative.",
		chain.BaseRewardForBlockIndex(localH+1))

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
		rm, err := a.fetchRemoteReportsBlocksJSON(rctx, canonBase, limit, before)
		cancel()
		if err == nil && writeCanon(rm) {
			return
		}
		rows, err2 := a.chain.ListBlockSummariesBefore(ctx, before, limit)
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
			rctx, cancel2 := context.WithTimeout(ctx, 3*time.Second)
			rm, err := a.fetchRemoteReportsBlocksJSON(rctx, canonBase, limit, before)
			cancel2()
			if err == nil && writeCanon(rm) {
				return
			}
		}
	}

	rows, err := a.chain.ListBlockSummariesBefore(ctx, before, limit)
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
	// Off-loopback LAN nodes must not expose GPU/CPU model without a token.
	if !requireAdminAuthOrDesktopLoopback(w, r) {
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
