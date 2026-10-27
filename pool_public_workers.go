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

// redactPublicWorkStats strips desktop id leakage and re-keys workers for unauthenticated callers.
func redactPublicWorkStats(ws map[string]any) {
	if ws == nil {
		return
	}
	stripDesktopWorkerFields(ws)
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
