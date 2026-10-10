package poolfuzz

import (
	"fmt"
	"os"
	"strings"

	"hackme/internal/fuzzengine"
)

// Clean hygiene floor rejects empty/fake Dig CLEAN submits without full segment replay.
// Defaults are conservative so honest miners keep earning; tighten via env after fleet soak.
//
// Env knobs:
//
//	HACKME_POOL_CLEAN_FLOOR          default ON (0/false/off to disable)
//	HACKME_POOL_CLEAN_MIN_DURATION_MS  absolute floor (default 1 for multi-exec)
//	HACKME_POOL_CLEAN_MIN_MS_PER_EXEC  per-exec ms floor (default 0 = off)
//	HACKME_POOL_CLEAN_MIN_EDGES        min unique edges when worker reports edges_touched
//	                                   (default 1 for wasm_edge_bitmap; 0 = off)
//	HACKME_POOL_CLEAN_EDGES_REQUIRE    1 = reject when edges_touched omitted on bitmap campaigns
//	                                   (default OFF — soft rollout for old workers)

func cleanFloorEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_POOL_CLEAN_FLOOR")))
	if v == "" {
		return true
	}
	switch v {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func cleanEdgesRequireReported() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_POOL_CLEAN_EDGES_REQUIRE")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// checkCleanHygieneFloor validates Dig CLEAN hygiene_skip submits before pay.
// Legacy workers omit EdgesTouchedOK — soft rollout skips the edge floor unless REQUIRE=1.
func checkCleanHygieneFloor(req SubmitRequest, execPer int, cfg map[string]any) error {
	if !cleanFloorEnabled() || execPer <= 1 {
		return nil
	}
	minDur := envIntClamped("HACKME_POOL_CLEAN_MIN_DURATION_MS", 1, 0, 600000)
	msPer := envIntClamped("HACKME_POOL_CLEAN_MIN_MS_PER_EXEC", 0, 0, 1000)
	need := minDur
	if msPer > 0 {
		per := execPer * msPer
		if per > need {
			need = per
		}
	}
	if req.DurationMS < need {
		return fmt.Errorf("poolfuzz: clean_floor: duration_ms %d below floor %d (exec=%d)", req.DurationMS, need, execPer)
	}
	if !fuzzengine.CoverageUsesWasmEdge(cfg) {
		return nil
	}
	minEdges := envIntClamped("HACKME_POOL_CLEAN_MIN_EDGES", 1, 0, 100000)
	if minEdges <= 0 {
		return nil
	}
	if !req.EdgesTouchedOK {
		if cleanEdgesRequireReported() {
			return fmt.Errorf("poolfuzz: clean_floor: edges_touched required for wasm_edge_bitmap CLEAN")
		}
		return nil
	}
	if req.EdgesTouched < minEdges {
		return fmt.Errorf("poolfuzz: clean_floor: edges_touched %d below floor %d", req.EdgesTouched, minEdges)
	}
	return nil
}
