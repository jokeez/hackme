package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzingcli"
)

const fuzzTopIssueLimit = 5
const fuzzCoverageNoiseLimit = 25
const fuzzSanitizerHygieneLimit = 25

type fuzzReproBlock struct {
	InputSHA256      string `json:"input_sha256"`
	InputHex         string `json:"input_hex,omitempty"`
	InputN           string `json:"input_n,omitempty"`
	OriginalInputLen int    `json:"original_input_len,omitempty"`
	Trimmed          bool   `json:"trimmed,omitempty"`
	Command          string `json:"command"`
	Artifact         string `json:"artifact_path,omitempty"`
	Ready            bool   `json:"ready"`
	Gap              string `json:"gap,omitempty"`
}

type fuzzProductTopIssue struct {
	ID          string         `json:"id"`
	Severity    string         `json:"severity"`
	FindingType string         `json:"finding_type"`
	Title       string         `json:"title"`
	Impact      string         `json:"impact"`
	ReproCmd    string         `json:"repro_cmd"`
	Artifact    string         `json:"artifact_path"`
	InputSHA256 string         `json:"input_sha256,omitempty"`
	TriageClass        string         `json:"triage_class"`
	TriageNote         string         `json:"triage_note"`
	SanitizerClass     string         `json:"sanitizer_class,omitempty"`
	SanitizerSubtype   string         `json:"sanitizer_subtype,omitempty"`
	SanitizerLabel     string         `json:"sanitizer_label,omitempty"`
	GuardPack          string         `json:"guard_pack,omitempty"`
	Explain     string         `json:"explain,omitempty"`
	Repro       fuzzReproBlock `json:"repro"`
}

func findingInputHex(f fuzzFinding) string {
	if f.Detail == nil {
		return ""
	}
	for _, k := range []string{"input_hex", "InputHex"} {
		if v := strings.TrimSpace(toString(f.Detail[k])); v != "" {
			return strings.TrimPrefix(strings.ToLower(v), "0x")
		}
	}
	if v := f.Detail["actual_input"]; v != nil {
		switch x := v.(type) {
		case float64:
			return fmt.Sprintf("%x", uint64(x))
		case int64:
			return fmt.Sprintf("%x", uint64(x))
		case int:
			return fmt.Sprintf("%x", uint64(x))
		case string:
			s := strings.TrimSpace(strings.ToLower(x))
			s = strings.TrimPrefix(s, "0x")
			if s != "" {
				return s
			}
		}
	}
	return ""
}

func findingInputN(f fuzzFinding) string {
	if f.Detail == nil {
		return ""
	}
	if v := f.Detail["actual_input"]; v != nil {
		s := strings.TrimSpace(toString(v))
		if s != "" {
			return s
		}
	}
	if v := f.Detail["input_n"]; v != nil {
		return strings.TrimSpace(toString(v))
	}
	return ""
}

func buildFindingRepro(f fuzzFinding) fuzzReproBlock {
	cmd := strings.TrimSpace(f.ReproCmd)
	inSHA := strings.TrimSpace(strings.ToLower(f.InputSHA256))
	inHex := fuzzengine.RedactInputForReport(findingInputHex(f))
	inN := fuzzengine.RedactInputNForReport(findingInputN(f))
	art := strings.TrimSpace(f.Artifact)
	ready := cmd != "" && (inSHA != "" || inHex != "" || inN != "")
	gap := ""
	if fuzzengine.IsCrashClass(f.FindingType) && !ready {
		missing := make([]string, 0, 2)
		if cmd == "" {
			missing = append(missing, "command")
		}
		if inSHA == "" && inHex == "" && inN == "" {
			missing = append(missing, "input")
		}
		gap = "crash-class finding missing required repro fields: " + strings.Join(missing, "+")
	}
	return fuzzReproBlock{
		InputSHA256:      inSHA,
		InputHex:         inHex,
		InputN:           inN,
		OriginalInputLen: findingOriginalInputLen(f),
		Trimmed:          findingTrimmed(f),
		Command:          cmd,
		Artifact:         art,
		Ready:            ready,
		Gap:              gap,
	}
}

func findingOriginalInputLen(f fuzzFinding) int {
	if f.Detail == nil {
		return 0
	}
	if v := f.Detail["input_hex_original_len"]; v != nil {
		switch t := v.(type) {
		case float64:
			return int(t)
		case int:
			return t
		case int64:
			return int(t)
		}
	}
	return 0
}

func findingTrimmed(f fuzzFinding) bool {
	if f.Detail == nil {
		return false
	}
	if v, ok := f.Detail["hunt_trimmed"].(bool); ok {
		return v
	}
	if s := strings.TrimSpace(toString(f.Detail["hunt_trimmed"])); s == "true" || s == "1" {
		return true
	}
	orig := findingOriginalInputLen(f)
	if orig > 0 {
		if hx := strings.TrimSpace(findingInputHex(f)); hx != "" {
			return len(hx)/2 < orig
		}
	}
	return false
}

func toProductTopIssue(f fuzzFinding) fuzzProductTopIssue {
	triage := fuzzengine.ClassifyFinding(f.FindingType, f.Severity)
	repro := buildFindingRepro(f)
	packID := findingGuardPack(f)
	preview := findingExplainPreview(f, repro)
	explain := ""
	if packID != "" {
		explain = fuzzingcli.ExplainPackFinding(packID, preview, f.Title)
	} else if f.Detail != nil {
		if e := strings.TrimSpace(toString(f.Detail["explain"])); e != "" {
			explain = e
		}
	}
	return fuzzProductTopIssue{
		ID:               f.ID,
		Severity:         f.Severity,
		FindingType:      f.FindingType,
		Title:            fuzzengine.RedactSensitiveString(f.Title),
		Impact:           severityImpact(f.Severity),
		ReproCmd:         f.ReproCmd,
		Artifact:         f.Artifact,
		InputSHA256:      f.InputSHA256,
		TriageClass:      triage.Class,
		TriageNote:       triage.Note,
		SanitizerClass:   findingSanitizerField(f, "sanitizer_class"),
		SanitizerSubtype: findingSanitizerField(f, "sanitizer_subtype"),
		SanitizerLabel:   findingSanitizerField(f, "sanitizer_label"),
		GuardPack:        packID,
		Explain:          explain,
		Repro:            repro,
	}
}

func findingSanitizerField(f fuzzFinding, key string) string {
	if f.Detail == nil {
		return ""
	}
	return strings.TrimSpace(toString(f.Detail[key]))
}

func findingGuardPack(f fuzzFinding) string {
	if f.Detail == nil {
		return ""
	}
	for _, k := range []string{"guard_pack", "GuardPack"} {
		if v := strings.TrimSpace(toString(f.Detail[k])); v != "" {
			return v
		}
	}
	return ""
}

func findingExplainPreview(f fuzzFinding, repro fuzzReproBlock) string {
	if hx := strings.TrimSpace(repro.InputHex); hx != "" {
		// InputHex is already customer-safe (RedactInputForReport). Prefer it as-is;
		// do not re-decode into raw secret bytes for explain matching.
		return hx
	}
	if n := strings.TrimSpace(repro.InputN); n != "" {
		return n
	}
	return fuzzengine.RedactSensitiveString(f.Title)
}

// partitionFindingsCrashFirst splits findings into crash top issues, sanitizer hygiene, and coverage noise.
func partitionFindingsCrashFirst(findings []fuzzFinding, topLimit, noiseLimit int) (top []fuzzProductTopIssue, sanitizerHygiene []fuzzProductTopIssue, noise []fuzzProductTopIssue, crashCount, hygieneCount, noiseCount int) {
	if topLimit <= 0 {
		topLimit = fuzzTopIssueLimit
	}
	if noiseLimit <= 0 {
		noiseLimit = fuzzCoverageNoiseLimit
	}
	top = make([]fuzzProductTopIssue, 0, topLimit)
	sanitizerHygiene = make([]fuzzProductTopIssue, 0, fuzzSanitizerHygieneLimit)
	noise = make([]fuzzProductTopIssue, 0, noiseLimit)
	for _, f := range findings {
		switch {
		case fuzzengine.IsCrashClass(f.FindingType):
			crashCount++
			if len(top) < topLimit {
				top = append(top, toProductTopIssue(f))
			}
		case f.FindingType == "sanitizer_informational":
			hygieneCount++
			if len(sanitizerHygiene) < fuzzSanitizerHygieneLimit {
				n := toProductTopIssue(f)
				if n.SanitizerLabel == "" && n.SanitizerSubtype != "" {
					n.SanitizerLabel = strings.ToUpper(n.SanitizerClass) + " · " + n.SanitizerSubtype
				}
				sanitizerHygiene = append(sanitizerHygiene, n)
			}
		case fuzzengine.IsCoverageNoise(f.FindingType):
			noiseCount++
			if len(noise) < noiseLimit {
				n := toProductTopIssue(f)
				n.TriageClass = "coverage_noise"
				if n.TriageNote == "" {
					n.TriageNote = "Detector/property signal — coverage noise appendix"
				}
				noise = append(noise, n)
			}
		default:
			noiseCount++
			if len(noise) < noiseLimit {
				noise = append(noise, toProductTopIssue(f))
			}
		}
	}
	return top, sanitizerHygiene, noise, crashCount, hygieneCount, noiseCount
}

// buildSanitizerHygieneSummary aggregates informational sanitizer subtypes for Hunt reports.
func buildSanitizerHygieneSummary(findings []fuzzFinding) map[string]any {
	bySubtype := map[string]int{}
	byClass := map[string]int{}
	total := 0
	for _, f := range findings {
		if f.FindingType != "sanitizer_informational" {
			continue
		}
		total++
		class := findingSanitizerField(f, "sanitizer_class")
		sub := findingSanitizerField(f, "sanitizer_subtype")
		if class == "" {
			class = "unknown"
		}
		if sub == "" {
			sub = "unknown"
		}
		byClass[class]++
		bySubtype[class+"/"+sub]++
	}
	return map[string]any{
		"total":      total,
		"by_class":   byClass,
		"by_subtype": bySubtype,
	}
}

// collapseCrashFindingsForReport keeps one representative row per stable crash bucket for display.
func collapseCrashFindingsForReport(findings []fuzzFinding) (display []fuzzFinding, crashUnique, crashDup int) {
	seen := map[string]struct{}{}
	display = make([]fuzzFinding, 0, len(findings))
	for _, f := range findings {
		if !fuzzengine.IsCrashClass(f.FindingType) {
			display = append(display, f)
			continue
		}
		key := fuzzengine.StableFindingKeyFromDetail(f.FindingType, f.Detail)
		if _, dup := seen[key]; dup {
			crashDup++
			continue
		}
		seen[key] = struct{}{}
		display = append(display, f)
	}
	crashUnique = len(seen)
	return display, crashUnique, crashDup
}

func crashClassSeverityCounts(findings []fuzzFinding) (critical, high, medium, low, info int) {
	for _, f := range findings {
		if !fuzzengine.IsCrashClass(f.FindingType) {
			continue
		}
		switch normalizeFindingSeverity(f.Severity) {
		case "critical":
			critical++
		case "high":
			high++
		case "medium":
			medium++
		case "low":
			low++
		default:
			info++
		}
	}
	return
}

func crashClassSeverityScore(critical, high, medium, low, info int) int {
	return critical*100 + high*40 + medium*10 + low*3 + info
}

func buildHumanSummaryLine(runsDone, edges, paths, crashCount, criticalCrash int) string {
	critNote := "no critical"
	if criticalCrash > 0 {
		critNote = fmt.Sprintf("%d critical", criticalCrash)
	} else if crashCount == 0 {
		critNote = "no critical · no crash-class bugs"
	}
	cov := fmt.Sprintf("%d edges · %d paths", edges, paths)
	bugs := fmt.Sprintf("%d crash-class", crashCount)
	return fmt.Sprintf("%d runs · coverage %s · bugs/crashes %s · %s", runsDone, cov, bugs, critNote)
}

func buildVerdictCard(runsDone, crashCount, criticalCrash int, gatePass bool, moneySpent float64) map[string]any {
	gate := "FAIL"
	if gatePass {
		gate = "PASS"
	}
	lines := []string{
		fmt.Sprintf("Runs: %d", runsDone),
		fmt.Sprintf("Crashes: %d", crashCount),
		fmt.Sprintf("Critical: %d", criticalCrash),
		fmt.Sprintf("Gate: %s", gate),
		fmt.Sprintf("Money spent: %.4f HMC", moneySpent),
	}
	return map[string]any{
		"runs":            runsDone,
		"crashes":         crashCount,
		"critical":        criticalCrash,
		"gate":            gate,
		"gate_pass":       gatePass,
		"money_spent_hmc": moneySpent,
		"lines":           lines,
	}
}

func buildAssuranceNote(runsDone, crashCritical, crashHigh int, crashTypesChecked string) string {
	if crashTypesChecked == "" {
		crashTypesChecked = "crash/hang/ASan/memory"
	}
	base := fmt.Sprintf(
		"Not proven secure. None found of %s at %d runs (crash-critical=%d, crash-high=%d). Detector/property signals are coverage noise, not a security proof.",
		crashTypesChecked, runsDone, crashCritical, crashHigh,
	)
	if crashCritical > 0 || crashHigh > 0 {
		return fmt.Sprintf(
			"Not proven secure. Crash-class findings present (critical=%d, high=%d) after %d runs — fix and re-gate before release. Detector noise is appendix-only.",
			crashCritical, crashHigh, runsDone,
		)
	}
	return base
}

// moneySpentFromCampaign returns actual spend when known — never the locked budget.
// Prefer spent/paid keys; budget_* is intentionally ignored (misleading in verdict card).
func moneySpentFromCampaign(c fuzzCampaign) float64 {
	if c.Summary != nil {
		for _, k := range []string{"spent_hmc", "escrow_spent_hmc", "runs_paid_hmc", "paid_hmc"} {
			if v, ok := c.Summary[k]; ok {
				if f := floatFromAny(v); f > 0 {
					return f
				}
			}
		}
	}
	if c.Config != nil {
		for _, k := range []string{"paid_hmc", "spent_hmc", "escrow_spent_hmc"} {
			if v, ok := c.Config[k]; ok {
				if f := floatFromAny(v); f > 0 {
					return f
				}
			}
		}
	}
	return 0
}

// moneySpentFromEscrow is runs + bounty + crash-bonus paid (actual outflow).
func moneySpentFromEscrow(runsPaid, bountyPaid, crashBonusPaid float64) float64 {
	spent := runsPaid + bountyPaid + crashBonusPaid
	if spent < 0 {
		return 0
	}
	return spent
}

func floatFromAny(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case string:
		var f float64
		_, _ = fmt.Sscanf(strings.TrimSpace(x), "%f", &f)
		return f
	default:
		return 0
	}
}

// buildTargetFingerprint derives a stable WASM/binary hash from campaign config.
func buildTargetFingerprint(cfg map[string]any) map[string]any {
	out := map[string]any{
		"available": false,
		"note":      "no wasm/binary hash in campaign config",
	}
	if cfg == nil {
		return out
	}
	if h := strings.TrimSpace(strings.ToLower(toString(cfg["artifact_hash"]))); len(h) == 64 && isHex64(h) {
		out["available"] = true
		out["wasm_sha256"] = h
		out["source"] = "config.artifact_hash"
		delete(out, "note")
	}
	if h := strings.TrimSpace(strings.ToLower(toString(cfg["wasm_sha256"]))); len(h) == 64 && isHex64(h) {
		out["available"] = true
		out["wasm_sha256"] = h
		if toString(out["source"]) == "" {
			out["source"] = "config.wasm_sha256"
		}
		delete(out, "note")
	}
	if hexStr := strings.TrimSpace(toString(cfg["wasm_check_hex"])); hexStr != "" {
		raw, err := hex.DecodeString(strings.TrimPrefix(strings.ToLower(hexStr), "0x"))
		if err == nil && len(raw) > 0 {
			sum := sha256.Sum256(raw)
			got := hex.EncodeToString(sum[:])
			out["available"] = true
			out["wasm_sha256"] = got
			out["wasm_bytes"] = len(raw)
			out["source"] = "sha256(wasm_check_hex)"
			delete(out, "note")
		}
	}
	if h := strings.TrimSpace(strings.ToLower(toString(cfg["binary_sha256"]))); h != "" {
		out["available"] = true
		out["binary_sha256"] = h
		if toString(out["source"]) == "" {
			out["source"] = "config.binary_sha256"
		}
		delete(out, "note")
	}
	if h := strings.TrimSpace(strings.ToLower(toString(cfg["binary_hash"]))); h != "" {
		out["available"] = true
		out["binary_sha256"] = h
		if toString(out["source"]) == "" {
			out["source"] = "config.binary_hash"
		}
		delete(out, "note")
	}
	return out
}

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		ok := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !ok {
			return false
		}
	}
	return true
}

func baselineCampaignID(cfg map[string]any) string {
	if cfg == nil {
		return ""
	}
	for _, k := range []string{"base_campaign_id", "baseline_campaign_id", "prior_campaign_id"} {
		raw := strings.TrimSpace(toString(cfg[k]))
		if raw != "" {
			return cleanFuzzID(raw, "campaign")
		}
	}
	return ""
}

func stampTargetFingerprint(cfg map[string]any) {
	if cfg == nil {
		return
	}
	fp := buildTargetFingerprint(cfg)
	if fp["available"] != true {
		return
	}
	if v := toString(fp["wasm_sha256"]); v != "" {
		if toString(cfg["wasm_sha256"]) == "" {
			cfg["wasm_sha256"] = v
		}
		if toString(cfg["artifact_hash"]) == "" {
			cfg["artifact_hash"] = v
		}
	}
	if v := toString(fp["binary_sha256"]); v != "" && toString(cfg["binary_sha256"]) == "" {
		cfg["binary_sha256"] = v
	}
}

func stubBaselineDiff(reason string) map[string]any {
	return map[string]any{
		"available": false,
		"stub":      true,
		"note":      reason,
		"coverage_delta": map[string]any{
			"new_edges": nil,
			"new_paths": nil,
		},
		"new_issues":    []fuzzDiffItem{},
		"closed_issues": []fuzzDiffItem{},
	}
}

func estimatePulseETA(runsDone, budgetRuns int, elapsedSec int64, budgetSeconds int, status string) map[string]any {
	out := map[string]any{
		"eta_sec":         -1,
		"eta_source":      "none",
		"progress_note":   "",
		"honest_progress": true,
	}
	if status == "completed" || status == "cancelled" {
		out["eta_sec"] = 0
		out["eta_source"] = "terminal"
		out["progress_note"] = "campaign finished"
		return out
	}
	remainingRuns := budgetRuns - runsDone
	if remainingRuns < 0 {
		remainingRuns = 0
	}
	if budgetRuns > 0 && runsDone >= budgetRuns {
		out["eta_sec"] = 0
		out["eta_source"] = "budget_runs_met"
		out["progress_note"] = "run budget reached; awaiting completion"
		return out
	}
	rate := 0.0
	if elapsedSec > 0 && runsDone > 0 {
		rate = float64(runsDone) / float64(elapsedSec)
	}
	etaFromRate := -1
	if rate > 0 && remainingRuns > 0 {
		etaFromRate = int(float64(remainingRuns)/rate + 0.5)
	}
	etaFromWall := -1
	if budgetSeconds > 0 && elapsedSec >= 0 {
		left := int64(budgetSeconds) - elapsedSec
		if left < 0 {
			left = 0
		}
		etaFromWall = int(left)
	}
	switch {
	case etaFromRate >= 0 && (etaFromWall < 0 || etaFromRate <= etaFromWall):
		out["eta_sec"] = etaFromRate
		out["eta_source"] = "runs_per_sec"
		out["progress_note"] = fmt.Sprintf("~%d runs left at %.2f runs/s", remainingRuns, rate)
	case etaFromWall >= 0:
		out["eta_sec"] = etaFromWall
		out["eta_source"] = "budget_seconds"
		out["progress_note"] = "wall-clock budget remaining (run rate unknown or lower confidence)"
	default:
		out["progress_note"] = "ETA unavailable — need heartbeat progress (runs_done) and elapsed time"
		out["honest_progress"] = runsDone > 0 || elapsedSec > 0
	}
	out["remaining_runs"] = remainingRuns
	out["runs_per_sec"] = rate
	return out
}
