package poolfuzz

import (
	"context"
	"fmt"
	"time"

	"hackme/internal/fuzzengine"
)

func (s *Service) deriveGuidedPoolInputs(ctx context.Context, campaignID string, inputN uint64, cfg map[string]any, workerID string) (uint64, []byte, []fuzzengine.PoolCorpusSeed, error) {
	_ = workerID
	seeds, err := s.loadPoolCorpusSeeds(ctx, campaignID, fuzzengine.PoolCorpusMax(cfg))
	if err != nil {
		return 0, nil, nil, err
	}
	u, b := fuzzengine.GuidedInputForWork(inputN, cfg, seeds)
	return u, b, seeds, nil
}

func (s *Service) expectedInputsForSubmit(ctx context.Context, campaignID string, itemID int64, inputN uint64, cfg map[string]any) (uint64, []byte, error) {
	if !fuzzengine.GuidedSchedulingEnabled(cfg) {
		u, b := derivePoolInputs(inputN, cfg)
		return u, b, nil
	}
	u, b, locked, err := s.loadExpectedInputs(ctx, campaignID, itemID)
	if err != nil {
		return 0, nil, err
	}
	if !locked {
		return 0, nil, fmt.Errorf("poolfuzz: guided work item missing expected input (reclaim required)")
	}
	return u, b, nil
}

// ExpectedInputsForWork returns claim-frozen inputs (guided) or derived pool inputs.
func (s *Service) ExpectedInputsForWork(ctx context.Context, campaignID string, itemID int64, inputN uint64, cfg map[string]any) (uint64, []byte, error) {
	return s.expectedInputsForSubmit(ctx, campaignID, itemID, inputN, cfg)
}

// ObserveCorpusHit updates guided pool corpus after a local/pool work unit completes.
func (s *Service) ObserveCorpusHit(ctx context.Context, campaignID string, input uint64, inputBytes []byte, recordFinding bool, now int64) error {
	return s.observePoolCorpus(ctx, campaignID, input, inputBytes, recordFinding, now)
}

// LockGuidedWorkItem freezes anchor input and corpus snapshot at claim (pool + local autorun).
func (s *Service) LockGuidedWorkItem(ctx context.Context, campaignID string, itemID int64, inputN uint64, cfg map[string]any, workerID string, now int64) (uint64, []byte, []fuzzengine.PoolCorpusSeed, error) {
	if err := s.EnsureGuidedCorpusSeeded(ctx, campaignID, cfg, now); err != nil {
		return 0, nil, nil, err
	}
	actualU, actualB, seeds, err := s.deriveGuidedPoolInputs(ctx, campaignID, inputN, cfg, workerID)
	if err != nil {
		return 0, nil, nil, err
	}
	if err := s.storeExpectedInputs(ctx, campaignID, itemID, actualU, actualB); err != nil {
		return 0, nil, nil, err
	}
	if err := s.storeCorpusSnapshot(ctx, campaignID, itemID, seeds); err != nil {
		return 0, nil, nil, err
	}
	return actualU, actualB, seeds, nil
}

func (s *Service) buildClaimedWork(ctx context.Context, campaignID string, itemID int64, inputN uint64, cfg map[string]any, workerID string) (ClaimedWork, error) {
	wasmHex := wasmHexFromConfig(cfg)
	sem := fuzzengine.ParseCheckSemantics(cfg)
	var actualU uint64
	var actualB []byte
	var corpusSeeds []fuzzengine.PoolCorpusSeed
	var corpusSHA string
	if fuzzengine.GuidedSchedulingEnabled(cfg) {
		var err error
		actualU, actualB, corpusSeeds, err = s.LockGuidedWorkItem(ctx, campaignID, itemID, inputN, cfg, workerID, time.Now().Unix())
		if err != nil {
			return ClaimedWork{}, err
		}
		if _, corpusSHA, err = fuzzengine.EncodeCorpusSnapshot(corpusSeeds); err != nil {
			return ClaimedWork{}, err
		}
	} else {
		actualU, actualB = derivePoolInputs(inputN, cfg)
	}
	return ClaimedWork{
		WorkID:               fmt.Sprintf("%s:%d", campaignID, itemID),
		CampaignID:           campaignID,
		ItemID:               itemID,
		InputN:               inputN,
		ActualInput:          actualU,
		InputBytes:           actualB,
		InputMode:            string(fuzzengine.ParseInputMode(cfg)),
		WasmCheckHex:         wasmHex,
		CheckSemantics:       string(sem),
		DepthTier:            string(fuzzengine.ParseDepthTier(cfg)),
		PerRunHMC:            perRunHMCFromConfig(cfg),
		ExecPerUnit:          PoolExecPerUnit(cfg),
		MaxInputBytes:        fuzzengine.ParseMaxInputBytes(cfg),
		CoverageKind:         fuzzengine.CoverageKind(cfg),
		CorpusSeeds:          corpusSeeds,
		CorpusSnapshotSHA256: corpusSHA,
	}, nil
}
