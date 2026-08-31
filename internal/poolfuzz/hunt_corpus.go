package poolfuzz

import (
	"context"

	"hackme/internal/fuzzengine"
	"hackme/internal/hunt"
)

func (s *Service) seedHuntPoolCorpusBootstrap(ctx context.Context, campaignID string, cfg map[string]any, now int64) error {
	if s == nil || s.DB == nil || !hunt.HuntCorpusGuided(cfg) {
		return nil
	}
	n, err := s.poolCorpusSize(ctx, campaignID)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	for inputN := uint64(0); inputN < 16; inputN++ {
		b := hunt.ShardAnchorBytes(campaignID, inputN, cfg)
		u := fuzzengine.PackInputBytesToU64(b)
		edge, path := fuzzengine.CoverageBucketsFromBytes(b)
		if err := s.upsertPoolCorpusSeed(ctx, campaignID, u, b, 2, edge, path, false, now); err != nil {
			return err
		}
	}
	return s.cullPoolCorpus(ctx, campaignID, fuzzengine.PoolCorpusMax(cfg))
}

func (s *Service) lockHuntGuidedWorkItem(ctx context.Context, campaignID string, itemID int64, inputN uint64, cfg map[string]any, now int64) (uint64, []byte, []fuzzengine.PoolCorpusSeed, error) {
	if err := s.EnsureGuidedCorpusSeeded(ctx, campaignID, cfg, now); err != nil {
		return 0, nil, nil, err
	}
	if err := s.seedHuntPoolCorpusBootstrap(ctx, campaignID, cfg, now); err != nil {
		return 0, nil, nil, err
	}
	seeds, err := s.loadPoolCorpusSeeds(ctx, campaignID, fuzzengine.PoolCorpusMax(cfg))
	if err != nil {
		return 0, nil, nil, err
	}
	actualB := hunt.ShardSegmentExecInput(campaignID, inputN, 0, cfg, seeds)
	actualU := fuzzengine.PackInputBytesToU64(actualB)
	if err := s.storeExpectedInputs(ctx, campaignID, itemID, actualU, actualB); err != nil {
		return 0, nil, nil, err
	}
	if err := s.storeCorpusSnapshot(ctx, campaignID, itemID, seeds); err != nil {
		return 0, nil, nil, err
	}
	return actualU, actualB, seeds, nil
}
