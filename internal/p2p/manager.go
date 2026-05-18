package p2p

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"hackme/internal/block"
)

type TipSnapshot struct {
	NodeID      string `json:"node_id"`
	Height      uint64 `json:"height"`
	Tip         string `json:"tip_hash"`
	SeenAt      int64  `json:"seen_at"`
	AnnounceURL string `json:"announce_url,omitempty"`
	PolicyHash  string `json:"policy_hash,omitempty"`
}

type Manager struct {
	nodeID      string
	token       string
	client      *http.Client
	discoveryOn bool
	advertise   string
	policyHash  string
	maxPeers    int

	mu    sync.Mutex
	peers map[string]PeerStatus
	list  []string
	seen  map[string]int64
}

type PeerStatus struct {
	PeerURL             string `json:"peer_url"`
	NodeID              string `json:"node_id"`
	Height              uint64 `json:"height"`
	Tip                 string `json:"tip_hash"`
	SeenAt              int64  `json:"seen_at"`
	LastAttemptUnix     int64  `json:"last_attempt_unix"`
	LastOKUnix          int64  `json:"last_ok_unix"`
	NextRetryUnix       int64  `json:"next_retry_unix,omitempty"`
	LastLatencyMS       int64  `json:"last_latency_ms,omitempty"`
	Healthy             bool   `json:"healthy"`
	LastError           string `json:"last_error,omitempty"`
	ConsecutiveFailures uint64 `json:"consecutive_failures"`
	FlapCount           uint64 `json:"flap_count"`
	LastStateChangeUnix int64  `json:"last_state_change_unix,omitempty"`
	Unstable            bool   `json:"unstable"`
	QualityScore        int    `json:"quality_score"`
	Quality             string `json:"quality"`
	Source              string `json:"source"`
}

type SyncHint struct {
	LocalHeight   uint64 `json:"local_height"`
	MaxPeerHeight uint64 `json:"max_peer_height"`
	LagBlocks     uint64 `json:"lag_blocks"`
	SyncNeeded    bool   `json:"sync_needed"`
	BestPeerURL   string `json:"best_peer_url,omitempty"`
	BestPeerNode  string `json:"best_peer_node,omitempty"`
}

type SyncPullPreview struct {
	PeerURL     string   `json:"peer_url,omitempty"`
	PlanReady   bool     `json:"plan_ready"`
	Reason      string   `json:"reason,omitempty"`
	DepthLimit  uint64   `json:"depth_limit"`
	PlannedFrom uint64   `json:"planned_from_height"`
	PlannedTo   uint64   `json:"planned_to_height"`
	PlannedCnt  int      `json:"planned_count"`
	Hashes      []string `json:"hashes,omitempty"`
}

func policyCompatible(localPolicyHash, remotePolicyHash string) bool {
	local := strings.TrimSpace(localPolicyHash)
	remote := strings.TrimSpace(remotePolicyHash)
	if local == "" {
		return true
	}
	if remote == "" {
		return false
	}
	return strings.EqualFold(local, remote)
}

func NewManager(nodeID string, peers []string, token string, discovery bool, advertiseURL string, policyHash string) *Manager {
	out := make([]string, 0, len(peers))
	for _, p := range peers {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		norm := normalizePeerURL(p)
		if norm == "" {
			continue
		}
		out = append(out, norm)
	}
	pm := make(map[string]PeerStatus, len(out))
	for _, peer := range out {
		pm[peer] = PeerStatus{PeerURL: peer, Source: "static"}
	}
	advertiseURL = normalizePeerURL(advertiseURL)
	return &Manager{
		nodeID:      nodeID,
		token:       strings.TrimSpace(token),
		client:      &http.Client{Timeout: 5 * time.Second},
		discoveryOn: discovery,
		advertise:   advertiseURL,
		policyHash:  strings.TrimSpace(policyHash),
		maxPeers:    maxPeerCap(len(out)),
		peers:       pm,
		list:        out,
		seen:        make(map[string]int64, 256),
	}
}

func (m *Manager) Enabled() bool { return m != nil && (len(m.list) > 0 || m.discoveryOn) }
func (m *Manager) DiscoveryEnabled() bool {
	return m != nil && m.discoveryOn
}

func (m *Manager) headers(h http.Header) {
	if m.token != "" {
		h.Set("X-Hackme-P2P-Token", m.token)
	}
}

func (m *Manager) Start(ctx context.Context, tipFn func(context.Context) (uint64, string, error)) {
	if !m.Enabled() || tipFn == nil {
		return
	}
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				h, tip, err := tipFn(ctx)
				if err != nil {
					continue
				}
				body, _ := json.Marshal(TipSnapshot{
					NodeID:      m.nodeID,
					Height:      h,
					Tip:         tip,
					SeenAt:      time.Now().Unix(),
					AnnounceURL: m.advertise,
					PolicyHash:  m.policyHash,
				})
				for _, peer := range m.peerListSnapshot() {
					now := time.Now().Unix()
					st := PeerStatus{
						PeerURL:         peer,
						LastAttemptUnix: now,
					}
					m.mu.Lock()
					prev, hadPrev := m.peers[peer]
					if hadPrev {
						st = prev
					}
					if st.Source == "" {
						st.Source = "static"
					}
					// Respect per-peer retry backoff when a peer is flapping/down.
					if st.NextRetryUnix > now {
						m.peers[peer] = st
						m.mu.Unlock()
						continue
					}
					st.LastAttemptUnix = now
					m.peers[peer] = st
					m.mu.Unlock()

					start := time.Now()
					req, _ := http.NewRequestWithContext(ctx, http.MethodPost, peer+"/api/p2p/handshake", bytes.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					m.headers(req.Header)
					resp, err := m.client.Do(req)
					if err != nil {
						st.LastError = strings.TrimSpace(err.Error())
						st.ConsecutiveFailures++
						st.NextRetryUnix = now + failureBackoffSec(st.ConsecutiveFailures)
						st.applyHealthTransition(false, now)
						st.updateQuality()
						m.mu.Lock()
						m.peers[peer] = st
						m.mu.Unlock()
						continue
					}
					var snap TipSnapshot
					raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
					_ = resp.Body.Close()
					st.LastLatencyMS = int64(time.Since(start) / time.Millisecond)
					if resp.StatusCode/100 != 2 {
						st.LastError = "http " + resp.Status
						st.ConsecutiveFailures++
						st.NextRetryUnix = now + failureBackoffSec(st.ConsecutiveFailures)
						st.applyHealthTransition(false, now)
						st.updateQuality()
						m.mu.Lock()
						m.peers[peer] = st
						m.mu.Unlock()
						continue
					}
					if err := json.Unmarshal(raw, &snap); err != nil {
						st.LastError = "decode: " + strings.TrimSpace(err.Error())
						st.ConsecutiveFailures++
						st.NextRetryUnix = now + failureBackoffSec(st.ConsecutiveFailures)
						st.applyHealthTransition(false, now)
						st.updateQuality()
						m.mu.Lock()
						m.peers[peer] = st
						m.mu.Unlock()
						continue
					}
					if !policyCompatible(m.policyHash, snap.PolicyHash) {
						st.LastError = "policy_mismatch"
						st.ConsecutiveFailures++
						st.NextRetryUnix = now + failureBackoffSec(st.ConsecutiveFailures)
						st.applyHealthTransition(false, now)
						st.updateQuality()
						m.mu.Lock()
						m.peers[peer] = st
						m.mu.Unlock()
						continue
					}
					st.PeerURL = peer
					st.NodeID = snap.NodeID
					st.Height = snap.Height
					st.Tip = snap.Tip
					st.SeenAt = snap.SeenAt
					st.LastOKUnix = now
					st.NextRetryUnix = 0
					st.LastError = ""
					st.ConsecutiveFailures = 0
					st.applyHealthTransition(true, now)
					st.updateQuality()
					m.mu.Lock()
					m.peers[peer] = st
					m.mu.Unlock()
					// Discovery can grow peer set transitively from already trusted peers.
					m.LearnDiscoveredPeer(snap.AnnounceURL)
				}
			}
		}
	}()
}

func failureBackoffSec(fails uint64) int64 {
	// Exponential backoff with practical caps for LAN flapping peers.
	if fails == 0 {
		return 0
	}
	if fails > 10 {
		fails = 10
	}
	sec := int64(1 << (fails - 1)) // 1,2,4,...,512
	if sec < 5 {
		sec = 5
	}
	if sec > 120 {
		sec = 120
	}
	return sec
}

func normalizePeerURL(in string) string {
	in = strings.TrimSpace(strings.TrimRight(in, "/"))
	if in == "" {
		return ""
	}
	u, err := url.Parse(in)
	if err != nil {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	if strings.TrimSpace(u.Host) == "" {
		return ""
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/")
}

func maxPeerCap(seedCount int) int {
	base := 128
	if seedCount > 0 && seedCount*8 > base {
		base = seedCount * 8
	}
	if base > 512 {
		base = 512
	}
	return base
}

func (m *Manager) peerListSnapshot() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.list))
	copy(out, m.list)
	return out
}

func (m *Manager) LearnDiscoveredPeer(rawPeer string) bool {
	if m == nil || !m.discoveryOn {
		return false
	}
	peer := normalizePeerURL(rawPeer)
	if peer == "" {
		return false
	}
	if m.advertise != "" && peer == m.advertise {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.peers[peer]; ok {
		return false
	}
	if m.maxPeers > 0 && len(m.list) >= m.maxPeers {
		return false
	}
	m.list = append(m.list, peer)
	m.peers[peer] = PeerStatus{PeerURL: peer, Source: "discovered"}
	return true
}

func (s *PeerStatus) applyHealthTransition(nextHealthy bool, now int64) {
	if s.LastStateChangeUnix == 0 && s.FlapCount == 0 {
		s.LastStateChangeUnix = now
		s.Healthy = nextHealthy
		return
	}
	if s.Healthy != nextHealthy {
		s.FlapCount++
		s.LastStateChangeUnix = now
	}
	s.Healthy = nextHealthy
}

func (s *PeerStatus) updateQuality() {
	if s.LastAttemptUnix == 0 {
		s.Unstable = false
		s.QualityScore = 0
		s.Quality = "unknown"
		return
	}
	score := 100
	if !s.Healthy {
		score -= 40
	}
	if s.ConsecutiveFailures > 0 {
		penalty := int(s.ConsecutiveFailures) * 8
		if penalty > 32 {
			penalty = 32
		}
		score -= penalty
	}
	if s.LastLatencyMS > 0 {
		switch {
		case s.LastLatencyMS > 2000:
			score -= 25
		case s.LastLatencyMS > 800:
			score -= 15
		case s.LastLatencyMS > 400:
			score -= 8
		}
	}
	if s.FlapCount >= 3 {
		score -= 12
	}
	if score < 0 {
		score = 0
	}
	s.Unstable = s.FlapCount >= 3 || s.ConsecutiveFailures >= 3
	s.QualityScore = score
	switch {
	case score >= 80:
		s.Quality = "good"
	case score >= 55:
		s.Quality = "warning"
	default:
		s.Quality = "bad"
	}
}

func (m *Manager) PeerSnapshots() []PeerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]PeerStatus, 0, len(m.peers))
	for _, s := range m.peers {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Healthy != out[j].Healthy {
			return out[i].Healthy
		}
		return out[i].PeerURL < out[j].PeerURL
	})
	return out
}

func (m *Manager) BuildSyncHint(localHeight uint64, localTip string) SyncHint {
	hint := SyncHint{
		LocalHeight:   localHeight,
		MaxPeerHeight: localHeight,
		LagBlocks:     0,
		SyncNeeded:    false,
	}
	if m == nil {
		return hint
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	localTip = strings.TrimSpace(localTip)
	bestBootstrapPeer := PeerStatus{}
	for _, p := range m.peers {
		if !p.Healthy {
			continue
		}
		if p.Height > hint.MaxPeerHeight {
			hint.MaxPeerHeight = p.Height
			hint.BestPeerURL = p.PeerURL
			hint.BestPeerNode = p.NodeID
		}
		// Bootstrap case: local chain has no tip yet, but peer already has genesis
		// at the same height (0). Treat it as sync-needed so follower can import
		// block #0 instead of stalling on "no_lag_or_no_healthy_peer".
		if localHeight == 0 && localTip == "" && p.Height == 0 && strings.TrimSpace(p.Tip) != "" {
			if bestBootstrapPeer.PeerURL == "" || p.PeerURL < bestBootstrapPeer.PeerURL {
				bestBootstrapPeer = p
			}
		}
	}
	if hint.MaxPeerHeight > localHeight {
		hint.LagBlocks = hint.MaxPeerHeight - localHeight
		hint.SyncNeeded = true
	} else if localHeight == 0 && localTip == "" && bestBootstrapPeer.PeerURL != "" {
		hint.SyncNeeded = true
		hint.LagBlocks = 1
		hint.BestPeerURL = bestBootstrapPeer.PeerURL
		hint.BestPeerNode = bestBootstrapPeer.NodeID
	}
	return hint
}

func (m *Manager) BuildLinearPullPreview(ctx context.Context, localHeight uint64, localTip string, depthLimit uint64) SyncPullPreview {
	if depthLimit == 0 {
		depthLimit = 64
	}
	if depthLimit > 512 {
		depthLimit = 512
	}
	out := SyncPullPreview{
		PlanReady:   false,
		DepthLimit:  depthLimit,
		PlannedFrom: localHeight + 1,
	}
	hint := m.BuildSyncHint(localHeight, localTip)
	if !hint.SyncNeeded || hint.BestPeerURL == "" {
		out.Reason = "no_lag_or_no_healthy_peer"
		return out
	}
	out.PeerURL = hint.BestPeerURL

	limit := depthLimit + 1
	if limit < 2 {
		limit = 2
	}
	fwd := localHeight + 1
	if localHeight == 0 && strings.TrimSpace(localTip) == "" {
		fwd = 0
	}
	chainPath := "/api/chain?from_height=" + strconv.FormatUint(fwd, 10) + "&limit=" + strconv.FormatUint(limit, 10)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, hint.BestPeerURL+chainPath, nil)
	m.headers(req.Header)
	resp, err := m.client.Do(req)
	if err != nil {
		out.Reason = "fetch_failed: " + strings.TrimSpace(err.Error())
		return out
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	_ = resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		out.Reason = "http " + resp.Status
		return out
	}
	var payload struct {
		Blocks []json.RawMessage `json:"blocks"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		out.Reason = "decode_failed"
		return out
	}
	if len(payload.Blocks) == 0 {
		out.Reason = "empty_remote_chain_window"
		return out
	}
	type hdr struct {
		Index    uint64 `json:"index"`
		Hash     string `json:"hash"`
		PrevHash string `json:"prev_hash"`
	}
	headers := make([]hdr, 0, len(payload.Blocks))
	for _, b := range payload.Blocks {
		var h hdr
		if err := json.Unmarshal(b, &h); err != nil {
			continue
		}
		if h.Hash == "" {
			continue
		}
		headers = append(headers, h)
	}
	if len(headers) == 0 {
		out.Reason = "no_valid_headers"
		return out
	}
	// Bootstrap case: local DB is empty (no genesis yet), but healthy peer has
	// genesis on height 0. Import block #0 first to anchor subsequent sync.
	if localHeight == 0 && strings.TrimSpace(localTip) == "" {
		for _, h := range headers {
			if h.Index == 0 && strings.TrimSpace(h.Hash) != "" {
				out.Hashes = append(out.Hashes, h.Hash)
				out.PlanReady = true
				out.Reason = "bootstrap_genesis_from_peer"
				out.PlannedFrom = 0
				out.PlannedTo = 0
				out.PlannedCnt = 1
				return out
			}
		}
		out.Reason = "peer_missing_genesis_block_0"
		return out
	}
	// Find first header that could extend local tip directly.
	start := -1
	for i, h := range headers {
		if h.Index == localHeight+1 && strings.TrimSpace(h.PrevHash) == strings.TrimSpace(localTip) {
			start = i
			break
		}
	}
	if start < 0 {
		out.Reason = "no_direct_tail_match"
		return out
	}
	prevHash := strings.TrimSpace(localTip)
	wantIdx := localHeight + 1
	for i := start; i < len(headers); i++ {
		h := headers[i]
		if h.Index != wantIdx || strings.TrimSpace(h.PrevHash) != prevHash {
			break
		}
		out.Hashes = append(out.Hashes, h.Hash)
		prevHash = h.Hash
		wantIdx++
		if uint64(len(out.Hashes)) >= depthLimit {
			break
		}
	}
	if len(out.Hashes) == 0 {
		out.Reason = "tail_validation_failed"
		return out
	}
	out.PlanReady = true
	out.PlannedCnt = len(out.Hashes)
	out.PlannedTo = localHeight + uint64(out.PlannedCnt)
	if out.PlannedTo < hint.MaxPeerHeight {
		out.Reason = fmt.Sprintf("depth_limit_reached_or_window_short (%d/%d)", out.PlannedCnt, hint.LagBlocks)
	}
	return out
}

func (m *Manager) FetchBlocksByHashes(ctx context.Context, peerURL string, hashes []string) ([]json.RawMessage, error) {
	if m == nil {
		return nil, fmt.Errorf("p2p manager is nil")
	}
	peerURL = normalizePeerURL(peerURL)
	if peerURL == "" {
		return nil, fmt.Errorf("invalid peer url")
	}
	if len(hashes) == 0 {
		return nil, nil
	}
	limit := len(hashes) + 4
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, peerURL+"/api/chain?limit="+strconv.Itoa(limit), nil)
	m.headers(req.Header)
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	_ = resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("fetch blocks: http %s", resp.Status)
	}
	var payload struct {
		Blocks []json.RawMessage `json:"blocks"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	byHash := make(map[string]json.RawMessage, len(payload.Blocks))
	for _, one := range payload.Blocks {
		var b block.Block
		if err := json.Unmarshal(one, &b); err != nil {
			continue
		}
		if strings.TrimSpace(b.Hash) == "" {
			continue
		}
		// Basic integrity check before staging any remote block.
		if b.HeaderHashHex() != strings.TrimSpace(b.Hash) {
			continue
		}
		byHash[b.Hash] = one
	}
	out := make([]json.RawMessage, 0, len(hashes))
	for _, h := range hashes {
		rawBlock, ok := byHash[h]
		if !ok {
			return nil, fmt.Errorf("missing planned block %s from peer response", h)
		}
		out = append(out, rawBlock)
	}
	return out, nil
}

func (m *Manager) BroadcastTx(ctx context.Context, raw []byte) {
	m.RelayTx(ctx, raw)
}

func txDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (m *Manager) shouldRelayTx(raw []byte, now int64) bool {
	if len(raw) == 0 {
		return false
	}
	key := txDigest(raw)
	const seenTTL = int64(120)
	m.mu.Lock()
	defer m.mu.Unlock()
	if exp, ok := m.seen[key]; ok && exp >= now {
		return false
	}
	// Opportunistic prune on writes to keep cache bounded.
	if len(m.seen) > 4096 {
		for k, exp := range m.seen {
			if exp < now {
				delete(m.seen, k)
			}
		}
	}
	m.seen[key] = now + seenTTL
	return true
}

func (m *Manager) RelayTx(ctx context.Context, raw []byte) {
	if !m.Enabled() || len(raw) == 0 {
		return
	}
	now := time.Now().Unix()
	if !m.shouldRelayTx(raw, now) {
		return
	}
	for _, peer := range m.peerListSnapshot() {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, peer+"/api/p2p/tx", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		m.headers(req.Header)
		resp, err := m.client.Do(req)
		if err == nil && resp != nil {
			_ = resp.Body.Close()
		}
	}
}
