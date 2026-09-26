package fuzzengine

import "crypto/sha256"

// MutationDepthStats summarizes uniqueness / depth of a mutation sample set.
type MutationDepthStats struct {
	Samples       int     `json:"samples"`
	UniqueSHA256  int     `json:"unique_sha256"`
	UniqueLens    int     `json:"unique_lengths"`
	AvgLen        float64 `json:"avg_len"`
	MaxLen        int     `json:"max_len"`
	MinLen        int     `json:"min_len"`
	UniqueRatio   float64 `json:"unique_ratio"`
	HavocSamples  int     `json:"havoc_samples"`
	HavocUnique   int     `json:"havoc_unique"`
	HavocUniqueRt float64 `json:"havoc_unique_ratio"`
}

// MeasureMutationDepth runs deterministic mutations and reports uniqueness metrics.
// Useful for A/B engine calibration (not a substitute for ASAN exec/s).
func MeasureMutationDepth(base []byte, dict []byte, corpus [][]byte, samples int, maxLen int) MutationDepthStats {
	if samples < 1 {
		samples = 1
	}
	if maxLen <= 0 {
		maxLen = DefaultMaxInputBytesStd
	}
	if len(base) == 0 {
		base = []byte(`{"a":1}`)
	}
	seen := map[string]struct{}{}
	lens := map[int]struct{}{}
	havocSeen := map[string]struct{}{}
	totalLen := 0
	minL, maxL := int(^uint(0)>>1), 0
	havocN := 0
	for i := 0; i < samples; i++ {
		stage := MutationStage(i % (StageDeterministicMax + HavocOpModulo))
		salt := uint64(i)*0x9E3779B97F4A7C15 + 0xDEAD
		out := mutateBytesWithDict(base, stage, salt, maxLen, dict, corpus)
		sum := sha256.Sum256(out)
		key := string(sum[:])
		seen[key] = struct{}{}
		lens[len(out)] = struct{}{}
		totalLen += len(out)
		if len(out) < minL {
			minL = len(out)
		}
		if len(out) > maxL {
			maxL = len(out)
		}
		if int(stage) >= StageHavocBase {
			havocN++
			havocSeen[key] = struct{}{}
		}
	}
	st := MutationDepthStats{
		Samples:      samples,
		UniqueSHA256: len(seen),
		UniqueLens:   len(lens),
		AvgLen:       float64(totalLen) / float64(samples),
		MaxLen:       maxL,
		MinLen:       minL,
		HavocSamples: havocN,
		HavocUnique:  len(havocSeen),
	}
	if samples > 0 {
		st.UniqueRatio = float64(st.UniqueSHA256) / float64(samples)
	}
	if havocN > 0 {
		st.HavocUniqueRt = float64(st.HavocUnique) / float64(havocN)
	}
	if minL == int(^uint(0)>>1) {
		st.MinLen = 0
	}
	return st
}

// EngineABReport compares upstream baseline havoc vs current engine on the same inputs.
type EngineABReport struct {
	Baseline MutationDepthStats `json:"baseline"`
	Current  MutationDepthStats `json:"current"`
	// UniqueGainPct = (current.unique - baseline.unique) / baseline.unique * 100
	UniqueGainPct float64 `json:"unique_gain_pct"`
	// LensGainPct = unique length diversity improvement
	LensGainPct float64 `json:"lens_gain_pct"`
}

// CompareEngineAB runs the same sample grid against baseline (v2.0 upstream) and current (v2.8).
func CompareEngineAB(base []byte, dict []byte, corpus [][]byte, samples int, maxLen int) EngineABReport {
	if samples < 1 {
		samples = 1
	}
	if maxLen <= 0 {
		maxLen = DefaultMaxInputBytesStd
	}
	if len(base) == 0 {
		base = []byte(`{"a":1}`)
	}
	baseSeen := map[string]struct{}{}
	curSeen := map[string]struct{}{}
	baseLens := map[int]struct{}{}
	curLens := map[int]struct{}{}
	for i := 0; i < samples; i++ {
		// Havoc-only grid: deterministic bitflips are identical upstream vs current.
		stage := MutationStage(StageHavocBase + (i % HavocOpModulo))
		salt := uint64(i)*0x9E3779B97F4A7C15 + 0xDEAD
		bOut := mutateBytesBaseline(base, stage, salt, maxLen, dict)
		cOut := mutateBytesWithDict(base, stage, salt, maxLen, dict, corpus)
		bs := sha256.Sum256(bOut)
		cs := sha256.Sum256(cOut)
		baseSeen[string(bs[:])] = struct{}{}
		curSeen[string(cs[:])] = struct{}{}
		baseLens[len(bOut)] = struct{}{}
		curLens[len(cOut)] = struct{}{}
	}
	bl := MutationDepthStats{Samples: samples, UniqueSHA256: len(baseSeen), UniqueLens: len(baseLens)}
	cr := MutationDepthStats{Samples: samples, UniqueSHA256: len(curSeen), UniqueLens: len(curLens)}
	if samples > 0 {
		bl.UniqueRatio = float64(bl.UniqueSHA256) / float64(samples)
		cr.UniqueRatio = float64(cr.UniqueSHA256) / float64(samples)
	}
	rep := EngineABReport{Baseline: bl, Current: cr}
	if bl.UniqueSHA256 > 0 {
		rep.UniqueGainPct = (float64(cr.UniqueSHA256-bl.UniqueSHA256) / float64(bl.UniqueSHA256)) * 100
	}
	if bl.UniqueLens > 0 {
		rep.LensGainPct = (float64(cr.UniqueLens-bl.UniqueLens) / float64(bl.UniqueLens)) * 100
	}
	return rep
}

// MeasureMutationDepthWithCFG is MeasureMutationDepth with optional deep-v28 cfg.
func MeasureMutationDepthWithCFG(base []byte, dict []byte, corpus [][]byte, samples int, maxLen int, cfg map[string]any) MutationDepthStats {
	if samples < 1 {
		samples = 1
	}
	if maxLen <= 0 {
		maxLen = DefaultMaxInputBytesStd
	}
	if len(base) == 0 {
		base = []byte(`{"a":1}`)
	}
	seen := map[string]struct{}{}
	lens := map[int]struct{}{}
	havocSeen := map[string]struct{}{}
	totalLen := 0
	minL, maxL := int(^uint(0)>>1), 0
	havocN := 0
	for i := 0; i < samples; i++ {
		stage := MutationStage(i % (StageDeterministicMax + 64))
		salt := uint64(i)*0x9E3779B97F4A7C15 + 0xDEAD
		var out []byte
		if cfg == nil {
			out = mutateBytesWithDict(base, stage, salt, maxLen, dict, corpus)
		} else {
			out = MutateBytesForHunt(base, stage, salt, maxLen, cfg, corpus)
		}
		sum := sha256.Sum256(out)
		key := string(sum[:])
		seen[key] = struct{}{}
		lens[len(out)] = struct{}{}
		totalLen += len(out)
		if len(out) < minL {
			minL = len(out)
		}
		if len(out) > maxL {
			maxL = len(out)
		}
		if int(stage) >= StageHavocBase {
			havocN++
			havocSeen[key] = struct{}{}
		}
	}
	st := MutationDepthStats{
		Samples:      samples,
		UniqueSHA256: len(seen),
		UniqueLens:   len(lens),
		AvgLen:       float64(totalLen) / float64(samples),
		MaxLen:       maxL,
		MinLen:       minL,
		HavocSamples: havocN,
		HavocUnique:  len(havocSeen),
	}
	if samples > 0 {
		st.UniqueRatio = float64(st.UniqueSHA256) / float64(samples)
	}
	if havocN > 0 {
		st.HavocUniqueRt = float64(st.HavocUnique) / float64(havocN)
	}
	if minL == int(^uint(0)>>1) {
		st.MinLen = 0
	}
	return st
}

// DeepV28Report is an honest A/B of core v2.7 vs deep v2.8 on the same salts.
type DeepV28Report struct {
	Samples       int     `json:"samples"`
	DiffRate      float64 `json:"diff_rate"`      // fraction where deep != core
	AvgByteDelta  float64 `json:"avg_byte_delta"` // mean absolute byte diffs (padded)
	AvgLenDelta   float64 `json:"avg_len_delta"`  // mean |len(deep)-len(core)|
	CoreUnique    int     `json:"core_unique"`
	DeepUnique    int     `json:"deep_unique"`
	UniqueGainPct float64 `json:"unique_gain_pct"`
	CoreEdges     int     `json:"core_structural_edges"`
	DeepEdges     int     `json:"deep_structural_edges"`
	EdgeGainPct   float64 `json:"edge_gain_pct"`
	CoreLens      int     `json:"core_unique_lens"`
	DeepLens      int     `json:"deep_unique_lens"`
	LensGainPct   float64 `json:"lens_gain_pct"`
	// Saturated grid (few stages × many salts) — uniqueness should climb with depth.
	SatCoreUnique int     `json:"sat_core_unique"`
	SatDeepUnique int     `json:"sat_deep_unique"`
	SatUniqueGain float64 `json:"sat_unique_gain_pct"`
}

// CompareDeepV28Detailed measures how much deeper v2.8 digs vs v2.7 core.
func CompareDeepV28Detailed(base []byte, dict []byte, corpus [][]byte, samples int, maxLen int) DeepV28Report {
	if samples < 1 {
		samples = 1
	}
	if maxLen <= 0 {
		maxLen = DefaultMaxInputBytesStd
	}
	if len(base) == 0 {
		base = []byte(`{"a":1}`)
	}
	cfgDeep := map[string]any{"havoc_deep_v28": true, "mutator_dict": dict}
	cfgCore := map[string]any{"mutator_dict": dict}
	coreSeen := map[string]struct{}{}
	deepSeen := map[string]struct{}{}
	coreEdges := map[int]struct{}{}
	deepEdges := map[int]struct{}{}
	coreLens := map[int]struct{}{}
	deepLens := map[int]struct{}{}
	diffN := 0
	byteDelta := 0.0
	lenDelta := 0.0
	for i := 0; i < samples; i++ {
		stage := MutationStage(StageHavocBase + (i % 64))
		salt := uint64(i)*0x9E3779B97F4A7C15 + 0xDEAD
		cOut := MutateBytesForHunt(base, stage, salt, maxLen, cfgCore, corpus)
		dOut := MutateBytesForHunt(base, stage, salt, maxLen, cfgDeep, corpus)
		cs := sha256.Sum256(cOut)
		ds := sha256.Sum256(dOut)
		coreSeen[string(cs[:])] = struct{}{}
		deepSeen[string(ds[:])] = struct{}{}
		coreLens[len(cOut)] = struct{}{}
		deepLens[len(dOut)] = struct{}{}
		ce, _ := CoverageBucketsStructural(cOut)
		de, _ := CoverageBucketsStructural(dOut)
		coreEdges[ce] = struct{}{}
		deepEdges[de] = struct{}{}
		if string(cOut) != string(dOut) {
			diffN++
		}
		byteDelta += float64(byteAbsDelta(cOut, dOut))
		if len(dOut) > len(cOut) {
			lenDelta += float64(len(dOut) - len(cOut))
		} else {
			lenDelta += float64(len(cOut) - len(dOut))
		}
	}
	// Saturated: only 8 havoc stages — uniqueness plateaus; deep should still climb.
	satCore := map[string]struct{}{}
	satDeep := map[string]struct{}{}
	satN := samples
	if satN > 3000 {
		satN = 3000
	}
	for i := 0; i < satN; i++ {
		stage := MutationStage(StageHavocBase + (i % 8))
		salt := uint64(i)*0x9E3779B97F4A7C15 + 0xBEEF
		cOut := MutateBytesForHunt(base, stage, salt, maxLen, cfgCore, corpus)
		dOut := MutateBytesForHunt(base, stage, salt, maxLen, cfgDeep, corpus)
		cs := sha256.Sum256(cOut)
		ds := sha256.Sum256(dOut)
		satCore[string(cs[:])] = struct{}{}
		satDeep[string(ds[:])] = struct{}{}
	}
	rep := DeepV28Report{
		Samples:       samples,
		DiffRate:      float64(diffN) / float64(samples),
		AvgByteDelta:  byteDelta / float64(samples),
		AvgLenDelta:   lenDelta / float64(samples),
		CoreUnique:    len(coreSeen),
		DeepUnique:    len(deepSeen),
		CoreEdges:     len(coreEdges),
		DeepEdges:     len(deepEdges),
		CoreLens:      len(coreLens),
		DeepLens:      len(deepLens),
		SatCoreUnique: len(satCore),
		SatDeepUnique: len(satDeep),
	}
	if rep.CoreUnique > 0 {
		rep.UniqueGainPct = (float64(rep.DeepUnique-rep.CoreUnique) / float64(rep.CoreUnique)) * 100
	}
	if rep.CoreEdges > 0 {
		rep.EdgeGainPct = (float64(rep.DeepEdges-rep.CoreEdges) / float64(rep.CoreEdges)) * 100
	}
	if rep.CoreLens > 0 {
		rep.LensGainPct = (float64(rep.DeepLens-rep.CoreLens) / float64(rep.CoreLens)) * 100
	}
	if rep.SatCoreUnique > 0 {
		rep.SatUniqueGain = (float64(rep.SatDeepUnique-rep.SatCoreUnique) / float64(rep.SatCoreUnique)) * 100
	}
	return rep
}

func byteAbsDelta(a, b []byte) int {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	d := 0
	for i := 0; i < n; i++ {
		var x, y byte
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x > y {
			d += int(x - y)
		} else {
			d += int(y - x)
		}
	}
	// Length mismatch counts as extra distance.
	if len(a) > len(b) {
		d += len(a) - len(b)
	} else {
		d += len(b) - len(a)
	}
	return d
}

// CompareDeepV28AB keeps EngineABReport shape for older callers (unique/lens only).
func CompareDeepV28AB(base []byte, dict []byte, corpus [][]byte, samples int, maxLen int) EngineABReport {
	d := CompareDeepV28Detailed(base, dict, corpus, samples, maxLen)
	bl := MutationDepthStats{Samples: samples, UniqueSHA256: d.CoreUnique, UniqueLens: d.CoreLens}
	cr := MutationDepthStats{Samples: samples, UniqueSHA256: d.DeepUnique, UniqueLens: d.DeepLens}
	if samples > 0 {
		bl.UniqueRatio = float64(bl.UniqueSHA256) / float64(samples)
		cr.UniqueRatio = float64(cr.UniqueSHA256) / float64(samples)
	}
	return EngineABReport{
		Baseline: bl, Current: cr,
		UniqueGainPct: d.UniqueGainPct, LensGainPct: d.LensGainPct,
	}
}

// MeasureMutationDepthMulti averages uniqueness across several format bases (local stress).
func MeasureMutationDepthMulti(samplesPerBase, maxLen int) MutationDepthStats {
	bases := [][]byte{
		[]byte(`{"a":1,"nested":{"b":[1,2,3],"tag":"<x/>"}}`),
		[]byte(`<root attr="1"><child>text</child></root>`),
		[]byte("GET /api?x=1 HTTP/1.1\r\nHost: t\r\n\r\n"),
		{0x00, 0x01, 0x02, 0xff, 0xfe, 0x7f, 0x80, 0x00, 0x10, 0x20},
		[]byte("%PDF-1.4\n1 0 obj<<>>endobj"),
	}
	dict := []byte(`"null""true""false""a""nested"{}[]<>`)
	corpus := [][]byte{
		[]byte(`{}`), []byte(`[1,2]`), []byte(`{"x":null}`),
		[]byte(`<root/>`), []byte("\x1f\x8b\x08\x00"),
	}
	agg := MutationDepthStats{}
	for _, base := range bases {
		st := MeasureMutationDepth(base, dict, corpus, samplesPerBase, maxLen)
		agg.Samples += st.Samples
		agg.UniqueSHA256 += st.UniqueSHA256
		agg.UniqueLens += st.UniqueLens
		agg.AvgLen += st.AvgLen
		if st.MaxLen > agg.MaxLen {
			agg.MaxLen = st.MaxLen
		}
		agg.HavocSamples += st.HavocSamples
		agg.HavocUnique += st.HavocUnique
	}
	n := len(bases)
	if n > 0 {
		agg.AvgLen /= float64(n)
	}
	if agg.Samples > 0 {
		agg.UniqueRatio = float64(agg.UniqueSHA256) / float64(agg.Samples)
	}
	if agg.HavocSamples > 0 {
		agg.HavocUniqueRt = float64(agg.HavocUnique) / float64(agg.HavocSamples)
	}
	return agg
}
