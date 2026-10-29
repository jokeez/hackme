package main

import (
	"encoding/hex"
	"strings"

	"github.com/zeebo/blake3"
)

func publicWorkerPseudoKey(workerID string) string {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return ""
	}
	sum := blake3.Sum256([]byte("hackme-public-worker-v1|" + workerID))
	return hex.EncodeToString(sum[:8])
}

func stripDesktopWorkerFields(ws map[string]any) {
	if ws == nil {
		return
	}
	delete(ws, "desktop_worker_id")
	delete(ws, "desktop_worker_injected")
	delete(ws, "desktop_worker_on_coordinator")
}

// redactPublicActiveRigRows pseudonymizes active_rigs worker_id/name (same key as workers map);
// raw ids there would let anyone recompute and de-anonymize the workers pseudonyms.
func redactPublicActiveRigRows(ws map[string]any) {
	rows, ok := ws["active_rigs"].([]any)
	if !ok || len(rows) == 0 {
		return
	}
	red := make([]any, 0, len(rows))
	for _, r := range rows {
		row, ok := r.(map[string]any)
		if !ok {
			continue
		}
		id, _ := row["worker_id"].(string)
		key := publicWorkerPseudoKey(id)
		if key == "" {
			continue
		}
		cp := make(map[string]any, len(row))
		for k, v := range row {
			switch k {
			case "ip", "client_ip", "last_client_ip":
				continue
			}
			cp[k] = v
		}
		cp["worker_id"] = key
		cp["name"] = key
		red = append(red, cp)
	}
	ws["active_rigs"] = red
}

func stripWorkerRowClientIPs(ws map[string]any) {
	raw, ok := ws["workers"].(map[string]any)
	if !ok || len(raw) == 0 {
		return
	}
	for id, row := range raw {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		cp := make(map[string]any, len(m))
		for k, v := range m {
			switch k {
			case "last_client_ip", "client_ip", "ip":
				continue
			default:
				cp[k] = v
			}
		}
		raw[id] = cp
	}
	ws["workers"] = raw
}

// redactPublicWorkStats strips desktop id leakage and re-keys workers for unauthenticated callers.
func redactPublicWorkStats(ws map[string]any) {
	if ws == nil {
		return
	}
	stripDesktopWorkerFields(ws)
	// Coordinator public lane already sets workers_keys=pseudonym_v1. Re-hashing
	// those keys produces a second blake3 layer (e.g. 6a97… → c696…) and hides
	// the operator's real public twin from the desktop table.
	if keys, _ := ws["workers_keys"].(string); keys == "pseudonym_v1" {
		stripWorkerRowClientIPs(ws)
		redactPublicActiveRigRows(ws)
		return
	}
	redactPublicActiveRigRows(ws)
	raw, ok := ws["workers"].(map[string]any)
	if !ok || len(raw) == 0 {
		return
	}
	out := make(map[string]any, len(raw))
	for id, row := range raw {
		key := publicWorkerPseudoKey(id)
		if key == "" {
			continue
		}
		m, ok := row.(map[string]any)
		if !ok {
			out[key] = row
			continue
		}
		cp := make(map[string]any, len(m))
		for k, v := range m {
			switch k {
			case "last_client_ip", "client_ip", "ip":
				continue
			default:
				cp[k] = v
			}
		}
		if prev, ok := out[key].(map[string]any); ok {
			for k, v := range cp {
				prev[k] = v
			}
			out[key] = prev
			continue
		}
		out[key] = cp
	}
	ws["workers"] = out
	ws["workers_count"] = uint64(len(out))
	ws["workers_keys"] = "pseudonym_v1"
}
