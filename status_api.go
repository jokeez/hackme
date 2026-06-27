package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"hackme/internal/block"
	"hackme/internal/chain"
	"hackme/internal/sandbox"
	"hackme/internal/store"
)

// mergeCanonicalEconomicsIntoStatus overlays economics and policy JSON from the canonical
// node's /api/status when a remote canonical base is configured. Keeps follower/worker
// dashboards and release gates aligned with canon without implying local ledger economics.
func (a *app) mergeCanonicalEconomicsIntoStatus(ctx context.Context, statusBody map[string]any) {
	if a.miner.Running() {
		return
	}
	base := strings.TrimRight(strings.TrimSpace(a.canonicalChainBaseURL()), "/")
	if base == "" || a.canonicalBaseIsSelfNode(base) {
		return
	}
	u, err := url.Parse(base)
	if err != nil {
		return
	}
	host := strings.ToLower(strings.TrimSpace(u.Host))
	if host == "127.0.0.1:8080" || host == "localhost:8080" {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, canonicalPeerStatusURL(base), nil)
	if err != nil {
		return
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp == nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var remote map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&remote); err != nil {
		return
	}
	if v, ok := remote["economics"]; ok {
		statusBody["economics"] = v
	}
	if v, ok := remote["crypto_policy"]; ok {
		statusBody["crypto_policy"] = v
	}
	if v, ok := remote["consensus_policy"]; ok {
		statusBody["consensus_policy"] = v
	}
}

func statusQueryLite(r *http.Request) bool {
	q := strings.TrimSpace(r.URL.Query().Get("lite"))
	return q == "1" || strings.EqualFold(q, "true")
}

// buildStatusLite serves fast polls for desktop dashboards: no coordinator fan-out,
// no remote economics merge, minimal SQLite reads.
func (a *app) buildStatusLite(ctx context.Context) map[string]any {
	networkModeActive := a.networkModeActive()
	localSoloAllowed := envBool("HACKME_CHAIN_LEADER_LOCAL_POH", false)
	poolCoordURL := strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_URL"))
	coordEff := strings.TrimRight(strings.TrimSpace(a.coordinatorBaseURL()), "/")
	canonBase := strings.TrimRight(strings.TrimSpace(a.canonicalChainBaseURL()), "/")
	body := map[string]any{
		"chain_id":                       block.ChainID,
		"version":                        Version,
		"commit":                         Commit,
		"build_date":                     BuildDate,
		"has_genesis":                    false,
		"tip_height":                     uint64(0),
		"tip_hash":                       "",
		"mining":                         a.miner.Running(),
		"node_address":                   a.nodeID,
		"network_mode_active":            networkModeActive,
		"local_solo_allowed":             localSoloAllowed,
		"chain_leader_local_poh":         localSoloAllowed,
		"pool_coordinator_url":           poolCoordURL,
		"pool_coordinator_url_effective": coordEff,
		"desktop_mode":                   envBool("HACKME_DESKTOP_MODE", false),
		"status_lite":                    true,
		"admin_auth_enabled":             adminAuthEnabled(),
		"sandbox_policy":                 sandbox.Policy(),
		"tip_sync_source":                "local_ledger",
	}
	has, h, tip, tipStale := a.chainTipForStatus(ctx)
	body["has_genesis"] = has
	body["tip_height"] = h
	body["tip_hash"] = tip
	if tipStale {
		body["tip_read_stale"] = true
	}
	a.applyFollowerCanonicalTipSnapshot(ctx, body, 700*time.Millisecond)
	if ct := asUint64(body["canonical_tip_height"]); ct > 0 && has {
		lag := int64(ct) - int64(h)
		if lag < 0 {
			lag = 0
		}
		body["network_sync"] = map[string]any{
			"canonical_ledger_lag_blocks": uint64(lag),
			"public_authority_base":       strings.TrimSpace(os.Getenv("HACKME_PUBLIC_AUTHORITY_BASE")),
		}
	}
	applyDisplayTipHeight(body)
	if canonBase != "" {
		body["canonical_chain_base_url"] = canonBase
	}
	if pub := strings.TrimSpace(os.Getenv("HACKME_PUBLIC_AUTHORITY_BASE")); pub != "" {
		body["public_authority_base"] = pub
	}
	a.attachPoolLaneCached(body)
	if strings.TrimSpace(a.coordinatorBaseURL()) != "" {
		a.warmWorkStatsCacheAsync(a.coordinatorBaseURL(), false)
	}
	econCtx, econCancel := context.WithTimeout(ctx, 250*time.Millisecond)
	if ec, err := a.chain.Economics(econCtx); err == nil {
		body["economics"] = ec
	}
	econCancel()
	body["pool_sync"] = a.poolSyncStatusPayload()
	return body
}

func (a *app) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if statusQueryLite(r) {
		writeJSON(w, a.buildStatusLite(r.Context()))
		return
	}
	ctx := r.Context()
	// Keep /api/status responsive even under transient DB contention.
	statusCtx, cancelStatus := context.WithTimeout(ctx, 3*time.Second)
	defer cancelStatus()
	networkModeActive := a.networkModeActive()
	// Only chain command nodes may HTTP-start local WASM PoH; followers use worker/coordinator.
	localSoloAllowed := envBool("HACKME_CHAIN_LEADER_LOCAL_POH", false)
	sv := 0
	economics := any(nil)
	schemaCtx, schemaCancel := context.WithTimeout(statusCtx, 200*time.Millisecond)
	if v, err := a.chain.SchemaVersion(schemaCtx); err == nil {
		sv = v
	}
	schemaCancel()
	has, h, tip, tipStale := a.chainTipForStatus(statusCtx)
	econCtx, econCancel := context.WithTimeout(statusCtx, 350*time.Millisecond)
	if ec, err := a.chain.Economics(econCtx); err == nil {
		economics = ec
	}
	econCancel()
	poolCoordURL := strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_URL"))
	statusBody := map[string]any{
		"chain_id":               block.ChainID,
		"version":                Version,
		"commit":                 Commit,
		"build_date":             BuildDate,
		"has_genesis":            has,
		"tip_height":             h,
		"tip_hash":               tip,
		"mining":                 a.miner.Running(),
		"node_address":           a.nodeID,
		"schema_version":         sv,
		"schema_expected":        store.CurrentSchemaVersion,
		"admin_auth_enabled":     adminAuthEnabled(),
		"network_mode_active":    networkModeActive,
		"local_solo_allowed":     localSoloAllowed,
		"chain_leader_local_poh": localSoloAllowed,
		"pool_coordinator_url":   poolCoordURL,
		"desktop_mode":           envBool("HACKME_DESKTOP_MODE", false),
		"economics":              economics,
		"sandbox_policy":         sandbox.Policy(),
		"p2p_policy": map[string]any{
			"ingress_allow_cidrs_count": len(a.p2pIngress.allowCIDRs),
			"ingress_deny_cidrs_count":  len(a.p2pIngress.denyCIDRs),
			"max_peers_per_24":          a.p2pIngress.maxPeersPer24,
			"token_fail_ban_sec":        a.p2pIngress.tokenBanSec,
			"sync_heavy_limit":          cap(a.p2pSyncHeavySem),
			"sync_heavy_inflight":       len(a.p2pSyncHeavySem),
		},
		"crypto_policy": map[string]any{
			"block_sig_algs_supported":    []string{chain.TransferSigAlgEd25519},
			"transfer_sig_algs_supported": []string{chain.TransferSigAlgEd25519},
			"pq_ready_wire_versioning":    true,
			"pq_note":                     "wire fields are versioned for future post-quantum signature rollout",
		},
		"consensus_policy": map[string]any{
			"simultaneous_block_rule": "first_valid_block_on_canonical_node_wins",
			"fork_resolution":         "no_reorg_v1_fail_closed",
			"fork_action":             "followers_stop_mining_and_reseed_from_canonical",
			"hybrid_signer_enabled":   envBool("HACKME_POOL_HYBRID_SIGNER_ENABLED", false),
		},
		"pool_sync": a.poolSyncStatusPayload(),
	}
	if tipStale {
		statusBody["tip_read_stale"] = true
	}
	if networkModeActive && !a.miner.Running() {
		a.applyFollowerCanonicalTipSnapshot(statusCtx, statusBody, 900*time.Millisecond)
	}
	a.mergeCanonicalEconomicsIntoStatus(statusCtx, statusBody)
	if !a.miner.Running() && !networkModeActive {
		a.applyCanonicalChainTipToStatusMap(statusCtx, statusBody)
		if ct := asUint64(statusBody["canonical_tip_height"]); ct > 0 && strings.TrimSpace(asString(statusBody["canonical_tip_hash"])) != "" {
			hasG, _ := statusBody["canonical_tip_has_genesis"].(bool)
			a.cacheCanonicalTip(hasG, ct, asString(statusBody["canonical_tip_hash"]))
		}
	}

	coordEff := strings.TrimRight(strings.TrimSpace(a.coordinatorBaseURL()), "/")
	canonBase := strings.TrimRight(strings.TrimSpace(a.canonicalChainBaseURL()), "/")
	p2pConfigured := strings.TrimSpace(os.Getenv("HACKME_P2P_PEERS")) != ""
	canonConfigured := canonBase != "" && walletCanonicalBaseUsable(canonBase)
	canonDisplayHost := ""
	if canonConfigured {
		if u, err := url.Parse(canonBase); err == nil {
			canonDisplayHost = strings.TrimSpace(u.Host)
		}
	}
	remoteCanon := false
	if tipOk, _ := statusBody["canonical_tip_ok"].(bool); tipOk && canonConfigured {
		remoteCanon = true
	}
	if !remoteCanon && canonConfigured && !a.miner.Running() {
		probeCtx, probeCancel := context.WithTimeout(statusCtx, 800*time.Millisecond)
		_, _, _, remoteCanon = a.fetchCanonicalStatusTip(probeCtx)
		probeCancel()
	}
	if !remoteCanon && canonConfigured {
		if u, err := url.Parse(canonBase); err == nil {
			h := strings.ToLower(strings.TrimSpace(u.Host))
			remoteCanon = h != "" && h != "127.0.0.1:8080" && h != "localhost:8080"
		}
	}
	hints := make([]string, 0, 6)
	stagingJoin := PublicStagingJoinEnvExport()
	pubAuth := strings.TrimSpace(os.Getenv("HACKME_PUBLIC_AUTHORITY_BASE")) != ""
	if !networkModeActive {
		hints = append(hints, "Standalone: local SQLite only. Public pool: network mode + Copy env snippet + restart + Mining → Start worker.")
	}
	if networkModeActive && coordEff == "" {
		hints = append(hints, "Set HACKME_POOL_COORDINATOR_URL or start the worker once so a coordinator URL is stored.")
	}
	if networkModeActive && canonBase == "" {
		hints = append(hints, "Set HACKME_CANONICAL_CHAIN_URL or HACKME_P2P_PEERS so tip/balance match the pool.")
	}
	if networkModeActive && !p2pConfigured && remoteCanon {
		if pubAuth {
			hints = append(hints, "HACKME_PUBLIC_AUTHORITY_BASE pins pool + canonical; add HACKME_P2P_PEERS only to sync local SQLite block height.")
		} else {
			hints = append(hints, "Optional HACKME_P2P_PEERS keeps local SQLite height closer to the network.")
		}
	}
	if len(hints) == 0 && networkModeActive && coordEff != "" && remoteCanon {
		hints = append(hints, "Pool path ready — Mining → Start worker.")
	}

	// Open-network sync diagnostics (fast path: in-memory P2P peer heights, no extra HTTP).
	netSync := map[string]any{
		"p2p_enabled":                  a.p2p != nil && a.p2p.Enabled(),
		"state_replay_enabled":         envBool("HACKME_P2P_SYNC_STATE_REPLAY_ENABLED", false),
		"background_sync_interval_sec": p2pBackgroundSyncIntervalSec(),
		"bind_addr":                    strings.TrimSpace(os.Getenv("HACKME_BIND_ADDR")),
		"http_cors_configured":         strings.TrimSpace(os.Getenv("HACKME_HTTP_CORS_ALLOW_ORIGIN")) != "",
		"public_authority_base":        strings.TrimSpace(os.Getenv("HACKME_PUBLIC_AUTHORITY_BASE")),
	}
	if a.p2p != nil && a.p2p.Enabled() {
		ph := a.p2p.BuildSyncHint(h, tip)
		netSync["p2p_sync_hint"] = ph
		if ph.LagBlocks > 0 {
			hints = append(hints, fmt.Sprintf("P2P: local height %d lags healthy peer by %d blocks (best peer %s). Enable HACKME_P2P_SYNC_STATE_REPLAY_ENABLED=1 and optional HACKME_P2P_BACKGROUND_SYNC_SEC=30 on followers, or POST /api/p2p/sync/run with admin token.",
				ph.LocalHeight, ph.LagBlocks, strings.TrimSpace(ph.BestPeerURL)))
		}
	}
	if ct := asUint64(statusBody["canonical_tip_height"]); ct > 0 && has {
		lag := int64(ct) - int64(h)
		if lag < 0 {
			lag = 0
		}
		netSync["canonical_ledger_lag_blocks"] = uint64(lag)
		if lag > 0 && networkModeActive {
			hints = append(hints, fmt.Sprintf("SQLite is %d blocks behind public tip — expected until P2P/bootstrap catches up (same policy_hash).", uint64(lag)))
		}
	} else {
		netSync["canonical_ledger_lag_blocks"] = 0
	}
	statusBody["network_sync"] = netSync

	// tip_height / tip_hash are always local SQLite; explorer badges use this + canonical_tip_*.
	statusBody["tip_sync_source"] = "local_ledger"
	statusBody["pool_coordinator_url_effective"] = coordEff
	statusBody["miner_public_ux"] = map[string]any{
		"coordinator_url_effective":  coordEff,
		"canonical_chain_base":       canonBase,
		"canonical_chain_configured": canonConfigured,
		"canonical_display_host":     canonDisplayHost,
		"canonical_remote_reachable": remoteCanon,
		"p2p_peers_configured":       p2pConfigured,
		"worker_subprocess_running":  a.workerProcessRunning(),
		"hints_en":                   hints,
		"staging_command_base":       defaultPublicStagingCommandBase,
		"staging_coordinator_base":   defaultPublicStagingCoordinatorBase,
		"staging_join_env_export":    stagingJoin,
	}

	a.attachPoolLaneCached(statusBody)
	if coord := strings.TrimRight(strings.TrimSpace(a.coordinatorBaseURL()), "/"); coord != "" {
		a.warmWorkStatsCacheAsync(coord, false)
	}
	applyDisplayTipHeight(statusBody)

	writeJSON(w, statusBody)
}
