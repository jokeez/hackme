package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"hackme/internal/sandbox"
)

const maxOrderWasmClaimBytes = 512 * 1024

type activeOrderSnap struct {
	ID        string
	RewardHMC float64
	WasmHex   string
	ChainMod  uint64
	FetchedAt int64
}

// orderManifestWasmReward extracts wasm_check_hex + reward_hmc from tasks.manifest_json
// whether the API decoded it as a JSON object or left it as a JSON string.
func orderManifestWasmReward(raw any) (wasmHex string, rewardHMC float64) {
	var mf struct {
		WasmCheckHex string  `json:"wasm_check_hex"`
		RewardHMC    float64 `json:"reward_hmc"`
	}
	switch v := raw.(type) {
	case nil:
		return "", 0
	case string:
		s := strings.TrimSpace(v)
		if s == "" || s == "<nil>" {
			return "", 0
		}
		if err := json.Unmarshal([]byte(s), &mf); err != nil {
			return "", 0
		}
	case map[string]any:
		b, err := json.Marshal(v)
		if err != nil {
			return "", 0
		}
		if err := json.Unmarshal(b, &mf); err != nil {
			return "", 0
		}
	case json.RawMessage:
		if len(v) == 0 {
			return "", 0
		}
		if err := json.Unmarshal(v, &mf); err != nil {
			return "", 0
		}
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", 0
		}
		if err := json.Unmarshal(b, &mf); err != nil {
			return "", 0
		}
	}
	return strings.TrimSpace(mf.WasmCheckHex), mf.RewardHMC
}

func (m *workManager) ordersAdminToken() string {
	for _, key := range []string{
		"HACKME_COORDINATOR_ORDERS_ADMIN_TOKEN",
		"HACKME_COORDINATOR_CHAIN_ADMIN_TOKEN",
		"HACKME_COORDINATOR_ADMIN_TOKEN",
	} {
		if t := strings.TrimSpace(os.Getenv(key)); t != "" {
			return t
		}
	}
	return ""
}

// liveChainPoHModFromNode returns canonical mining_target_mod from the chain node metrics API.
func (m *workManager) liveChainPoHModFromNode() uint64 {
	base := strings.TrimRight(strings.TrimSpace(m.ordersProbeURL), "/")
	if base == "" {
		base = strings.TrimRight(strings.TrimSpace(m.targetURL), "/")
	}
	if base == "" {
		return 0
	}
	cl := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequest(http.MethodGet, base+"/api/metrics", nil)
	if err != nil {
		return 0
	}
	if tok := m.ordersAdminToken(); tok != "" {
		req.Header.Set("X-Hackme-Admin-Token", tok)
	}
	resp, err := cl.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0
	}
	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return 0
	}
	v, ok := body["mining_target_mod"]
	if !ok {
		return 0
	}
	switch t := v.(type) {
	case float64:
		if t > 0 {
			return uint64(t)
		}
	case json.Number:
		if x, err := t.Int64(); err == nil && x > 0 {
			return uint64(x)
		}
	}
	return 0
}

// parseJSONUint64 reads a positive integer from common JSON number encodings.
func parseJSONUint64(v any) (uint64, bool) {
	switch t := v.(type) {
	case float64:
		if t > 0 {
			return uint64(t), true
		}
	case json.Number:
		if x, err := t.Int64(); err == nil && x > 0 {
			return uint64(x), true
		}
	case int:
		if t > 0 {
			return uint64(t), true
		}
	case int64:
		if t > 0 {
			return uint64(t), true
		}
	case uint64:
		if t > 0 {
			return t, true
		}
	}
	return 0, false
}

func nodeProbeBaseURL(m *workManager) string {
	base := strings.TrimRight(strings.TrimSpace(m.ordersProbeURL), "/")
	if base == "" {
		base = strings.TrimRight(strings.TrimSpace(m.targetURL), "/")
	}
	return base
}

// liveCanonicalTipHeightFromNode returns canonical chain tip height from the linked hackme-node.
func (m *workManager) liveCanonicalTipHeightFromNode() (uint64, bool) {
	base := nodeProbeBaseURL(m)
	if base == "" {
		return 0, false
	}
	cl := &http.Client{Timeout: 2 * time.Second}
	tok := m.ordersAdminToken()

	fetch := func(path string) (uint64, bool) {
		req, err := http.NewRequest(http.MethodGet, base+path, nil)
		if err != nil {
			return 0, false
		}
		if tok != "" {
			req.Header.Set("X-Hackme-Admin-Token", tok)
		}
		resp, err := cl.Do(req)
		if err != nil {
			return 0, false
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return 0, false
		}
		var body map[string]any
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
			return 0, false
		}
		for _, key := range []string{"tip_height", "block_height", "canonical_tip_height"} {
			if h, ok := parseJSONUint64(body[key]); ok {
				return h, true
			}
		}
		return 0, false
	}

	if h, ok := fetch("/api/status"); ok {
		return h, true
	}
	return fetch("/api/metrics")
}

func (m *workManager) ordersSolveEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_COORDINATOR_ORDERS_SOLVE_RELAY")))
	if v == "" {
		return strings.TrimSpace(m.ordersProbeURL) != ""
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func (m *workManager) storeChainPoHMod(mod uint64) {
	if mod == 0 {
		return
	}
	m.schedulerMu.Lock()
	m.chainPoHMod = mod
	m.schedulerMu.Unlock()
}

func (m *workManager) chainPoHModNow() uint64 {
	m.schedulerMu.Lock()
	defer m.schedulerMu.Unlock()
	return m.chainPoHModUnlocked()
}

// chainPoHModUnlocked reads chainPoHMod/targetMod; caller must hold schedulerMu.
func (m *workManager) chainPoHModUnlocked() uint64 {
	if m.chainPoHMod > 0 {
		return m.chainPoHMod
	}
	return m.targetMod
}

func (m *workManager) activeOrderSnapshot() activeOrderSnap {
	m.schedulerMu.Lock()
	defer m.schedulerMu.Unlock()
	return m.refreshActiveOrderLocked()
}

// verifyOrderWasmGate re-runs the order WASM check on the coordinator; worker wasm_gate_pass is not trusted alone.
func (m *workManager) verifyOrderWasmGate(snap activeOrderSnap, nonce uint64) (bool, string) {
	if snap.ID == "" || strings.TrimSpace(snap.WasmHex) == "" {
		return true, ""
	}
	raw, err := hex.DecodeString(strings.TrimSpace(snap.WasmHex))
	if err != nil || len(raw) == 0 {
		return false, "wasm_gate_invalid_hex"
	}
	if err := sandbox.ValidateCheckWasm(context.Background(), raw); err != nil {
		return false, "wasm_gate_server_reject"
	}
	ok, execErr := sandbox.InvokeCheck(context.Background(), raw, nonce)
	if execErr != nil || !ok {
		return false, "wasm_gate_server_reject"
	}
	return true, ""
}

func (m *workManager) refreshActiveOrderLocked() activeOrderSnap {
	if strings.TrimSpace(m.ordersProbeURL) == "" {
		return m.activeOrder
	}
	now := time.Now().Unix()
	if m.activeOrder.ID != "" && now-m.activeOrder.FetchedAt < m.ordersProbeEverySec {
		return m.activeOrder
	}
	cl := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequest(http.MethodGet, m.ordersProbeURL+"/api/tasks", nil)
	if err != nil {
		return activeOrderSnap{}
	}
	if tok := m.ordersAdminToken(); tok != "" {
		req.Header.Set("X-Hackme-Admin-Token", tok)
	}
	resp, err := cl.Do(req)
	if err != nil {
		return activeOrderSnap{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return activeOrderSnap{}
	}
	var body struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&body); err != nil {
		return activeOrderSnap{}
	}
	var pick map[string]any
	var pickCreated float64
	for _, row := range body.Tasks {
		if !strings.EqualFold(strings.TrimSpace(fmt.Sprintf("%v", row["status"])), "open") {
			continue
		}
		cr, _ := row["created_at"].(float64)
		// Oldest open order first — fair FIFO for paying customers.
		if pick == nil || cr < pickCreated {
			pick, pickCreated = row, cr
		}
	}
	if pick == nil {
		m.activeOrder = activeOrderSnap{}
		return activeOrderSnap{}
	}
	id := strings.TrimSpace(fmt.Sprintf("%v", pick["id"]))
	// API may return manifest_json as a JSON object (map) or as a JSON string.
	// fmt.Sprintf("%v", map) is NOT valid JSON — that used to drop wasm and leave
	// scheduler_mode=orders with leases but no order_task_id on claims (progress stuck at 0).
	wasmHex, mfReward := orderManifestWasmReward(pick["manifest_json"])
	if wasmHex == "" {
		m.activeOrder = activeOrderSnap{}
		return activeOrderSnap{}
	}
	raw, err := hex.DecodeString(wasmHex)
	if err != nil || len(raw) == 0 || len(raw) > maxOrderWasmClaimBytes {
		m.activeOrder = activeOrderSnap{}
		return activeOrderSnap{}
	}
	if err := sandbox.ValidateCheckWasm(context.Background(), raw); err != nil {
		m.activeOrder = activeOrderSnap{}
		return activeOrderSnap{}
	}
	reward, _ := pick["reward"].(float64)
	if reward <= 0 {
		reward = mfReward
	}
	// Order pool work uses coordinator pool M (fair for remote miners), not canonical chain solo M.
	poolMod := m.clampTargetMod(m.targetMod)
	if poolMod == 0 {
		poolMod = m.chainPoHModUnlocked()
	}
	if live := m.liveChainPoHModFromNode(); live > 0 {
		m.chainPoHMod = live
	}
	snap := activeOrderSnap{
		ID:        id,
		RewardHMC: reward,
		WasmHex:   wasmHex,
		ChainMod:  poolMod,
		FetchedAt: now,
	}
	m.activeOrder = snap
	return snap
}

type orderSolveRelayResult struct {
	OK        bool
	BlockHash string
	Reason    string
}

func (m *workManager) relayOrderSolve(minerAddress string, foundNonce, targetMod uint64, orderTaskID string) orderSolveRelayResult {
	out := orderSolveRelayResult{Reason: "orders_solve_disabled"}
	if !m.ordersSolveEnabled() {
		return out
	}
	base := strings.TrimRight(strings.TrimSpace(m.ordersProbeURL), "/")
	if base == "" || strings.TrimSpace(orderTaskID) == "" || strings.TrimSpace(minerAddress) == "" {
		out.Reason = "orders_url_or_fields_missing"
		return out
	}
	tok := m.ordersAdminToken()
	if tok == "" {
		out.Reason = "orders_admin_token_missing"
		return out
	}
	body, _ := json.Marshal(map[string]any{
		"miner_address": minerAddress,
		"found_nonce":   foundNonce,
		"target_mod":    targetMod,
		"order_task_id": orderTaskID,
	})
	req, err := http.NewRequest(http.MethodPost, base+"/api/poh/solve-order", bytes.NewReader(body))
	if err != nil {
		out.Reason = "relay_build_failed"
		return out
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", tok)
	cl := &http.Client{Timeout: 15 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		out.Reason = "relay_http_error"
		return out
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var decoded map[string]any
	_ = json.Unmarshal(raw, &decoded)
	if resp.StatusCode != http.StatusOK {
		if r, _ := decoded["error"].(string); r != "" {
			out.Reason = r
		} else {
			out.Reason = fmt.Sprintf("relay_http_%d", resp.StatusCode)
		}
		return out
	}
	out.OK = true
	out.BlockHash, _ = decoded["block_hash"].(string)
	out.Reason = ""
	return out
}

func configTruthy(cfg map[string]any, keys ...string) bool {
	if cfg == nil {
		return false
	}
	for _, k := range keys {
		v, ok := cfg[k]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case bool:
			if t {
				return true
			}
		case string:
			s := strings.TrimSpace(strings.ToLower(t))
			if s == "1" || s == "true" || s == "yes" || s == "on" {
				return true
			}
		case float64:
			if t != 0 {
				return true
			}
		case int:
			if t != 0 {
				return true
			}
		case json.Number:
			if x, err := t.Float64(); err == nil && x != 0 {
				return true
			}
		}
	}
	return false
}

func configString(cfg map[string]any, keys ...string) string {
	if cfg == nil {
		return ""
	}
	for _, k := range keys {
		v, ok := cfg[k]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			if s := strings.TrimSpace(t); s != "" {
				return s
			}
		}
	}
	return ""
}

func configFloat(cfg map[string]any, keys ...string) float64 {
	if cfg == nil {
		return 0
	}
	for _, k := range keys {
		v, ok := cfg[k]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case float64:
			return t
		case int:
			return float64(t)
		case json.Number:
			if x, err := t.Float64(); err == nil {
				return x
			}
		case string:
			if x, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil {
				return x
			}
		}
	}
	return 0
}

func configInt(cfg map[string]any, keys ...string) int {
	if cfg == nil {
		return 0
	}
	for _, k := range keys {
		v, ok := cfg[k]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case float64:
			return int(t)
		case int:
			return t
		case json.Number:
			if x, err := t.Int64(); err == nil {
				return int(x)
			}
		case string:
			if x, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
				return x
			}
		}
	}
	return 0
}

func (m *workManager) attachPoHOrderEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_COORDINATOR_ATTACH_POH_ORDER")))
	if v == "0" || v == "false" || v == "no" || v == "off" {
		return false
	}
	return true
}

// attachPoHOrderFromFuzzConfig creates a chain PoH order on ORDERS_URL when a
// pool-distributed campaign asks for create_poh_order/attach_poh_order.
// Workerpoh fleets then auto-switch into scheduler_mode=orders — no workerfuzz needed for that rail.
func (m *workManager) attachPoHOrderFromFuzzConfig(campaignID string, cfg map[string]any) map[string]any {
	out := map[string]any{"ok": false, "skipped": true}
	if m == nil || !m.attachPoHOrderEnabled() {
		out["reason"] = "attach_disabled"
		return out
	}
	if !configTruthy(cfg, "attach_poh_order", "create_poh_order") {
		out["reason"] = "flag_off"
		return out
	}
	base := strings.TrimRight(strings.TrimSpace(m.ordersProbeURL), "/")
	if base == "" {
		out["reason"] = "orders_url_missing"
		out["skipped"] = false
		return out
	}
	tok := m.ordersAdminToken()
	if tok == "" {
		out["reason"] = "orders_admin_token_missing"
		out["skipped"] = false
		return out
	}
	wasmHex := strings.TrimSpace(strings.ToLower(configString(cfg, "wasm_check_hex")))
	if wasmHex == "" {
		out["reason"] = "wasm_missing"
		out["skipped"] = false
		return out
	}
	raw, err := hex.DecodeString(wasmHex)
	if err != nil || len(raw) == 0 || len(raw) > maxOrderWasmClaimBytes {
		out["reason"] = "wasm_invalid"
		out["skipped"] = false
		return out
	}
	if err := sandbox.ValidateCheckWasm(context.Background(), raw); err != nil {
		out["reason"] = "wasm_validation_failed"
		out["skipped"] = false
		return out
	}
	orderID := configString(cfg, "poh_order_id", "order_id")
	if orderID == "" {
		cid := strings.TrimSpace(campaignID)
		if cid == "" {
			out["reason"] = "order_id_missing"
			out["skipped"] = false
			return out
		}
		orderID = "order-pool-" + cid
	}
	reward := configFloat(cfg, "poh_reward_hmc", "reward_hmc")
	if reward <= 0 {
		reward = 0.05
	}
	target := configInt(cfg, "poh_target_solves", "target_solves")
	if target < 1 {
		target = 1
	}
	diff := configInt(cfg, "poh_difficulty_score", "difficulty_score")
	if diff < 1 {
		diff = 10
	}
	payer := configString(cfg, "poh_payer_ref", "payer_ref")
	if payer == "" {
		payer = "pool-attach:" + strings.TrimSpace(campaignID)
	}
	manifest := map[string]any{
		"id":               orderID,
		"kind":             "synthetic_poh_v1",
		"reward_hmc":       reward,
		"difficulty_score": diff,
		"target_solves":    target,
		"payer_ref":        payer,
		"wasm_check_hex":   wasmHex,
	}
	body, _ := json.Marshal(manifest)
	req, err := http.NewRequest(http.MethodPost, base+"/api/tasks", bytes.NewReader(body))
	if err != nil {
		out["reason"] = "build_failed"
		out["skipped"] = false
		return out
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", tok)
	cl := &http.Client{Timeout: 20 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		out["reason"] = "http_error"
		out["skipped"] = false
		out["detail"] = err.Error()
		return out
	}
	defer resp.Body.Close()
	rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var decoded map[string]any
	_ = json.Unmarshal(rawBody, &decoded)
	errText, _ := decoded["error"].(string)
	if resp.StatusCode == http.StatusOK {
		out["ok"] = true
		out["skipped"] = false
		out["order_id"] = orderID
		out["prepaid_hmc"] = decoded["prepaid_hmc"]
		out["total_debit_hmc"] = decoded["total_debit_hmc"]
		m.mu.Lock()
		m.activeOrder = activeOrderSnap{} // force refresh on next claim
		m.mu.Unlock()
		return out
	}
	if resp.StatusCode == http.StatusBadRequest && strings.Contains(strings.ToLower(errText), "already exists") {
		out["ok"] = true
		out["skipped"] = false
		out["order_id"] = orderID
		out["reason"] = "already_exists"
		m.mu.Lock()
		m.activeOrder = activeOrderSnap{}
		m.mu.Unlock()
		return out
	}
	out["skipped"] = false
	if errText != "" {
		out["reason"] = errText
	} else {
		out["reason"] = fmt.Sprintf("http_%d", resp.StatusCode)
	}
	out["order_id"] = orderID
	return out
}
