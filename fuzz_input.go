package main

import (
	"hackme/internal/fuzzengine"
)

// FuzzEngineVersion is reported in campaign summaries and customer reports.
const FuzzEngineVersion = fuzzengine.Version

var defaultFuzzSeedCorpus = fuzzengine.DefaultSeedCorpus

func normalizeFuzzCampaignConfig(cfg map[string]any, campaignType string) map[string]any {
	return fuzzengine.NormalizeCampaignConfig(cfg, campaignType)
}

func parseSeedCorpus(cfg map[string]any) []uint64 {
	return fuzzengine.ParseSeedCorpus(cfg)
}

func mutationRoundsFromConfig(cfg map[string]any) int {
	return fuzzengine.MutationRounds(cfg)
}

func deriveFuzzInput(inputN uint64, cfg map[string]any) uint64 {
	return fuzzengine.DeriveInput(inputN, cfg)
}

func fuzzInputSHA256(input uint64) string {
	return fuzzengine.InputSHA256(input)
}

func fuzzInputReproCmd(input uint64) string {
	return fuzzengine.ReproCmd(input)
}

func fuzzCoverageBuckets(input uint64) (edgeBucket, pathBucket int) {
	return fuzzengine.CoverageBuckets(input)
}

func fuzzEngineMetaFromConfig(cfg map[string]any) map[string]any {
	return fuzzengine.MetaFromConfig(cfg)
}

func wasmCheckInputParts(n uint64) (opType int, itemID int, quantity int64) {
	return fuzzengine.WasmCheckInputParts(n)
}
