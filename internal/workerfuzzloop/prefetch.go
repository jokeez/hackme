package workerfuzzloop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
)

// prefetchBuf holds at most one extra leased claim for double-buffer Dig.
type prefetchBuf struct {
	mu   sync.Mutex
	next *ClaimResp
}

func prefetchEnabled(cfg Config) bool {
	if cfg.Prefetch != nil {
		return *cfg.Prefetch
	}
	v := os.Getenv("HACKME_WORKER_PREFETCH")
	if v == "" {
		return true
	}
	return !Falsy(v)
}

func (p *prefetchBuf) take() *ClaimResp {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	cr := p.next
	p.next = nil
	return cr
}

func (p *prefetchBuf) tryPut(cr *ClaimResp) bool {
	if p == nil || cr == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.next != nil {
		return false
	}
	p.next = cr
	return true
}

// FetchCorpusSnapshot loads frozen corpus seeds for a light claim (lease-gated).
func FetchCorpusSnapshot(ctx context.Context, cl *http.Client, base, token, workerID, campaignID string, itemID int64) ([]map[string]any, string, error) {
	body, _ := json.Marshal(map[string]any{
		"worker_id":   workerID,
		"campaign_id": campaignID,
		"item_id":     itemID,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/api/fuzz/work/corpus_snapshot", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", token)
	res, err := cl.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	var wrap struct {
		OK                   bool             `json:"ok"`
		Reason               string           `json:"reason,omitempty"`
		CorpusSeeds          []map[string]any `json:"corpus_seeds"`
		CorpusSnapshotSHA256 string           `json:"corpus_snapshot_sha256"`
	}
	_ = json.Unmarshal(raw, &wrap)
	if res.StatusCode != http.StatusOK || !wrap.OK {
		if wrap.Reason != "" {
			return nil, "", fmt.Errorf("%s", wrap.Reason)
		}
		return nil, "", fmt.Errorf("HTTP %d %s", res.StatusCode, shortHTTPBody(res.StatusCode, raw))
	}
	return wrap.CorpusSeeds, wrap.CorpusSnapshotSHA256, nil
}

func ensureCorpusSeeds(ctx context.Context, cfg Config, base string, cr *ClaimResp) error {
	if cr == nil {
		return nil
	}
	if len(cr.CorpusSeeds) > 0 {
		return nil
	}
	if !cr.CorpusLight && strings.TrimSpace(cr.CorpusSnapshotSHA256) == "" {
		return nil
	}
	seeds, sha, err := FetchCorpusSnapshot(ctx, cfg.HTTPClient, base, cfg.Token, cfg.WorkerID, cr.CampaignID, cr.ItemID)
	if err != nil {
		return err
	}
	cr.CorpusSeeds = seeds
	if sha != "" {
		cr.CorpusSnapshotSHA256 = sha
	}
	cr.CorpusLight = false
	return nil
}

func startPrefetch(ctx context.Context, cfg Config, base string, buf *prefetchBuf) {
	if !prefetchEnabled(cfg) || buf == nil {
		return
	}
	go func() {
		cr, err := Claim(ctx, cfg.HTTPClient, base, cfg.Token, cfg.WorkerID, cfg.PubHex, cfg.MinerAddr, claimCaps(cfg))
		if err != nil || !cr.OK {
			return
		}
		cpy := cr
		if !buf.tryPut(&cpy) {
			_ = ReleaseLease(ctx, cfg.HTTPClient, base, cfg.Token, cfg.WorkerID, cpy.CampaignID, cpy.ItemID, cfg.PubHex, cfg.MinerAddr)
		}
	}()
}
