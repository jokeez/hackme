package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// txChainIDForSigning is the chain_id a node-side signer must put in a transfer. A follower or
// desktop relaying to the canonical hub asks the hub (GET /api/status tx_chain_id): it usually
// has no Economics v2 activation env of its own, and signing with its local view would produce
// transfers the hub rejects with chain_id_mismatch from the activation height on.
func (a *app) txChainIDForSigning(ctx context.Context) string {
	local := a.chain.TxChainIDForNextBlock(ctx)
	if !a.shouldUseCanonicalChainAPI() {
		return local
	}
	base := strings.TrimRight(strings.TrimSpace(a.canonicalChainBaseURL()), "/")
	if !walletCanonicalBaseUsable(base) || a.canonicalBaseIsSelfNode(base) {
		return local
	}
	if id, ok := canonicalTxChainIDCache.get(base); ok {
		return id
	}
	if id, ok := fetchCanonicalTxChainID(ctx, base); ok {
		canonicalTxChainIDCache.put(base, id)
		return id
	}
	return local // hub unreachable: the relay fails anyway; never guess a foreign network id
}

type txChainIDCache struct {
	mu   sync.Mutex
	base string
	id   string
	at   time.Time
}

var canonicalTxChainIDCache txChainIDCache

func (c *txChainIDCache) get(base string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.base == base && time.Since(c.at) < 30*time.Second {
		return c.id, true
	}
	return "", false
}

func (c *txChainIDCache) put(base, id string) {
	c.mu.Lock()
	c.base, c.id, c.at = base, id, time.Now()
	c.mu.Unlock()
}

// fetchCanonicalTxChainID reads tx_chain_id from the hub; a pre-v2 hub omits it (legacy "").
func fetchCanonicalTxChainID(ctx context.Context, base string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/status?lite=1", nil)
	if err != nil {
		return "", false
	}
	resp, err := coordinatorHTTPClient().Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	var st struct {
		TxChainID *string `json:"tx_chain_id"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&st); err != nil {
		return "", false
	}
	if st.TxChainID == nil {
		return "", true
	}
	return strings.TrimSpace(*st.TxChainID), true
}
